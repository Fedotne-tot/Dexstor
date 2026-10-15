package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestNonGitAutonomousEndToEnd(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go так, чтобы add(2, 3) возвращала 10. Не ломай test.go."

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

		// State-based mock (Perl migration): the FSM path depends on the
		// controller state advertised in the prompt, not a fixed request counter.
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
		Max:       10,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	if err := agent.Run(task); err != nil {
		t.Fatalf("non-git autonomous run failed: %v", err)
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

	foundGitDiff := false

	for _, item := range agent.ActionLedger {
		if item.Tool == "git_diff" &&
			item.OK &&
			item.Exit == 0 {
			foundGitDiff = true
		}
	}

	if !foundGitDiff {
		t.Fatal("expected successful git_diff in non-git project")
	}
}
