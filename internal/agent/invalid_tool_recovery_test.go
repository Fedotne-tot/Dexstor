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

func TestInvalidToolInFSMStateIsBlockedAndRecovered(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go так, чтобы add(2, 3) возвращала 10. Используй read_file/write_file/git_diff/test."

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

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "BRATAN Test"},
		{"git", "-C", root, "add", "math.go", "test.go"},
		{"git", "-C", root, "commit", "-m", "initial state"},
	}

	for _, cmd := range gitCommands {
		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
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

		// State-based mock (Perl migration): FSM path depends on the controller
		// state advertised in the prompt. The deliberately invalid git_diff call
		// in IMPLEMENT is blocked by the controller and re-asks the model, so a
		// fixed request counter cannot describe this run; per-state responses can.
		state := "UNKNOWN"
		for _, marker := range []string{"Current state is: ", "Current state: "} {
			if i := strings.Index(req.Prompt, marker); i >= 0 {
				rest := req.Prompt[i+len(marker):]
				if j := strings.IndexByte(rest, '\n'); j >= 0 {
					state = rest[:j]
				} else {
					state = rest
				}
				break
			}
		}

		var response string

		switch state {
		case "DISCOVER":
			response = `TOOL_CALL
name: inspect_project
END_TOOL_CALL`

		case "IMPLEMENT":
			if n == 2 {
				// Deliberately invalid for StateImplement: must be blocked
				// and recovered without breaking the FSM.
				response = `TOOL_CALL
name: git_diff
END_TOOL_CALL`
			} else if strings.Contains(req.Prompt, "--- FILE: math.go") {
				response = `TOOL_CALL
name: read_file
path: test.go
END_TOOL_CALL`
			} else {
				response = `TOOL_CALL
name: read_file
path: math.go
END_TOOL_CALL`
			}

		case "WRITE_REQUIRED", "REPAIR_WRITE":
			response = `TOOL_CALL
name: write_file
path: math.go
content: package main

func Add(a, b int) int {
    return 10
}
END_TOOL_CALL`

		default:
			t.Fatalf("unexpected Ollama request #%d in state %s", n, state)
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

	if err := agent.Run(task); err != nil {
		t.Fatalf("agent failed after invalid tool: %v", err)
	}

	if agent.State != StateDone {
		t.Fatalf("expected StateDone, got %s", agent.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10") {
		t.Fatalf("math.go was not repaired:\n%s", data)
	}

	if len(agent.History) != 6 {
		t.Fatalf(
			"expected 6 successful workflow actions, got %d: %v",
			len(agent.History),
			agent.History,
		)
	}

	wantHistory := []string{
		"inspect_project",
		"read_file:math.go",
		"read_file:test.go",
		"write_file:math.go",
		"git_diff",
		"test:test.go",
	}

	for i, want := range wantHistory {
		if agent.History[i] != want {
			t.Fatalf(
				"history[%d]: got %q, want %q; history=%v",
				i,
				agent.History[i],
				want,
				agent.History,
			)
		}
	}

	for _, action := range agent.ActionLedger {
		if action.Tool == "git_diff" &&
			action.Iteration == 2 {
			t.Fatalf(
				"blocked git_diff was recorded as an executed action: %+v",
				action,
			)
		}
	}

	if requests != 5 {
		t.Fatalf("expected 5 Ollama requests, got %d", requests)
	}

	state, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}

	if !ok {
		t.Fatal("expected persisted task state")
	}

	if state.State != StateDone {
		t.Fatalf(
			"persisted state: expected DONE, got %s",
			state.State,
		)
	}
}
