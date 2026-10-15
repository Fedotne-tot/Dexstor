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

func TestAssertOutputCannotCompleteTaskWithoutMandatoryVerification(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.go. test.go является тестом."

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
		{"git", "-C", root, "config", "user.name", "DEXSTER Test"},
		{"git", "-C", root, "add", "math.go", "test.go"},
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
				assert_output deliberately succeeds.

				The important invariant is that this must NOT be
				enough to reach StateDone.
			*/
			response = `TOOL_CALL
name: assert_output
command: printf 'PASS\n'
expected: PASS`

		case 4:
			/*
				After assert_output the controller must request another
				model response in WRITE_REQUIRED.

				Deliberately return a forbidden tool here. The purpose of
				this test is not to complete the task, but to prove that
				assert_output did not terminate the FSM.
			*/
			response = `TOOL_CALL
name: inspect_project`

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

	err := agent.Run(task)

	/*
		Architectural requirement:

		    assert_output PASS
		        must not
		    transition directly to DONE.

		The mandatory completion chain remains:

		    write_file
		    -> git_diff
		    -> test
		    -> validateCompletion
		    -> DONE

		The fourth model response deliberately requests inspect_project,
		which is forbidden in WRITE_REQUIRED. Therefore Run must reject
		the task instead of completing it.
	*/

	if err == nil {
		t.Fatal("expected WRITE_REQUIRED to reject non-write tool")
	}

	if agent.State == StateDone {
		t.Fatal("assert_output must not transition agent directly to DONE")
	}

	if agent.State != StateWriteReq {
		t.Fatalf(
			"expected agent to remain in WRITE_REQUIRED, got %s",
			agent.State,
		)
	}

	for _, record := range agent.History {
		if record == "write_file" ||
			strings.HasPrefix(record, "write_file:") {
			t.Fatalf("assert_output-only task unexpectedly performed write: %v", agent.History)
		}

		if record == "git_diff" {
			t.Fatalf("assert_output-only task unexpectedly performed git_diff: %v", agent.History)
		}

		if strings.HasPrefix(record, "test:") {
			t.Fatalf("assert_output-only task unexpectedly performed test: %v", agent.History)
		}
	}
}
