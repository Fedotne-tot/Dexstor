package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/ollama"
	"bratan-go/internal/projectindex"
	"bratan-go/internal/tools"
)

/*
Regression tests for the TEST ledger recording bug.

The controller used to store only result.Status ("PASS"/"FAIL") in the
Result field of a "test" ActionLedger record. Every diagnostic produced
by the test command was therefore lost from the persistent ledger, while
all completion validators and repair heuristics scan record.Result for
substrings of the real output ("PASS", "WRONG:", "TEST FAILED").

The fix records the combined command output (falling back to Status
only when the output is empty). These tests pin that contract so the
bug cannot silently return.

Style note: these tests follow the existing httptest-based pattern of
run_repair_test.go / controller_write_snapshot_test.go. The agent is
started from a persisted StateTest checkpoint (the same mechanism used
by task_state_atomic_crash_test.go), so the deterministic TEST
controller state runs without any model request at all.
*/

func findTestRecords(a *Agent) []ActionRecord {
	var records []ActionRecord

	for _, record := range a.ActionLedger {
		if record.Tool == "test" {
			records = append(records, record)
		}
	}

	return records
}

// newFailingProject creates math.pl + test.pl where add(2,3)=5 but the
// test demands 10, exactly like run_repair_test.go. The failure marker
// "WRONG: 5" lives only inside the perl diagnostics, never in the
// coarse PASS/FAIL status.
func newFailingProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

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

	return root
}

// persistTestCheckpoint writes a verified TaskState whose FSM sits in
// StateTest with a completed write + diff phase, so Run() resumes
// directly into the deterministic TEST controller state.
func persistTestCheckpoint(t *testing.T, root, task string) *TaskState {
	t.Helper()

	mathData, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	mathContent := string(mathData)

	index, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateTest,
		Iteration: 4,

		ProjectIndex: index,

		ReadFiles: map[string]string{
			"math.pl": mathContent,
		},
		FileHashes: map[string]string{
			"math.pl": contentHash(mathContent),
		},
		ChangedFiles: []string{"math.pl"},

		LastWritePath: "math.pl",

		History: []string{
			"inspect_project",
			"read_file:math.pl",
			"write_file:math.pl",
			"git_diff",
			"test:test.pl",
		},
		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "project inspected",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "math.pl"),
				Args:      []string{"math.pl"},
				Result:    mathContent,
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "write_file",
				ActionKey: makeActionKey("write_file", "math.pl"),
				Args:      []string{"math.pl"},
				BaseHash:  contentHash(mathContent),
				Result:    "ok",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 3,
				Tool:      "git_diff",
				ActionKey: makeActionKey("git_diff"),
				Result:    "modified:\n  math.pl\n",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 4,
				Tool:      "test",
				ActionKey: makeActionKey("test", "test.pl"),
				Args:      []string{"test.pl"},
				/*
					Buggy legacy shape: only the coarse status was
					recorded here before the fix.
				*/
				Result: "FAIL",
				Error:  "exit status 9",
				OK:     false,
				Exit:   9,
			},
		},

		Wrote:       true,
		DiffChecked: true,
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("persist TEST checkpoint: %v", err)
	}

	return state
}

func failFirstRequestServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("TEST checkpoint resume must not request the model, got %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": ""})
	}))

	return server
}

func TestControllerStateTestRecordsFullOutputInLedger(t *testing.T) {
	task := "Fix math.pl so add(2, 3) returns 10"
	root := newFailingProject(t)

	persistTestCheckpoint(t, root, task)

	server := failFirstRequestServer(t)
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

	/*
		The resumed failing test moves the FSM to REPAIR_READ, not an
		error: the run continues deterministically until the next model
		request (which the fake server would reject, ending the run).
	*/
	_ = agent.Run(task)

	records := findTestRecords(agent)
	if len(records) != 2 {
		t.Fatalf("expected original plus one new test ledger record, got %d", len(records))
	}

	record := records[1]

	if record.Result == "FAIL" || record.Result == "PASS" {
		t.Fatalf(
			"test ledger recorded coarse status instead of output: %q",
			record.Result,
		)
	}

	if !strings.Contains(record.Result, "WRONG: 5") {
		t.Fatalf(
			"test ledger must contain full command output, got %q",
			record.Result,
		)
	}

	if record.OK || record.Exit == 0 {
		t.Fatal("failing test must be recorded as failed action")
	}

	/*
		The recorded result must survive the same integrity checks the
		persisted ledger goes through on resume.
	*/
	if err := validateActionLedgerIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("recorded ledger fails integrity validation: %v", err)
	}

	if err := validateActionLedgerResultIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("recorded ledger fails result validation: %v", err)
	}

	if err := validateActionLedgerTransitionIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("recorded ledger fails transition validation: %v", err)
	}
}

func TestControllerStateTestRecordsPassOutputInLedger(t *testing.T) {
	task := "verify passing regression project"
	root := newFailingProject(t)

	persistTestCheckpoint(t, root, task)

	/*
		Success path: validators require "PASS" inside Result. The real
		test output contains PASS, so completion stays reachable while
		diagnostics are preserved. Repair math.pl so the test passes.
	*/
	repaired := `sub add {
    my ($a, $b) = @_;
    return 10;
}

use Exporter qw(import);
our @EXPORT_OK = qw(add);

1;
`

	if err := os.WriteFile(filepath.Join(root, "math.pl"), []byte(repaired), 0644); err != nil {
		t.Fatal(err)
	}

	server := failFirstRequestServer(t)
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

	if err := agent.Run(task); err != nil {
		t.Fatalf("passing test run returned unexpected error: %v", err)
	}

	records := findTestRecords(agent)
	if len(records) != 2 {
		t.Fatalf("expected original plus one new test ledger record, got %d", len(records))
	}

	record := records[1]

	if !strings.Contains(record.Result, "PASS") {
		t.Fatalf(
			"successful test ledger result must contain PASS, got %q",
			record.Result,
		)
	}

	if record.OK != true || record.Exit != 0 {
		t.Fatalf("passing test must be recorded as successful action: ok=%v exit=%d", record.OK, record.Exit)
	}

	if agent.State != StateDone {
		t.Fatalf("passing test must move FSM to DONE, got %s", agent.State)
	}

	if err := validateActionLedgerResultIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("pass ledger fails result validation: %v", err)
	}

	if err := validateActionLedgerTransitionIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("pass ledger fails transition validation: %v", err)
	}
}

func TestControllerStateTestFallsBackToStatusOnEmptyOutput(t *testing.T) {
	root := t.TempDir()

	/*
		Silent passing test: perl exits 0 with no output. The fallback
		keeps the invariant enforced by validateActionLedgerResultIntegrity
		— a successful test record must contain "PASS".
	*/
	if err := os.WriteFile(filepath.Join(root, "test.pl"), []byte("exit 0;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	index, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	task := "silent passing test fallback"

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateTest,
		Iteration: 3,

		ProjectIndex: index,

		ReadFiles:    map[string]string{},
		FileHashes:   map[string]string{},
		ChangedFiles: []string{},

		LastWritePath: "test.pl",

		History: []string{
			"inspect_project",
			"write_file:test.pl",
			"git_diff",
		},
		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "project inspected",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 1,
				Tool:      "write_file",
				ActionKey: makeActionKey("write_file", "test.pl"),
				Args:      []string{"test.pl"},
				BaseHash:  contentHash("exit 0;\n"),
				Result:    "ok",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "git_diff",
				ActionKey: makeActionKey("git_diff"),
				Result:    "added:\n  test.pl\n",
				OK:        true,
				Exit:      0,
			},
		},

		Wrote:       true,
		DiffChecked: true,
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("persist TEST checkpoint: %v", err)
	}

	server := failFirstRequestServer(t)
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

	if err := agent.Run(task); err != nil {
		t.Fatalf("silent passing test run returned unexpected error: %v", err)
	}

	records := findTestRecords(agent)
	if len(records) != 1 {
		t.Fatalf("expected exactly one test ledger record, got %d", len(records))
	}

	record := records[0]

	if record.Result != "PASS" {
		t.Fatalf(
			"empty test output must fall back to status, got %q",
			record.Result,
		)
	}

	if err := validateActionLedgerResultIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("fallback ledger fails result validation: %v", err)
	}
}
