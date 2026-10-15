package agent

import "testing"

func TestActionLedgerTransitionAllowsPartialSnapshotStartingWithRead(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(7, "read_file", "content", "math.go"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("partial snapshot beginning with read_file must be accepted: %v", err)
	}
}

func TestActionLedgerTransitionRejectsWriteAfterInspectWithoutRead(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "write_file", "write completed", "math.go", "fixed"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err == nil {
		t.Fatal("write_file after inspect_project without read_file must be rejected")
	}
}

func TestActionLedgerTransitionAcceptsInspectThenRead(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "read_file", "content", "math.go"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("inspect_project followed by read_file must be accepted: %v", err)
	}
}

func TestActionLedgerTransitionRejectsRepairWriteBeforeRepairRead(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "repair_write", "write completed", "math.go", "fixed"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err == nil {
		t.Fatal("repair_write before repair_read must be rejected")
	}
}

func TestActionLedgerTransitionAcceptsRepairSequence(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "read_file", "content", "math.go"),
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", "fixed"),
			Args:      []string{"math.go", "fixed"},
			BaseHash:  contentHash("content"),
			Result:    "file written successfully",
			OK:        true,
			Exit:      0,
		},
		completionAction(4, "git_diff", "diff"),
		completionAction(5, "test", "WRONG: 5", "test.go"),
		completionAction(6, "repair_read", "broken content", "math.go"),
		{
			Iteration: 7,
			Tool:      "repair_write",
			ActionKey: makeActionKey("repair_write", "math.go", "fixed"),
			Args:      []string{"math.go", "fixed"},
			BaseHash:  contentHash("broken content"),
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
		completionAction(8, "git_diff", "diff"),
		completionAction(9, "test", "PASS", "test.go"),
	}

	ledger[4].OK = false
	ledger[4].Exit = 1
	ledger[4].ActionKey = makeActionKey("test", "test.go")

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("valid repair sequence must be accepted: %v", err)
	}
}

func TestActionLedgerTransitionAllowsMultipleActionsSameIteration(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "read_file", "math", "math.go"),
		completionAction(2, "read_file", "test", "test.go"),
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", "fixed"),
			Args:      []string{"math.go", "fixed"},
			BaseHash:  contentHash("math"),
			Result:    "file written successfully",
			OK:        true,
			Exit:      0,
		},
		completionAction(4, "git_diff", "diff"),
		completionAction(5, "test", "PASS", "test.go"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("multiple actions in one iteration must be accepted: %v", err)
	}
}

func TestActionLedgerTransitionAllowsLegacyRecords(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 4,
			Tool:      "write_file:math.go",
			Result:    "file written successfully",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("legacy records without ActionKey must remain loadable: %v", err)
	}
}

func TestActionLedgerTransitionRejectsWriteToUnreadFile(t *testing.T) {
	ledger := []ActionRecord{
		completionAction(1, "inspect_project", "math.go\ntest.go"),
		completionAction(2, "read_file", "test source", "test.go"),
		completionAction(3, "write_file", "write completed", "math.go", "fixed"),
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err == nil {
		t.Fatal("write_file to a file that was not read must be rejected")
	}
}

func TestActionLedgerTransitionAcceptsSnapshotAnchoredRewrite(t *testing.T) {
	oldContent := "sub add { return $_[0] + $_[1]; }\n"
	newContent := "sub add { return ($_[0] + $_[1]) * 2; }\n"

	ledger := []ActionRecord{
		completionAction(
			1,
			"inspect_project",
			"math.go\ntest.go",
		),
		{
			Iteration: 2,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file", "math.go"),
			Args:      []string{"math.go"},
			Result:    oldContent,
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", newContent),
			Args:      []string{"math.go", newContent},
			BaseHash:  contentHash(oldContent),
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err != nil {
		t.Fatalf("valid snapshot-anchored rewrite was rejected: %v", err)
	}
}

func TestActionLedgerTransitionRejectsMismatchedSnapshotAnchor(t *testing.T) {
	oldContent := "sub add { return $_[0] + $_[1]; }\n"
	newContent := "completely new implementation\n"

	ledger := []ActionRecord{
		completionAction(
			1,
			"inspect_project",
			"math.go\ntest.go",
		),
		{
			Iteration: 2,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file", "math.go"),
			Args:      []string{"math.go"},
			Result:    oldContent,
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", newContent),
			Args:      []string{"math.go", newContent},
			BaseHash:  contentHash("tampered snapshot\n"),
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err == nil {
		t.Fatal("write_file with mismatched snapshot anchor was accepted")
	}
}

func TestActionLedgerTransitionRejectsWriteWithoutSnapshotAnchor(t *testing.T) {
	oldContent := "sub add { return $_[0] + $_[1]; }\n"
	newContent := "completely new implementation\n"

	ledger := []ActionRecord{
		completionAction(
			1,
			"inspect_project",
			"math.go\ntest.go",
		),
		{
			Iteration: 2,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file", "math.go"),
			Args:      []string{"math.go"},
			Result:    oldContent,
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", newContent),
			Args:      []string{"math.go", newContent},
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTransitionIntegrity(ledger); err == nil {
		t.Fatal("write_file without snapshot anchor was accepted")
	}
}
