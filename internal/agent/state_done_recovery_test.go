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

func TestStateDoneRecoveryWithoutTestEvidence(t *testing.T) {
	root := t.TempDir()

	content := "package main\n\nfunc main() {}\n"

	if err := os.WriteFile(
		filepath.Join(root, "main.go"),
		[]byte(content),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
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

	task := "finish the task"
	hash := contentHash(content)

	agent := &Agent{
		Model:  "test-model",
		Max:    20,
		Root:   root,
		Router: tools.New(root),
		Ollama: ollama.New(server.URL),

		State: StateDone,

		ReadFiles: map[string]string{
			"main.go": content,
		},

		History: []string{
			"inspect_project",
			"read_file:main.go",
			"write_file:main.go",
			"git_diff",
		},

		ActionLedger: []ActionRecord{
			{
				ActionKey: makeActionKey("inspect_project"),
				Tool:      "inspect_project",
				Iteration: 1,
				OK:        true,
				Exit:      0,
				Result:    "main.go",
			},
			{
				ActionKey: makeActionKey("read_file", "main.go"),
				Tool:      "read_file",
				Args:      []string{"main.go"},
				Iteration: 2,
				OK:        true,
				Exit:      0,
				Result:    content,
			},
			{
				ActionKey: makeActionKey(
					"write_file",
					"main.go",
					content,
				),
				Tool:      "write_file",
				Args:      []string{"main.go", content},
				Iteration: 3,
				OK:        true,
				Exit:      0,
				Result:    "written",
				BaseHash:  hash,
			},
			{
				ActionKey: makeActionKey("git_diff"),
				Tool:      "git_diff",
				Iteration: 4,
				OK:        true,
				Exit:      0,
				Result:    "",
			},
		},

		LastAction:    "git_diff",
		LastResult:    "",
		LastWritePath: "main.go",

		Wrote:       true,
		DiffChecked: true,
	}

	if err := agent.saveState(task, 4); err != nil {
		t.Fatalf("save DONE state: %v", err)
	}

	recovered := &Agent{
		Model:  "test-model",
		Max:    20,
		Root:   root,
		Router: tools.New(root),
		Ollama: ollama.New(server.URL),
	}

	err := recovered.Run(task)
	if err == nil {
		t.Fatal("SECURITY REGRESSION: persisted StateDone was accepted without successful test evidence")
	}

	if !strings.Contains(
		err.Error(),
		"completion blocked: successful test PASS was not recorded",
	) {
		t.Fatalf(
			"unexpected recovery error: %v",
			err,
		)
	}

	t.Logf("PROOF GUARD: persisted StateDone rejected: %v", err)

	if recovered.State != StateDone {
		t.Fatalf(
			"expected StateDone, got %s",
			recovered.State,
		)
	}

	if requests != 0 {
		t.Fatalf(
			"persisted StateDone recovery contacted Ollama %d times, want 0",
			requests,
		)
	}

	t.Log(
		"PROOF: Run() rejects persisted StateDone without successful test evidence",
	)
}
