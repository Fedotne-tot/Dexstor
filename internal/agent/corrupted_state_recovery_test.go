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

func TestCorruptedTaskStateRecovery(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go так, чтобы add(2, 3) возвращала 10."

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
		{"git", "-C", root, "commit", "-m", "initial corrupted-state test"},
	}

	for _, cmd := range gitCommands {
		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", cmd, err, output)
		}
	}

	// Deliberately corrupt the persisted state.
	statePath := taskStatePath(root)

	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		statePath,
		[]byte(`{"version":3,"task":`),
		0644,
	); err != nil {
		t.Fatal(err)
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

		// State-based mock: choose the tool from the FSM state advertised
		// in the prompt instead of a fixed request counter (recovery paths
		// issue extra NUDGE requests and iteration counts differ).
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
			response = `TOOL_CALL
name: read_file
path: math.go
END_TOOL_CALL`

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
		t.Fatalf("agent failed to recover from corrupted state: %v", err)
	}

	if agent.State != StateDone {
		t.Fatalf("expected StateDone after recovery, got %s", agent.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10;") {
		t.Fatalf("math.go was not repaired:\n%s", data)
	}

	// The corrupted state must have been preserved rather than silently
	// destroyed.
	corruptPath := statePath + ".corrupt"

	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("expected corrupted state backup %s: %v", corruptPath, err)
	}

	// The main state file must now contain valid JSON.
	finalData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read recovered task state: %v", err)
	}

	var finalState TaskState

	if err := json.Unmarshal(finalData, &finalState); err != nil {
		t.Fatalf("recovered task state is invalid JSON: %v", err)
	}

	if finalState.State != StateDone {
		t.Fatalf(
			"expected recovered persisted state DONE, got %s",
			finalState.State,
		)
	}

	if finalState.TaskHash != taskHash(task) {
		t.Fatalf("recovered task hash mismatch")
	}

	if requests != 3 {
		t.Fatalf("expected 3 Ollama requests after recovery, got %d", requests)
	}
}
