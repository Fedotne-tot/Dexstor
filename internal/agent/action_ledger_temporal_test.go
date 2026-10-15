package agent

import "testing"

func TestActionLedgerTemporalRejectsNegativeIteration(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: -1,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err == nil {
		t.Fatal("expected negative iteration to be rejected")
	}
}

func TestActionLedgerTemporalRejectsZeroIteration(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 0,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err == nil {
		t.Fatal("expected zero iteration to be rejected")
	}
}

func TestActionLedgerTemporalRejectsBackwardIteration(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 2,
			Tool:      "git_diff",
			ActionKey: makeActionKey("git_diff"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 1,
			Tool:      "test",
			Args:      []string{"test.go"},
			ActionKey: makeActionKey("test", "test.go"),
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err == nil {
		t.Fatal("expected backward iteration sequence to be rejected")
	}
}

func TestActionLedgerTemporalAllowsSameIteration(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 1,
			Tool:      "git_diff",
			ActionKey: makeActionKey("git_diff"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 2,
			Tool:      "read_file",
			Args:      []string{"math.go"},
			ActionKey: makeActionKey("read_file", "math.go"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 2,
			Tool:      "test",
			Args:      []string{"test.go"},
			ActionKey: makeActionKey("test", "test.go"),
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err != nil {
		t.Fatalf("same iteration must be allowed: %v", err)
	}
}

func TestActionLedgerTemporalAllowsResumeStartingLater(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 7,
			Tool:      "read_file",
			Args:      []string{"math.go"},
			ActionKey: makeActionKey("read_file", "math.go"),
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 8,
			Tool:      "write_file",
			Args:      []string{"math.go", "return 10;\n"},
			ActionKey: makeActionKey("write_file", "math.go", "return 10;\n"),
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err != nil {
		t.Fatalf("resumed ledger starting at iteration 7 must be allowed: %v", err)
	}
}

func TestActionLedgerTemporalAllowsLegacyRecordWithoutActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 4,
			Tool:      "write_file:math.go",
			ActionKey: "",
			Result:    "file written successfully",
		},
		{
			Iteration: 2,
			Tool:      "old_legacy_action",
			ActionKey: "",
			Result:    "legacy",
		},
	}

	if err := validateActionLedgerTemporalIntegrity(ledger); err != nil {
		t.Fatalf("legacy records without ActionKey must remain loadable: %v", err)
	}
}
