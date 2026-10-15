package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestOneFileTaskDoesNotRequireArtificialSecondRead(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go. В этом проекте только один исходный файл."

	math := `package main

func Add(a, b int) int {
	return a + b
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "DEXSTER Test"},
		{"git", "-C", root, "add", "math.go"},
		{"git", "-C", root, "commit", "-m", "initial state"},
	}

	for _, cmd := range gitCommands {
		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", cmd, err, output)
		}
	}

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

		requests++

		var response string

		switch requests {
		case 1:
			response = `TOOL_CALL
name: inspect_project`

		case 2:
			response = `TOOL_CALL
name: read_file
arg: math.go`

		case 3:
			/*
				One source file is enough to establish the write
				snapshot.

				The controller must not force another read merely
				because len(ReadFiles) == 1.
			*/
			response = `package main

func Add(a, b int) int {
	return a + b
}
`

		default:
			t.Fatalf("unexpected Ollama request #%d", requests)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
		})
	}))
	defer server.Close()

	agent := &Agent{
		Model:     "test-model",
		Max:       8,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	err := agent.Run(task)

	/*
		The current implementation is expected to expose the
		artificial len(ReadFiles) >= 2 transition.

		The architectural requirement is that one source file
		can progress:

		    DISCOVER
		    -> IMPLEMENT
		    -> read_file
		    -> WRITE_REQUIRED
		    -> write_file
		    -> VERIFY_DIFF

		without a mandatory second read.
	*/

	if err != nil {
		t.Fatalf("one-file task should be able to proceed to write: %v", err)
	}

	for _, record := range agent.History {
		if strings.HasPrefix(record, "read_file:") &&
			record != "read_file:math.go" {
			t.Fatalf("unexpected second file read: %v", agent.History)
		}
	}
}
