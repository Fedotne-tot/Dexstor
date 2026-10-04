package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestCorruptedTaskStateRecovery(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.pl так, чтобы add(2, 3) возвращала 10."

	math := `sub add {
    my ($a, $b) = @_;
    return $a + $b;
}

use Exporter qw(import);
our @EXPORT_OK = qw(add);

1;
`

	testProgram := `require "./math.pl";

my $result = add(2, 3);

die "WRONG: $result\n" unless $result == 10;

print "PASS\n";
`

	if err := os.WriteFile(
		filepath.Join(root, "math.pl"),
		[]byte(math),
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

	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "BRATAN Test"},
		{"git", "-C", root, "add", "math.pl", "test.pl"},
		{"git", "-C", root, "commit", "-m", "initial corrupted-state test"},
	}

	for _, cmd := range gitCommands {
		output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", cmd, err, output)
		}
	}

	// Deliberately corrupt the persisted state.
	statePath := taskStatePath(root)

	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		statePath,
		[]byte(`{"version":3,"task":`),
		0644,
	); err != nil {
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
name: inspect_project`

		case 2:
			response = `TOOL_CALL
name: read_file
arg: math.pl`

		case 3:
			response = `TOOL_CALL
name: write_file
path: math.pl
content: sub add {
    my ($a, $b) = @_;
    return 10;
}

use Exporter qw(import);
our @EXPORT_OK = qw(add);

1;
`

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
		Max:       20,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	if err := agent.Run(task); err != nil {
		t.Fatalf("agent failed to recover from corrupted state: %v", err)
	}

	if agent.State != StateDone {
		t.Fatalf("expected StateDone after recovery, got %s", agent.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10;") {
		t.Fatalf("math.pl was not repaired:\n%s", data)
	}

	// The corrupted state must have been preserved rather than silently
	// destroyed.
	corruptPath := statePath + ".corrupt"

	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("expected corrupted state backup %s: %v", corruptPath, err)
	}

	// The main state file must now contain valid JSON.
	finalData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read recovered task state: %v", err)
	}

	var finalState TaskState

	if err := json.Unmarshal(finalData, &finalState); err != nil {
		t.Fatalf("recovered task state is invalid JSON: %v", err)
	}

	if finalState.State != StateDone {
		t.Fatalf(
			"expected recovered persisted state DONE, got %s",
			finalState.State,
		)
	}

	if finalState.TaskHash != taskHash(task) {
		t.Fatalf("recovered task hash mismatch")
	}

	if requests != 3 {
		t.Fatalf("expected 3 Ollama requests after recovery, got %d", requests)
	}
}
