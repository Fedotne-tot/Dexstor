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
	"bratan-go/internal/projectindex"
	"bratan-go/internal/tools"
)

func TestPersistedRunContinuesFromRepairRead(t *testing.T) {
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

	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(math), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "test.go"), []byte(testProgram), 0644); err != nil {
		t.Fatal(err)
	}

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
			/*
				This is the only model call needed after restore:
				REPAIR_READ is deterministic, then REPAIR_WRITE asks
				the model for the actual write_file operation.
			*/
			response = `package main

func Add(a, b int) int {
	return 10
}
`
		default:
			t.Fatalf("unexpected Ollama request #%d", n)
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
		})
	}))
	defer server.Close()

	/*
		Simulate the state saved immediately after TEST FAILED.
		The persisted ProjectIndex is the pre-task filesystem baseline
		used by semantic change verification after restore.
	*/
	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	first := &Agent{
		Model:        "test-model",
		Max:          20,
		Root:         root,
		Router:       tools.New(root),
		Ollama:       ollama.New(server.URL),
		State:        StateRepairRead,
		ProjectIndex: baseline,

		RepairPath:    "math.go",
		LastWritePath: "math.go",
		LastTestError: "WRONG: 5",
		LastAction:    "test:test.go",
		LastResult:    "WRONG: 5",

		History: []string{
			"inspect_project",
			"read_file:math.go",
			"read_file:test.go",
			"write_file:math.go",
			"git_diff",
			"test:test.go",
		},

		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "math.go\ntest.go",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "math.go"),
				Args:      []string{"math.go"},
				Result:    math,
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 3,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "test.go"),
				Args:      []string{"test.go"},
				Result:    testProgram,
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 4,
				Tool:      "write_file",
				ActionKey: makeActionKey("write_file", "math.go", math),
				Args:      []string{"math.go", math},
				BaseHash:  contentHash(math),
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 5,
				Tool:      "git_diff",
				ActionKey: makeActionKey("git_diff"),
				Result:    "diff",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 6,
				Tool:      "test",
				ActionKey: makeActionKey("test", "test.go"),
				Args:      []string{"test.go"},
				Result:    "WRONG: 5",
				OK:        false,
				Exit:      1,
			},
		},
	}

	task := "Fix math.go so add(2, 3) returns 10"

	if err := first.saveState(task, 6); err != nil {
		t.Fatalf("save interrupted state: %v", err)
	}

	/*
		New Agent: this is the actual continuation.
	*/
	resumed := &Agent{
		Model:  "test-model",
		Max:    20,
		Root:   root,
		Router: tools.New(root),
		Ollama: ollama.New(server.URL),
		State:  StateDiscover,
	}

	if err := resumed.Run(task); err != nil {
		t.Fatalf("resumed Run failed: %v", err)
	}

	if resumed.State != StateDone {
		t.Fatalf("expected StateDone, got %s", resumed.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10;") {
		t.Fatalf("math.go was not repaired:\n%s", data)
	}

	var sawRepairRead bool
	var sawRepairWrite bool
	var sawDiff bool
	var sawPass bool

	for _, action := range resumed.ActionLedger {
		switch {
		case action.Tool == "repair_read" && action.OK:
			sawRepairRead = true
		case action.Tool == "repair_write" && action.OK:
			sawRepairWrite = true
		case action.Tool == "git_diff" && action.OK:
			sawDiff = true
		case action.Tool == "test" && action.OK && action.Exit == 0:
			sawPass = true
		}
	}

	// inspect_project is expected in the restored ledger because it
	// belongs to the pre-persistence execution history. What matters
	// for continuation is that the resumed Run did not call Ollama
	// for DISCOVER again.
	if requests != 1 {
		t.Fatalf("resumed Run made %d Ollama requests, want exactly 1", requests)
	}

	if !sawRepairRead {
		t.Fatal("expected repair_read after restoring REPAIR_READ")
	}

	if !sawRepairWrite {
		t.Fatal("expected repair_write after restoring REPAIR_READ")
	}

	if !sawDiff {
		t.Fatal("expected git_diff after repair_write")
	}

	if !sawPass {
		t.Fatal("expected final TEST PASS")
	}

	if requests != 1 {
		t.Fatalf(
			"expected exactly 1 Ollama request after restore, got %d",
			requests,
		)
	}

	t.Logf("resumed history: %v", resumed.History)
}
