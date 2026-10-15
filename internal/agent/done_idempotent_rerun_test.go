package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestDoneStateRerunIsIdempotent(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
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

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/generate" {
				http.NotFound(w, r)
				return
			}

			mu.Lock()
			requests++
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")

			_ = json.NewEncoder(w).Encode(
				map[string]string{
					"response": "",
				},
			)
		},
	))
	defer server.Close()

	task := "Fix math.go so add(2, 3) returns 10"

	newAgent := func() *Agent {
		return &Agent{
			Model:     "test-model",
			Max:       20,
			Root:      root,
			Router:    tools.New(root),
			Ollama:    ollama.New(server.URL),
			State:     StateDone,
			ReadFiles: make(map[string]string),
		}
	}

	first := newAgent()

	writeArgs := []string{
		"math.go",
		math,
	}

	first.ActionLedger = []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("inspect_project"),
			Result:    "math.go\ntest.go",
		},
		{
			Iteration: 2,
			Tool:      "read_file",
			Args:      []string{"math.go"},
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("read_file", "math.go"),
			Result:    math,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			Args:      writeArgs,
			BaseHash:  contentHash(math),
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("write_file", writeArgs...),
			Result:    "written",
		},
		{
			Iteration: 4,
			Tool:      "git_diff",
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("git_diff"),
			Result:    "diff checked",
		},
		{
			Iteration: 5,
			Tool:      "test",
			Args:      []string{"test.go"},
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("test", "test.go"),
			Result:    "PASS\n",
		},
	}

	first.History = []string{
		"inspect_project",
		"read_file:math.go",
		"write_file:math.go",
		"git_diff",
		"test:test.go",
	}

	first.Wrote = true
	first.DiffChecked = true
	first.LastWritePath = "math.go"
	first.LastResult = "PASS"
	first.LastAction = "test:test.go"

	if err := first.saveState(task, 10); err != nil {
		t.Fatalf("save DONE state: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	second := newAgent()

	if err := second.Run(task); err != nil {
		t.Fatalf("rerun from DONE failed: %v", err)
	}

	if second.State != StateDone {
		t.Fatalf(
			"expected StateDone after idempotent rerun, got %s",
			second.State,
		)
	}

	if requests != 0 {
		t.Fatalf(
			"DONE rerun contacted Ollama %d times, want 0",
			requests,
		)
	}

	after, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != string(before) {
		t.Fatal("DONE rerun modified math.go")
	}

	if len(second.History) != 5 {
		t.Fatalf(
			"DONE rerun changed action history: %v",
			second.History,
		)
	}

	t.Log("DONE rerun is idempotent")
}
