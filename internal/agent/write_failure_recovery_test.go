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

func TestWriteFileFailureDoesNotRetryForever(t *testing.T) {
	root := t.TempDir()

	task := "Inspect test.go and attempt to write the same content back to test.go."

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
		[]byte("package main\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"),
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

	original, err := os.ReadFile(filepath.Join(root, "test.go"))
	if err != nil {
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
path: test.go
END_TOOL_CALL`

		case 3:
			// NUDGE (state-based): the model produced bare code in
			// IMPLEMENT; the controller asks again for a TOOL_CALL.
			response = `TOOL_CALL
name: read_file
path: math.go
END_TOOL_CALL`

		case 4:
			response = `TOOL_CALL
name: write_file
path: math.go
content: package main

func Add(a, b int) int {
	return 5
}
END_TOOL_CALL`

		default:
			// REPAIR_WRITE turn: the model proposes to repair the
			// protected root test file (test.go is the Go successor
			// of the historical protected test.pl). The Router must
			// reject the write ("protected file cannot be modified")
			// and the controller must stop the run with a
			// "write_file failed" error instead of retrying forever
			// through the model.
			response = `TOOL_CALL
name: write_file
path: test.go
content: package main

func main() {
	panic("silent")
}
END_TOOL_CALL`
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

	err = agent.Run(task)
	if err == nil {
		t.Fatal("expected write_file failure to stop the agent")
	}

	if !strings.Contains(err.Error(), "write_file failed") {
		t.Fatalf(
			"expected write_file failure error, got: %v",
			err,
		)
	}

	if requests != 4 {
		t.Fatalf(
			"expected exactly 4 Ollama requests, got %d",
			requests,
		)
	}

	if agent.State != StateImplement {
		t.Fatalf(
			"expected state IMPLEMENT at failure, got %s",
			agent.State,
		)
	}

	current, err := os.ReadFile(filepath.Join(root, "test.go"))
	if err != nil {
		t.Fatal(err)
	}

	if string(current) != string(original) {
		t.Fatalf(
			"protected test.go changed after failed write:\n%s",
			current,
		)
	}

	for _, action := range agent.ActionLedger {
		if action.Tool == "write_file" {
			if action.OK {
				t.Fatalf(
					"protected write_file was recorded as successful: %+v",
					action,
				)
			}

			if action.Exit == 0 {
				t.Fatalf(
					"failed write_file has successful exit code: %+v",
					action,
				)
			}
		}
	}
}
