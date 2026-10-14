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
	"bratan-go/internal/tools"
)

/*
Regression coverage for the assert_output FSM path.

The pre-existing test TestAssertOutputCannotCompleteTaskWithoutMandatoryVerification
only inspects History strings and terminal State. It never checks that the
assert_output tool call actually produced a ledger record, so a production
regression that silently drops recordAction("assert_output", ...) would keep
that test green.

This test closes exactly that hole: it drives the same FSM path
(DISCOVER -> ... -> assert_output PASS -> WRITE_REQUIRED) and asserts on the
persisted ActionLedger entries, not just History strings.

Production code is unchanged; the test reuses only existing helpers
(makeActionKey, contentHash, saveState/loadTaskState pattern from
action_ledger_persistence_test.go).
*/
func TestAssertOutputRecordsVerifiableLedgerEntryAndCannotComplete(t *testing.T) {
	root := t.TempDir()

	task := "assert_output must be recorded in the action ledger"

	if err := os.WriteFile(
		filepath.Join(root, "math.pl"),
		[]byte("print 'hello';\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	const (
		assertCommand  = "printf 'PASS\\n'"
		assertExpected = "PASS"
	)

	/*
		Behavior-driven mock (no request-number coupling): responses are
		selected from the prompt content itself. After a successful
		assert_output the FSM enters WRITE_REQUIRED and asks the model for
		a write_file plan; the mock answers that state with a deliberately
		invalid tool call (empty path/content), which the production code
		rejects before execution. The run then burns the remaining iteration
		budget deterministically. No HTTP request ever fails, so a transport
		or parse problem cannot hide behind generic error matching, and no
		assertion depends on how many requests the FSM happened to issue.
	*/
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

		var response string

		switch {
		case strings.Contains(req.Prompt, "Current state: WRITE_REQUIRED"):
			// Invalid proposal: rejected before execution, keeps the run
			// inside the budget without touching the filesystem.
			response = "TOOL_CALL\nname: write_file\npath:\ncontent:\nEND_TOOL_CALL"
		case strings.Contains(req.Prompt, "Current state: IMPLEMENT"):
			response = "TOOL_CALL\nname: assert_output\ncommand: " + assertCommand + "\nexpected: " + assertExpected + "\nEND_TOOL_CALL"
		default:
			// DISCOVER and any other pre-IMPLEMENT state.
			response = "TOOL_CALL\nname: inspect_project\nEND_TOOL_CALL"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": response,
			"done":     "true",
		})
	}))
	defer server.Close()

	agent := &Agent{
		Model:     "test-model",
		Max:       5,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	/*
	   The old version accepted any error whose text contained the budget
	   message ("agent stopped after N iterations"). Now the outcome is
	   checked through behavior only: DONE must never be reached, and the
	   ledger assertions below prove the assert_output path actually ran.
	   Skipping read_file here is safe: assert_output is verification-only
	   evidence and does not require a read snapshot in the controller.
	*/
	_ = agent.Run(task)

	/*
		1. A successful assert_output must leave an OK ledger entry with
		   recomputable integrity. This is the actual regression guard:
		   dropping or corrupting recordAction here fails this assertion,
		   while the old History-only test would stay green.
	*/
	var assertRecord *ActionRecord
	for i := range agent.ActionLedger {
		record := &agent.ActionLedger[i]
		if record.Tool == "assert_output" {
			if assertRecord != nil {
				t.Fatalf("expected exactly one assert_output ledger entry, got multiple: %+v", agent.ActionLedger)
			}
			assertRecord = record
		}
	}

	if assertRecord == nil {
		t.Fatalf("successful assert_output did not produce a ledger entry; ledger=%+v", agent.ActionLedger)
	}

	if !assertRecord.OK || assertRecord.Exit != 0 {
		t.Fatalf("assert_output ledger entry must record the passing verification, got OK=%v Exit=%d Error=%q",
			assertRecord.OK, assertRecord.Exit, assertRecord.Error)
	}

	if len(assertRecord.Args) != 2 ||
		assertRecord.Args[0] != assertCommand ||
		assertRecord.Args[1] != assertExpected {
		t.Fatalf("assert_output ledger args mismatch: want [%q %q], got %v",
			assertCommand, assertExpected, assertRecord.Args)
	}

	if assertRecord.ActionKey == "" ||
		assertRecord.ActionKey != makeActionKey("assert_output", assertRecord.Args...) {
		t.Fatalf("assert_output ActionKey is missing or does not recompute from Tool+Args: %q",
			assertRecord.ActionKey)
	}

	/*
		2. Ledger-level integrity validators must accept the persisted run.
	*/
	if err := validateActionLedgerIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("assert_output ledger rejected by integrity validator: %v", err)
	}
	if err := validateActionLedgerResultIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("assert_output ledger rejected by result-integrity validator: %v", err)
	}
	if err := validateActionLedgerTransitionIntegrity(agent.ActionLedger); err != nil {
		t.Fatalf("assert_output ledger rejected by transition-integrity validator: %v", err)
	}

	/*
		3. Completion still requires write_file -> git_diff -> test PASS.
		   Check the ledger (not History strings): no mandatory-verification
		   entries may exist, and validateCompletion must reject DONE.
	*/
	for _, record := range agent.ActionLedger {
		switch record.Tool {
		case "write_file", "git_diff", "test":
			t.Fatalf("assert_output-only run leaked mandatory-verification ledger entry %q: %+v",
				record.Tool, agent.ActionLedger)
		}
	}

	if err := agent.validateCompletion(); err == nil {
		t.Fatal("validateCompletion must reject a run that only contains assert_output evidence")
	}

	if agent.State == StateDone {
		t.Fatal("assert_output must not transition agent directly to DONE")
	}

	/*
		4. The ledger entry must survive persistence round-trip, matching the
		   established pattern in action_ledger_persistence_test.go.
	*/
	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("loadTaskState failed: %v", err)
	}
	if !ok {
		t.Fatal("expected persisted task state to be found")
	}

	var persisted *ActionRecord
	for i := range saved.ActionLedger {
		if saved.ActionLedger[i].Tool == "assert_output" {
			persisted = &saved.ActionLedger[i]
		}
	}
	if persisted == nil {
		t.Fatalf("assert_output ledger entry missing from persisted state: %+v", saved.ActionLedger)
	}
	if persisted.ActionKey != assertRecord.ActionKey {
		t.Fatalf("persisted assert_output ActionKey mismatch: want %q, got %q",
			assertRecord.ActionKey, persisted.ActionKey)
	}
}
