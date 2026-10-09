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

func TestNonGitAutonomousEndToEnd(t *testing.T) {
	root := t.TempDir()

	task := "Исправь math.pl так, чтобы add(2, 3) возвращала 10. Не ломай test.pl."

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

	if err := os.WriteFile(filepath.Join(root, "math.pl"), []byte(math), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "test.pl"), []byte(testProgram), 0644); err != nil {
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
			response = `TOOL_CALL
name: inspect_project`
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

	if err := agent.Run(task); err != nil {
		t.Fatalf("non-git autonomous run failed: %v", err)
	}

	if agent.State != StateDone {
		t.Fatalf("expected StateDone, got %s", agent.State)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "return 10;") {
		t.Fatalf("math.pl was not repaired:\n%s", data)
	}

	foundGitDiff := false

	for _, item := range agent.ActionLedger {
		if item.Tool == "git_diff" &&
			item.OK &&
			item.Exit == 0 {
			foundGitDiff = true
		}
	}

	if !foundGitDiff {
		t.Fatal("expected successful git_diff in non-git project")
	}
}
