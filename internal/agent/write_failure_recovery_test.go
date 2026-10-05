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

	task := "Inspect test.pl and attempt to write the same content back to test.pl."

	testProgram := `require "./math.pl";

my $result = add(2, 3);

die "WRONG: $result\n" unless $result == 10;

print "PASS\n";
`

	if err := os.WriteFile(
		filepath.Join(root, "math.pl"),
		[]byte("sub add { return 5; }\n1;\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.pl"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	original, err := os.ReadFile(filepath.Join(root, "test.pl"))
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
path: test.pl
END_TOOL_CALL`

		case 3:
			response = `TOOL_CALL
name: write_file
path: test.pl
content: require "./math.pl";

my $result = add(2, 3);

die "WRONG: $result\n" unless $result == 10;

print "PASS\n";
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

	if agent.State != StateImplement {
		t.Fatalf(
			"expected state IMPLEMENT at failure, got %s",
			agent.State,
		)
	}

	current, err := os.ReadFile(filepath.Join(root, "test.pl"))
	if err != nil {
		t.Fatal(err)
	}

	if string(current) != string(original) {
		t.Fatalf(
			"protected test.pl changed after failed write:\n%s",
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
