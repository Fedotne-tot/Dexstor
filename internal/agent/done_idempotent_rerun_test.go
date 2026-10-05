package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/tools"
)

func TestDoneStateRerunIsIdempotent(t *testing.T) {
	root := t.TempDir()

	math := `sub add {
    my ($a, $b) = @_;
    return 10;
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

	var mu sync.Mutex
	requests := 0

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/generate" {
				http.NotFound(w, r)
				return
			}

			mu.Lock()
			requests++
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")

			_ = json.NewEncoder(w).Encode(
				map[string]string{
					"response": "",
				},
			)
		},
	))
	defer server.Close()

	task := "Fix math.pl so add(2, 3) returns 10"

	newAgent := func() *Agent {
		return &Agent{
			Model:     "test-model",
			Max:       20,
			Root:      root,
			Router:    tools.New(root),
			Ollama:    ollama.New(server.URL),
			State:     StateDone,
			ReadFiles: make(map[string]string),
		}
	}

	first := newAgent()

	writeArgs := []string{
		"math.pl",
		math,
	}

	first.ActionLedger = []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("inspect_project"),
			Result:    "math.pl\ntest.pl",
		},
		{
			Iteration: 2,
			Tool:      "read_file",
			Args:      []string{"math.pl"},
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("read_file", "math.pl"),
			Result:    math,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			Args:      writeArgs,
			BaseHash:  contentHash(math),
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("write_file", writeArgs...),
			Result:    "written",
		},
		{
			Iteration: 4,
			Tool:      "git_diff",
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("git_diff"),
			Result:    "diff checked",
		},
		{
			Iteration: 5,
			Tool:      "test",
			Args:      []string{"test.pl"},
			OK:        true,
			Exit:      0,
			ActionKey: makeActionKey("test", "test.pl"),
			Result:    "PASS\n",
		},
	}

	first.History = []string{
		"inspect_project",
		"read_file:math.pl",
		"write_file:math.pl",
		"git_diff",
		"test:test.pl",
	}

	first.Wrote = true
	first.DiffChecked = true
	first.LastWritePath = "math.pl"
	first.LastResult = "PASS"
	first.LastAction = "test:test.pl"

	if err := first.saveState(task, 10); err != nil {
		t.Fatalf("save DONE state: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	second := newAgent()

	if err := second.Run(task); err != nil {
		t.Fatalf("rerun from DONE failed: %v", err)
	}

	if second.State != StateDone {
		t.Fatalf(
			"expected StateDone after idempotent rerun, got %s",
			second.State,
		)
	}

	if requests != 0 {
		t.Fatalf(
			"DONE rerun contacted Ollama %d times, want 0",
			requests,
		)
	}

	after, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != string(before) {
		t.Fatal("DONE rerun modified math.pl")
	}

	if len(second.History) != 5 {
		t.Fatalf(
			"DONE rerun changed action history: %v",
			second.History,
		)
	}

	t.Log("DONE rerun is idempotent")
}
