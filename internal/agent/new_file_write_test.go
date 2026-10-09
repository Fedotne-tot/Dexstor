package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestNewFileWriteDoesNotRequireReadSnapshot(t *testing.T) {
	root := t.TempDir()

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
			t.Fatal(err)
		}

		requests++

		var response string

		switch requests {
		case 1:
			response = `TOOL_CALL
name: inspect_project
path: .
END_TOOL_CALL`

		case 2:
			response = `TOOL_CALL
name: write_file
path: hello.txt
content: HELLO_BRATAN
END_TOOL_CALL`

		default:
			t.Fatalf("unexpected model request %d", requests)
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
		})
	}))

	defer server.Close()

	agent := &Agent{
		Model:     "test-model",
		Max:       5,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	err := agent.Run("создай файл hello.txt содержащий HELLO_BRATAN")
	if err != nil {
		t.Fatalf("new file task failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("hello.txt was not created: %v", err)
	}

	if string(data) != "HELLO_BRATAN" {
		t.Fatalf(
			"unexpected file content: %q",
			string(data),
		)
	}

	if len(agent.ReadFiles) != 0 {
		t.Fatalf(
			"new file creation unexpectedly required read snapshot: %+v",
			agent.ReadFiles,
		)
	}

	expected := []string{
		"inspect_project",
		"write_file:hello.txt",
		"git_diff",
		"test:hello.txt",
	}

	if len(agent.History) != len(expected) {
		t.Fatalf(
			"unexpected history: %+v",
			agent.History,
		)
	}

	for i := range expected {
		if agent.History[i] != expected[i] {
			t.Fatalf(
				"history mismatch at %d: got %q want %q",
				i,
				agent.History[i],
				expected[i],
			)
		}
	}
}
