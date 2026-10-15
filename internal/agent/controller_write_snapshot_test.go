package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestControllerRejectsWriteForUnreadExistingFile(t *testing.T) {
	root := t.TempDir()

	task := `Исправь math.go, сохранив корректную функцию add.`

	if err := os.WriteFile(
		filepath.Join(root, "TASK.md"),
		[]byte(task),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	math := `package main

func Add(a, b int) int {
	return a + b
}
`

	mathPath := filepath.Join(root, "math.go")

	if err := os.WriteFile(
		mathPath,
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "DEXSTER Test"},
		{"git", "-C", root, "add", "TASK.md", "math.go"},
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

		requests++

		var response string

		switch requests {
		case 1:
			response = `TOOL_CALL
name: inspect_project`

		case 2:
			response = `TOOL_CALL
name: read_file
arg: TASK.md`

		case 3:
			response = `TOOL_CALL
name: write_file
path: math.go
content: BROKEN CONTENT`

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

	if err == nil {
		t.Fatal("expected unread write_file to fail")
	}

	if requests != 3 {
		t.Fatalf("expected exactly 3 Ollama requests, got %d", requests)
	}

	content, err := os.ReadFile(mathPath)

	if err != nil {
		t.Fatal(err)
	}

	if string(content) != math {
		t.Fatal("math.go was modified despite missing trusted snapshot")
	}

	for _, record := range agent.ActionLedger {
		if record.Tool == "write_file" && record.OK {
			t.Fatal("unread write_file must not be recorded as successful")
		}
	}
}
