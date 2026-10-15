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

func TestWriteFileCannotModifyTestFile(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go. test.go является защищённым тестом и не должен изменяться."

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

	originalTest, err := os.ReadFile(filepath.Join(root, "test.go"))
	if err != nil {
		t.Fatal(err)
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
			response = `TOOL_CALL
name: read_file
arg: test.go`

		case 4:
			// Deliberately attempt to modify the protected test file.
			response = `TOOL_CALL
name: write_file
path: test.go
content: THIS FILE MUST NOT BE MODIFIED
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
		Max:       4,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	err = agent.Run(task)

	// The attempted protected write must not be accepted.
	if err == nil {
		t.Fatal("expected protected test.go write to be rejected")
	}

	afterTest, err := os.ReadFile(filepath.Join(root, "test.go"))
	if err != nil {
		t.Fatal(err)
	}

	if string(afterTest) != string(originalTest) {
		t.Fatalf(
			"protected test.go was modified\noriginal:\n%s\nafter:\n%s",
			originalTest,
			afterTest,
		)
	}

	if strings.Contains(string(afterTest), "THIS FILE MUST NOT BE MODIFIED") {
		t.Fatal("protected test.go contains injected write content")
	}
}
