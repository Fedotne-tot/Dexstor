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

// newFailingProject creates math.go + test.go where add(2,3)=5 but the
// test demands 10, exactly like run_repair_test.go. The failure marker
// "WRONG: 5" lives only inside the perl diagnostics, never in the
// coarse PASS/FAIL status.
func newFailingProject(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

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

	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(math), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "test.go"), []byte(testProgram), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

// persistTestCheckpoint writes a verified TaskState whose FSM sits in
// StateTest with a completed write + diff phase, so Run() resumes
// directly into the deterministic TEST controller state.
func persistTestCheckpoint(t *testing.T, root, task string) *TaskState {
	t.Helper()

	mathData, err := os.ReadFile(filepath.Join(root, "math.go"))
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
			"math.go": mathContent,
		},
		FileHashes: map[string]string{
			"math.go": contentHash(mathContent),
		},
		ChangedFiles: []string{"math.go"},

		LastWritePath: "math.go",

		History: []string{
			"inspect_project",
			"read_file:math.go",
			"write_file:math.go",
			"git_diff",
			"test:test.go",
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
				ActionKey: makeActionKey("read_file", "math.go"),
				Args:      []string{"math.go"},
				Result:    mathContent,
				OK:        true,
				Exit:      0,
			},
			/*
				write_file ledger records must carry the full
				[path, content] argument pair:
				validateActionLedgerIntegrity rejects any other arity
				and recomputes ActionKey from Tool + Args.
			*/
			{
				Iteration: 2,
				Tool:      "write_file",
				ActionKey: makeActionKey("write_file", "math.go", mathContent),
				Args:      []string{"math.go", mathContent},
				BaseHash:  contentHash(mathContent),
				Result:    "ok",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 3,
				Tool:      "git_diff",
				ActionKey: makeActionKey("git_diff"),
				Result:    "modified:\n  math.go\n",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 4,
				Tool:      "test",
				ActionKey: makeActionKey("test", "test.go"),
				Args:      []string{"test.go"},
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

/*
requestLogServer records every model request and always answers with an
empty response (no TOOL_CALL). It is used by tests whose FSM legitimately
continues past the deterministic TEST state into REPAIR_READ and then asks
the model for a repair decision; the recorded paths let those tests assert
that the first request happened only at the expected point of the run.
*/
func requestLogServer(t *testing.T, paths *[]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": ""})
	}))
}

/*
failFirstRequestServer rejects any model request outright. A resumed TEST
checkpoint runs the deterministic controller test state without asking the
model, so tests that stop before the next model-dependent state use this
server to pin that contract.
*/
func failFirstRequestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("TEST checkpoint resume must not request the model, got %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"response": ""})
	}))
}

// newRepairAgent builds an agent that resumes from a persisted checkpoint
// while recording model requests instead of rejecting them outright.
func newRepairAgent(t *testing.T, root string, paths *[]string) *Agent {
	t.Helper()

	server := requestLogServer(t, paths)
	t.Cleanup(server.Close)

	return &Agent{
		Model:     "test-model",
		Max:       8,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}
}

// newCheckpointAgent builds an agent whose Run() is expected to restore
// the persisted TEST checkpoint and execute the deterministic controller
// test state before any model request can occur.
func newCheckpointAgent(t *testing.T, root string) *Agent {
	t.Helper()

	server := failFirstRequestServer(t)
	t.Cleanup(server.Close)

	return &Agent{
		Model:     "test-model",
		Max:       8,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}
}

func TestControllerStateTestRecordsFullOutputInLedger(t *testing.T) {
	task := "Fix math.go so add(2, 3) returns 10"
	root := newFailingProject(t)

	persistTestCheckpoint(t, root, task)

	var requests []string
	agent := newRepairAgent(t, root, &requests)

	/*
		The resumed failing test moves the FSM to REPAIR_READ; the run
		then continues deterministically through repair_read and only
		after that asks the model for a repair decision. The empty
		response (no TOOL_CALL) eventually ends the run with an error.
	*/
	if err := agent.Run(task); err == nil {
		t.Fatal("run must stop when the model keeps producing no tool call")
	}

	if len(requests) == 0 || requests[0] != "/api/generate" {
		t.Fatalf(
			"first model request must happen only after the deterministic repair read, got %v",
			requests,
		)
	}

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
		The run continued into REPAIR_READ/REPAIR_WRITE with the empty
		model responses, so the freshly recorded test entry is no longer
		the last ledger record. Locate it by its ActionKey instead.
	*/
	testKey := makeActionKey("test", "test.go")

	var saved []ActionRecord
	for _, item := range agent.ActionLedger {
		saved = append(saved, item)

		if item.Tool == "test" && item.ActionKey == testKey && !item.OK {
			break
		}
	}

	if len(saved) == 0 || saved[len(saved)-1].Tool != "test" {
		t.Fatal("new failing test record not found in the ledger")
	}

	/*
		Every ledger prefix up to and including the fresh test entry was
		persisted by saveState before the next model request, so it must
		pass the persisted-ledger validators as-is — including the legacy
		coarse-status fixture record, which stays loadable because the
		validators skip entries whose ActionKey recomputation fails.
	*/
	if err := validateActionLedgerIntegrity(saved); err != nil {
		t.Fatalf("recorded ledger fails integrity validation: %v", err)
	}

	if err := validateActionLedgerResultIntegrity(saved); err != nil {
		t.Fatalf("recorded ledger fails result validation: %v", err)
	}

	if err := validateActionLedgerTransitionIntegrity(saved); err != nil {
		t.Fatalf("recorded ledger fails transition validation: %v", err)
	}
}

func TestControllerStateTestRecordsPassOutputInLedger(t *testing.T) {
	task := "verify passing regression project"
	root := newFailingProject(t)

	checkpoint := persistTestCheckpoint(t, root, task)

	/*
		Success path: validators require "PASS" inside Result. The real
		test output contains PASS, so completion stays reachable while
		diagnostics are preserved. Repair math.go so the test passes.
	*/
	repaired := `package main

func Add(a, b int) int {
	return 10
}
`

	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(repaired), 0644); err != nil {
		t.Fatal(err)
	}

	/*
		Re-anchor the checkpoint to the repaired file: FileHashes is
		the baseline reconcile compares against, and saveTaskState
		validates every ledger record before writing.
	*/
	checkpoint.FileHashes["math.go"] = contentHash(repaired)

	if err := saveTaskState(root, checkpoint); err != nil {
		t.Fatalf("re-anchor TEST checkpoint: %v", err)
	}

	agent := newCheckpointAgent(t, root)

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

	/*
		Kill check for the ledger-output fix: if the production TEST
		state regressed to recording only the coarse result.Status
		("PASS"), this assertion must fail. The real perl tester
		prints "PASS\n"; a bare "PASS" can only come from the status
		field. Mirrors TestControllerStateTestRecordsFullOutputInLedger,
		which kills the same regression on the FAIL path via "WRONG:".
	*/
	if record.Result == "PASS" {
		t.Fatalf(
			"test ledger recorded coarse status instead of full output: got %q",
			record.Result,
		)
	}

	if !strings.Contains(record.Result, "PASS\n") {
		t.Fatalf(
			"successful test ledger result must carry the raw tester output ending with PASS line, got %q",
			record.Result,
		)
	}

	if agent.State != StateDone {
		t.Fatalf("passing test must move FSM to DONE, got %s", agent.State)
	}

	/*
		The in-memory ledger still carries the legacy coarse-status
		fixture record; validate the exact shape that was persisted
		on DONE: all records except the pre-fix legacy entry.
	*/
	var saved []ActionRecord
	for _, item := range agent.ActionLedger {
		if item.Tool == "test" && item.Result == "FAIL" {
			continue
		}

		saved = append(saved, item)
	}

	if err := validateActionLedgerIntegrity(saved); err != nil {
		t.Fatalf("pass ledger fails integrity validation: %v", err)
	}

	if err := validateActionLedgerResultIntegrity(saved); err != nil {
		t.Fatalf("pass ledger fails result validation: %v", err)
	}

	if err := validateActionLedgerTransitionIntegrity(saved); err != nil {
		t.Fatalf("pass ledger fails transition validation: %v", err)
	}
}

func TestControllerStateTestFallsBackToStatusOnEmptyOutput(t *testing.T) {
	root := t.TempDir()

	/*
		Silent failing test: perl dies with no output and a non-zero
		exit. The ledger falls back to the coarse status, and because
		a failed test without diagnostics gives the controller no
		repair marker ("WRONG:"/"TEST FAILED"), validateCompletion must
		block DONE instead of accepting a PASS-less completion.
	*/
	silentContent := "die;\n"

	if err := os.WriteFile(filepath.Join(root, "test.go"), []byte(silentContent), 0644); err != nil {
		t.Fatal(err)
	}

	/*
		The deterministic TEST state runs test.go first; die aborts it
		before the require below executes, so math.go is only needed
		for a passing run. Create it after the checkpoint is persisted
		so reconcile keeps the TEST resume path intact.
	*/
	mathContent := `package main

func Add(a, b int) int {
	return a + b
}
`

	index, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	task := "silent failing test fallback"

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateTest,
		Iteration: 4,

		ProjectIndex: index,

		ReadFiles: map[string]string{
			"math.go": mathContent,
		},
		FileHashes: map[string]string{
			"math.go": contentHash(mathContent),
		},
		ChangedFiles: []string{"math.go"},

		LastWritePath: "math.go",

		History: []string{
			"inspect_project",
			"read_file:math.go",
			"write_file:math.go",
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
				Iteration: 2,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "math.go"),
				Args:      []string{"math.go"},
				Result:    mathContent,
				OK:        true,
				Exit:      0,
			},
			/*
				write_file ledger records must carry the full
				[path, content] argument pair: transition validation
				requires every written path to have been read first,
				and the BaseHash must anchor to that read snapshot.
			*/
			{
				Iteration: 2,
				Tool:      "write_file",
				ActionKey: makeActionKey("write_file", "math.go", mathContent),
				Args:      []string{"math.go", mathContent},
				BaseHash:  contentHash(mathContent),
				Result:    "ok",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 3,
				Tool:      "git_diff",
				ActionKey: makeActionKey("git_diff"),
				Result:    "modified:\n  math.go\n",
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

	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte(mathContent), 0644); err != nil {
		t.Fatal(err)
	}

	agent := newCheckpointAgent(t, root)

	runErr := agent.Run(task)
	if runErr == nil {
		t.Fatal("silent failing test must not complete the task")
	}

	if !strings.Contains(runErr.Error(), "completion blocked") {
		t.Fatalf("expected completion-blocked error, got %v", runErr)
	}

	records := findTestRecords(agent)
	if len(records) != 1 {
		t.Fatalf("expected exactly one test ledger record, got %d", len(records))
	}

	record := records[0]

	if record.Result != "FAIL" {
		t.Fatalf(
			"empty test output must fall back to status, got %q",
			record.Result,
		)
	}

	if record.OK || record.Exit == 0 {
		t.Fatal("silently dying test must be recorded as failed action")
	}

	if err := validateActionLedgerResultIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("fallback ledger fails result validation: %v", err)
	}
}
