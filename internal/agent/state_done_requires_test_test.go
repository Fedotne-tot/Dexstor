package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDoneRecoveryRequiresSuccessfulTestEvidence(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".ai"), 0755); err != nil {
		t.Fatal(err)
	}

	content := "package main\n\nfunc main() {}\n"

	if err := os.WriteFile(
		filepath.Join(root, "main.go"),
		[]byte(content),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	hash := contentHash(content)

	ledger := []ActionRecord{
		{
			ActionKey: makeActionKey("inspect_project"),
			Tool:      "inspect_project",
			Iteration: 1,
			OK:        true,
			Exit:      0,
			Result:    "main.go",
		},
		{
			ActionKey: makeActionKey("read_file", "main.go"),
			Tool:      "read_file",
			Args:      []string{"main.go"},
			Iteration: 2,
			OK:        true,
			Exit:      0,
			Result:    content,
		},
		{
			ActionKey: makeActionKey("write_file", "main.go", content),
			Tool:      "write_file",
			Args:      []string{"main.go", content},
			Iteration: 3,
			OK:        true,
			Exit:      0,
			Result:    "written",
			BaseHash:  hash,
		},
		{
			ActionKey: makeActionKey("git_diff"),
			Tool:      "git_diff",
			Args:      nil,
			Iteration: 4,
			OK:        true,
			Exit:      0,
			Result:    "",
		},
	}

	state := &TaskState{
		State:        StateDone,
		TaskHash:     taskHash("finish the task"),
		Iteration:    4,
		ReadFiles:    map[string]string{"main.go": content},
		FileHashes:   map[string]string{"main.go": hash},
		ChangedFiles: []string{"main.go"},
		History: []string{
			"inspect_project",
			"read_file:main.go",
			"write_file:main.go",
			"git_diff:main.go",
		},
		ActionLedger: ledger,
		LastAction:   "git_diff",
		LastResult:   "",
		Wrote:        true,
		DiffChecked:  true,
	}

	if err := validateActionLedgerIntegrity(state.ActionLedger); err != nil {
		t.Fatalf("ledger integrity rejected state: %v", err)
	}

	if err := validateActionLedgerTemporalIntegrity(state.ActionLedger); err != nil {
		t.Fatalf("temporal integrity rejected state: %v", err)
	}

	if err := validateActionLedgerResultIntegrity(state.ActionLedger); err != nil {
		t.Fatalf("result integrity rejected state: %v", err)
	}

	if err := validateActionLedgerTransitionIntegrity(state.ActionLedger); err != nil {
		t.Fatalf("transition integrity rejected state: %v", err)
	}

	if err := validateActionLedgerSnapshotIntegrity(state); err != nil {
		t.Fatalf("snapshot integrity rejected state: %v", err)
	}

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("persisted-state integrity rejected state: %v", err)
	}

	if state.State != StateDone {
		t.Fatalf("expected StateDone, got %q", state.State)
	}

	t.Log("PROOF: current persisted-state validators accept StateDone without any test action")
}
