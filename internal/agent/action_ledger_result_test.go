package agent

import "testing"

func resultAction(
	iteration int,
	tool string,
	result string,
	ok bool,
	exit int,
	args ...string,
) ActionRecord {
	storedArgs := append([]string(nil), args...)

	return ActionRecord{
		Iteration: iteration,
		Tool:      tool,
		ActionKey: makeActionKey(tool, storedArgs...),
		Args:      storedArgs,
		Result:    result,
		OK:        ok,
		Exit:      exit,
	}
}

func TestActionLedgerResultRejectsFailedActionWithZeroExit(t *testing.T) {
	ledger := []ActionRecord{
		resultAction(1, "read_file", "failed", false, 0, "math.go"),
	}

	if err := validateActionLedgerResultIntegrity(ledger); err == nil {
		t.Fatal("failed action with zero exit must be rejected")
	}
}

func TestActionLedgerResultRejectsSuccessfulActionWithNonZeroExit(t *testing.T) {
	ledger := []ActionRecord{
		resultAction(1, "read_file", "content", true, 1, "math.go"),
	}

	if err := validateActionLedgerResultIntegrity(ledger); err == nil {
		t.Fatal("successful action with non-zero exit must be rejected")
	}
}

func TestActionLedgerResultRejectsSuccessfulTestWithoutPASS(t *testing.T) {
	ledger := []ActionRecord{
		resultAction(1, "test", "tests completed", true, 0, "test.go"),
	}

	if err := validateActionLedgerResultIntegrity(ledger); err == nil {
		t.Fatal("successful test without PASS must be rejected")
	}
}

func TestActionLedgerResultAcceptsSuccessfulNonTestAction(t *testing.T) {
	ledger := []ActionRecord{
		resultAction(1, "read_file", "content", true, 0, "math.go"),
		resultAction(2, "write_file", "file written successfully", true, 0, "math.go", "content"),
		resultAction(3, "git_diff", "diff", true, 0),
	}

	if err := validateActionLedgerResultIntegrity(ledger); err != nil {
		t.Fatalf("valid successful actions must be accepted: %v", err)
	}
}

func TestActionLedgerResultAcceptsFailedTest(t *testing.T) {
	ledger := []ActionRecord{
		resultAction(1, "test", "WRONG: 5", false, 1, "test.go"),
	}

	if err := validateActionLedgerResultIntegrity(ledger); err != nil {
		t.Fatalf("failed test must remain valid: %v", err)
	}
}

func TestActionLedgerResultAllowsLegacyRecordWithoutActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 4,
			Tool:      "write_file:math.go",
			Result:    "file written successfully",
			OK:        false,
			Exit:      0,
		},
	}

	if err := validateActionLedgerResultIntegrity(ledger); err != nil {
		t.Fatalf("legacy records without ActionKey must remain loadable: %v", err)
	}
}
