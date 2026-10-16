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

		var state string
		if i := strings.Index(req.Prompt, "Current state:"); i >= 0 {
			rest := req.Prompt[i+len("Current state:"):]
			if j := strings.IndexAny(rest, "\r\n"); j >= 0 {
				rest = rest[:j]
			}
			state = strings.TrimSpace(rest)
		}

		isNudge := strings.Contains(req.Prompt, "NUDGE:")

		switch {
		case state == "DISCOVER":
			response = `TOOL_CALL
name: inspect_project
path: .
depth: 3
END_TOOL_CALL`

		case state == "IMPLEMENT" && n == 2:
			// Read the primary source file first. This satisfies the CAS
			// trusted-snapshot requirement for the later write to math.go
			// (writing an existing file without a snapshot is rejected by
			// design and must stay rejected). One read is enough — the
			// single-file task contract forbids an artificial second read.
			response = `TOOL_CALL
name: read_file
path: math.go
END_TOOL_CALL`

		case state == "IMPLEMENT" && n == 3:
			// After math.go was read, the model attempts to overwrite the
			// protected root test program directly from IMPLEMENT. This is
			// the scenario under test: the FSM must reject it with the
			// canonical Router error "protected file cannot be modified"
			// BEFORE the CAS/snapshot preflight (fix #6 ordering contract),
			// even though test.go exists on disk without a trusted read
			// snapshot, and the run must stop immediately instead of
			// retrying through the model.
			response = `TOOL_CALL
name: write_file
path: test.go
content: package main

func main() {
	panic("silent")
}
END_TOOL_CALL`

		case state == "WRITE_REQUIRED" && !isNudge:
			// The IMPLEMENT turn above moved the FSM into WRITE_REQUIRED
			// (one trusted read is sufficient per the single-file task
			// contract). The corrective model turn repeats the same
			// legitimate intent: overwrite the protected root test
			// program. The Router rejection must fire here too — before
			// any baseHash/CAS preflight — and terminate the run.
			response = `TOOL_CALL
name: write_file
path: test.go
content: package main

func main() {
	panic("silent")
}
END_TOOL_CALL`

		default:
			// Safety net: any further request (VERIFY_DIFF, TEST, REPAIR_*,
			// DONE, ...) means the controller did NOT stop after the
			// protected-path failure (retry-forever regression). Return an
			// empty response; the request-counter and error assertions
			// below fail loudly.
			response = ""
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

	if requests != 3 {
		t.Fatalf(
			"expected exactly 3 Ollama requests, got %d",
			requests,
		)
	}

	if agent.State != StateWriteReq {
		t.Fatalf(
			"expected state WRITE_REQUIRED at failure, got %s",
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
