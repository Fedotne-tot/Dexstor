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

func TestWriteFileRejectsStaleSnapshotAtFSM(t *testing.T) {
	root := t.TempDir()

	task := "Inspect math.pl, read it, then write the same content back."

	original := "sub add { return 5; }\n1;\n"
	external := "sub add { return 999; }\n1;\n"

	path := filepath.Join(root, "math.pl")

	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
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

		var response string

		switch n {
		case 1:
			response = `TOOL_CALL
name: inspect_project
path: .
depth: 3
END_TOOL_CALL`

		case 2:
			response = `TOOL_CALL
name: read_file
path: math.pl
END_TOOL_CALL`

		case 3:
			// Simulate an external change after the trusted read
			// but before the FSM executes the requested write.
			if err := os.WriteFile(path, []byte(external), 0644); err != nil {
				t.Fatalf("external mutation failed: %v", err)
			}

			response = `TOOL_CALL
name: write_file
path: math.pl
content: sub add { return 5; }
1;
END_TOOL_CALL`

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
		Max:       10,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	err := agent.Run(task)
	if err == nil {
		t.Fatal("expected stale write to fail")
	}

	if !strings.Contains(err.Error(), "write_file failed") {
		t.Fatalf(
			"expected write_file failure error, got: %v",
			err,
		)
	}

	if requests != 3 {
		t.Fatalf(
			"expected exactly 3 Ollama requests, got %d",
			requests,
		)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(current) != external {
		t.Fatalf(
			"stale FSM write modified externally changed file:\n%s",
			current,
		)
	}

	var failedWrite bool

	for _, action := range agent.ActionLedger {
		if action.Tool != "write_file" {
			continue
		}

		if action.OK {
			t.Fatalf(
				"stale write_file was recorded as successful: %+v",
				action,
			)
		}

		if action.Exit == 0 {
			t.Fatalf(
				"stale write_file has successful exit code: %+v",
				action,
			)
		}

		if !strings.Contains(action.Error, "stale snapshot") {
			t.Fatalf(
				"expected stale snapshot error, got: %+v",
				action,
			)
		}

		failedWrite = true
	}

	if !failedWrite {
		t.Fatal("no failed write_file action was recorded")
	}
}
