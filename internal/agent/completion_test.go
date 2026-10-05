package agent

import "testing"

func completionAction(
	iteration int,
	tool string,
	result string,
	args ...string,
) ActionRecord {
	storedArgs := append([]string(nil), args...)

	return ActionRecord{
		Iteration: iteration,
		Tool:      tool,
		ActionKey: makeActionKey(tool, storedArgs...),
		Args:      storedArgs,
		Result:    result,
		OK:        true,
		Exit:      0,
	}
}

func failedCompletionAction(
	iteration int,
	tool string,
	result string,
	args ...string,
) ActionRecord {
	storedArgs := append([]string(nil), args...)

	return ActionRecord{
		Iteration: iteration,
		Tool:      tool,
		ActionKey: makeActionKey(tool, storedArgs...),
		Args:      storedArgs,
		Result:    result,
		OK:        false,
		Exit:      255,
	}
}

func TestValidateCompletionSuccess(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"read_file",
				"math.pl",
				"math.pl",
			),
			completionAction(
				3,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				4,
				"git_diff",
				"diff",
			),
			completionAction(
				5,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err != nil {
		t.Fatalf("expected completion to be allowed, got: %v", err)
	}
}

func TestValidateCompletionRejectsFailedTest(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected after failed test")
	}
}

func TestValidateCompletionRepair(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 5;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"WRONG: 5",
				"test.pl",
			),
			completionAction(
				5,
				"repair_read",
				"math.pl",
				"math.pl",
			),
			completionAction(
				6,
				"repair_write",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				7,
				"git_diff",
				"diff",
			),
			completionAction(
				8,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err != nil {
		t.Fatalf("expected repaired task to be allowed, got: %v", err)
	}
}

func TestValidateCompletionRejectsWriteAfterFinalTest(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			completionAction(
				4,
				"test",
				"PASS",
				"test.pl",
			),
			completionAction(
				5,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected when a write occurs after the successful test")
	}
}

func TestValidateCompletionRejectsDiffBeforeFinalWrite(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"git_diff",
				"diff",
			),
			completionAction(
				3,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				4,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected when git_diff occurred before the final write")
	}
}

func TestValidateCompletionRejectsTestBeforeFinalDiff(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			completionAction(
				4,
				"test",
				"PASS",
				"test.pl",
			),
			completionAction(
				5,
				"git_diff",
				"diff",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected when a diff occurs after the successful test")
	}
}

func TestValidateCompletionRejectsRepairWithoutRepairRead(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"WRONG: 5",
				"test.pl",
			),
			completionAction(
				5,
				"repair_write",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				6,
				"git_diff",
				"diff",
			),
			completionAction(
				7,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected without repair_read")
	}
}

func TestValidateCompletionRejectsRepairWithoutRepairWrite(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"WRONG: 5",
				"test.pl",
			),
			completionAction(
				5,
				"repair_read",
				"math.pl",
				"math.pl",
			),
			completionAction(
				6,
				"git_diff",
				"diff",
			),
			completionAction(
				7,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected without repair_write")
	}
}

func TestValidateCompletionTreatsRepairWriteAsFinalWrite(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 5;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"WRONG: 5",
				"test.pl",
			),
			completionAction(
				5,
				"repair_read",
				"math.pl",
				"math.pl",
			),
			completionAction(
				6,
				"repair_write",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				7,
				"git_diff",
				"diff",
			),
			completionAction(
				8,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err != nil {
		t.Fatalf("expected repaired task to be allowed, got: %v", err)
	}
}

func TestValidateCompletionRejectsRepairWriteAfterFinalDiff(t *testing.T) {
	a := &Agent{
		ActionLedger: []ActionRecord{
			completionAction(
				1,
				"inspect_project",
				"files=2",
			),
			completionAction(
				2,
				"write_file",
				"write completed",
				"math.pl",
				"return 5;\n",
			),
			completionAction(
				3,
				"git_diff",
				"diff",
			),
			failedCompletionAction(
				4,
				"test",
				"WRONG: 5",
				"test.pl",
			),
			completionAction(
				5,
				"repair_read",
				"math.pl",
				"math.pl",
			),
			completionAction(
				6,
				"git_diff",
				"diff",
			),
			completionAction(
				7,
				"repair_write",
				"write completed",
				"math.pl",
				"return 10;\n",
			),
			completionAction(
				8,
				"test",
				"PASS",
				"test.pl",
			),
		},
	}

	if err := a.validateCompletion(); err == nil {
		t.Fatal("expected completion to be rejected when repair_write occurs after final git_diff")
	}
}
