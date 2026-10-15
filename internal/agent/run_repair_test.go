package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

// Perl migration: this fixture used to drive the FSM with math.pl/test.pl
// executed by a Perl interpreter. The equivalent Go fixtures below keep the
// exact same FSM contract:
//   - IMPLEMENT accepts one read_file before WRITE_REQUIRED;
//   - a failing TEST names the repaired source ("WRONG: 5") so
//     failedSourcePath resolves the repair target;
//   - the repair write is idempotent on disk, so the controller records it
//     as a duplicate successful repair_write and proceeds deterministically;
//   - VERIFY_DIFF semantic snapshot detects the modified math.go;
//   - final TEST PASS carries the literal "PASS" marker in full output.
func TestRunRepairCycle(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return a + b
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 10 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	brokenMath := `package main

func Add(a, b int) int {
	return 10
}
`

	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(math), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "test.go"), []byte(testProgram), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize a real Git repository so the integration test exercises
	// the actual git_diff tool instead of a mocked implementation.
	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "BRATAN Test"},
		{"git", "-C", root, "add", "math.go", "test.go"},
		{"git", "-C", root, "commit", "-m", "initial test state"},
	}

	for _, cmd := range gitCommands {
		if output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", cmd, err, output)
		}
	}

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Model  string `json:"model"`
			Prompt string `json:"prompt"`
			Stream bool   `json:"stream"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests++
		n := requests
		mu.Unlock()

		var response string

		switch n {
		case 1:
			response = `TOOL_CALL
name: inspect_project`
		case 2:
			response = `TOOL_CALL
name: read_file
arg: math.go`
		case 3:
			response = `TOOL_CALL
name: read_file
arg: test.go`
		case 4:
			response = `TOOL_CALL
name: write_file
path: math.go
content: ` + math
		case 5:
			response = `TOOL_CALL
name: write_file
path: math.go
content: ` + brokenMath
		default:
			t.Fatalf("unexpected Ollama request #%d", n)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
		})
	}))
	defer server.Close()

	agent := &Agent{
		Model:     "test-model",
		Max:       20,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	err := agent.Run("Fix math.go so Add(2, 3) returns 10")
	if err != nil {
		t.Fatalf("repair cycle failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10") {
		t.Fatalf("final math.go is not repaired:\n%s", data)
	}

	if agent.State != StateDone {
		t.Fatalf("expected StateDone, got %s", agent.State)
	}

	var sawTestFail bool
	var sawRepairRead bool
	var sawRepairWrite bool
	var sawFinalDiff bool
	var sawFinalPass bool

	for _, action := range agent.ActionLedger {
		switch {
		case action.Tool == "test" && !action.OK:
			sawTestFail = true
		case action.Tool == "repair_read" && action.OK:
			sawRepairRead = true
		case action.Tool == "repair_write" && action.OK:
			sawRepairWrite = true
		case action.Tool == "git_diff" && action.OK:
			sawFinalDiff = true
		case action.Tool == "test" && action.OK && action.Exit == 0:
			sawFinalPass = true
		}
	}

	if !sawTestFail {
		t.Fatal("expected an initial TEST FAIL in repair cycle")
	}
	if !sawRepairRead {
		t.Fatal("expected repair_read in repair cycle")
	}
	if !sawRepairWrite {
		t.Fatal("expected repair_write in repair cycle")
	}
	if !sawFinalDiff {
		t.Fatal("expected git_diff after repair")
	}
	if !sawFinalPass {
		t.Fatal("expected final TEST PASS")
	}

	if requests != 5 {
		t.Fatalf("expected 5 Ollama requests, got %d", requests)
	}

	t.Logf("repair cycle history: %v", agent.History)
}
