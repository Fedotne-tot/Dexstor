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

func TestAutonomousEndToEnd(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go так, чтобы add(2, 3) возвращала 10. Не ломай test.go. Используй read_file/write_file/git_diff/test."

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

	// Real Git repository: exercise the real git_diff tool.
	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "BRATAN Test"},
		{"git", "-C", root, "add", "math.go", "test.go"},
		{"git", "-C", root, "commit", "-m", "initial e2e state"},
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

		var response string

		/*
			State-based mock: the FSM reacts to the "Current state:" marker
			embedded in the production prompt instead of request numbers.
			WRITE_REQUIRED must answer with a write_file TOOL_CALL; bare Go
			code is not a tool call and gets blocked forever in that state.
		*/
		_ = n

		switch {
		case strings.Contains(req.Prompt, "Current state: DISCOVER"):
			response = `TOOL_CALL
name: inspect_project`

		case strings.Contains(req.Prompt, "Current state: IMPLEMENT"),
			strings.Contains(req.Prompt, "Current state is: IMPLEMENT"):
			response = `TOOL_CALL
name: read_file
arg: math.go`

		case strings.Contains(req.Prompt, "Current state: WRITE_REQUIRED"):
			response = `TOOL_CALL
name: write_file
path: math.go
content: package main

func Add(a, b int) int {
	return 10
}
`

		default:
			// Deterministic states should never ask the model; an inert
			// inspect call gets blocked without being recorded.
			response = `TOOL_CALL
name: inspect_project`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
		})
	}))
	defer server.Close()

	newAgent := func() *Agent {
		return &Agent{
			Model:     "test-model",
			Max:       20,
			Root:      root,
			Router:    tools.New(root),
			Ollama:    ollama.New(server.URL),
			State:     StateDiscover,
			ReadFiles: make(map[string]string),
		}
	}

	// ------------------------------------------------------------------
	// RUN #1: full autonomous path to DONE.
	// ------------------------------------------------------------------

	agent1 := newAgent()

	if err := agent1.Run(task); err != nil {
		t.Fatalf("first autonomous run failed: %v", err)
	}

	if agent1.State != StateDone {
		t.Fatalf("first run: expected StateDone, got %s", agent1.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10") {
		t.Fatalf("first run did not repair math.go:\n%s", data)
	}

	state, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}

	if !ok {
		t.Fatal("expected persisted task state after first run")
	}

	if state.State != StateDone {
		t.Fatalf("persisted state: expected DONE, got %s", state.State)
	}

	if state.Iteration != 6 {
		t.Fatalf("persisted state: expected iteration 6, got %d", state.Iteration)
	}

	firstRunRequests := requests

	if firstRunRequests != 4 {
		t.Fatalf("first run: expected 4 Ollama requests, got %d", firstRunRequests)
	}

	// ------------------------------------------------------------------
	// EXTERNAL CHANGE: invalidate the completed task after persistence.
	// ------------------------------------------------------------------

	brokenMath := strings.Replace(
		string(data),
		"return 10",
		"return 4",
		1,
	)

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(brokenMath),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// ------------------------------------------------------------------
	// RUN #2: same task, persisted DONE, but filesystem is stale.
	// Reconcile must reject DONE and restart the FSM.
	// ------------------------------------------------------------------

	agent2 := newAgent()

	if err := agent2.Run(task); err != nil {
		t.Fatalf("second autonomous run failed: %v", err)
	}

	if agent2.State != StateDone {
		t.Fatalf("second run: expected StateDone, got %s", agent2.State)
	}

	data, err = os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10") {
		t.Fatalf("second run did not repair externally changed math.go:\n%s", data)
	}

	state, ok, err = loadTaskState(root, task)
	if err != nil {
		t.Fatalf("load final persisted state: %v", err)
	}

	if !ok {
		t.Fatal("expected final persisted task state")
	}

	if state.State != StateDone {
		t.Fatalf("final persisted state: expected DONE, got %s", state.State)
	}

	totalRequests := requests

	if totalRequests != 8 {
		t.Fatalf("expected 8 total Ollama requests across two runs, got %d", totalRequests)
	}

	// Second run must have executed the full model-driven discovery path.
	if len(agent2.History) != 5 {
		t.Fatalf(
			"second run: expected 5 actions, got %d: %v",
			len(agent2.History),
			agent2.History,
		)
	}

	wantHistory := []string{
		"inspect_project",
		"read_file:math.go",
		"write_file:math.go",
		"git_diff",
		"test:test.go",
	}

	// The controller records exactly these successful actions for the
	// normal path. Compare the prefix that represents the workflow.
	for i, want := range wantHistory {
		if i >= len(agent2.History) {
			t.Fatalf("second run history missing action %q", want)
		}

		if agent2.History[i] != want {
			t.Fatalf(
				"second run history[%d]: got %q, want %q; history=%v",
				i,
				agent2.History[i],
				want,
				agent2.History,
			)
		}
	}

	t.Logf("first run history: %v", agent1.History)
	t.Logf("second run history: %v", agent2.History)
	t.Logf("total Ollama requests: %d", totalRequests)
}
