package agent

import (
	"testing"
)

func TestActionLedgerRejectsTamperedActionKey(t *testing.T) {
	root := t.TempDir()

	args := []string{
		"math.pl",
		"return 10;\n",
	}

	validKey := makeActionKey("write_file", args...)

	a := &Agent{
		Root: root,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: validKey + "tampered",
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
				Args:      append([]string(nil), args...),
			},
		},
	}

	if _, ok := a.hasSuccessfulAction("write_file", args...); ok {
		t.Fatal("tampered ActionKey was accepted as successful action")
	}
}

func TestActionLedgerAcceptsValidActionKey(t *testing.T) {
	root := t.TempDir()

	args := []string{
		"math.pl",
		"return 10;\n",
	}

	key := makeActionKey("write_file", args...)

	a := &Agent{
		Root: root,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key,
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
				Args:      append([]string(nil), args...),
			},
		},
	}

	record, ok := a.hasSuccessfulAction("write_file", args...)
	if !ok {
		t.Fatal("valid ActionKey was not accepted")
	}

	if record.ActionKey != key {
		t.Fatalf("unexpected ActionKey: got %q want %q", record.ActionKey, key)
	}
}

func TestActionLedgerRejectsMismatchedArguments(t *testing.T) {
	root := t.TempDir()

	storedArgs := []string{
		"math.pl",
		"return 10;\n",
	}

	key := makeActionKey("write_file", storedArgs...)

	a := &Agent{
		Root: root,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key,
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
				Args:      append([]string(nil), storedArgs...),
			},
		},
	}

	differentArgs := []string{
		"math.pl",
		"return 11;\n",
	}

	if _, ok := a.hasSuccessfulAction("write_file", differentArgs...); ok {
		t.Fatal("ActionLedger accepted action with different arguments")
	}
}

func TestActionLedgerLegacyRecordWithoutArgsDoesNotPassIntegrityCheck(t *testing.T) {
	root := t.TempDir()

	key := makeActionKey(
		"write_file",
		"math.pl",
		"return 10;\n",
	)

	a := &Agent{
		Root: root,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file:math.pl",
				ActionKey: key,
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
			},
		},
	}

	if _, ok := a.hasSuccessfulAction(
		"write_file",
		"math.pl",
		"return 10;\n",
	); ok {
		t.Fatal("legacy record without Args passed integrity validation")
	}
}

func TestPersistedStateRejectsTamperedActionKey(t *testing.T) {
	root := t.TempDir()
	task := "persisted tampered action key"

	key := makeActionKey(
		"write_file",
		"math.pl",
		"return 10;\n",
	)

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateVerifyDiff,
		Iteration: 7,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key + "tampered",
				Args: []string{
					"math.pl",
					"return 10;\n",
				},
				Result: "file written successfully",
				OK:     true,
				Exit:   0,
			},
		},
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("saveTaskState failed: %v", err)
	}

	if _, _, err := loadTaskState(root, task); err == nil {
		t.Fatal("tampered persisted ActionKey was accepted")
	}
}

func TestPersistedStateRejectsChangedArguments(t *testing.T) {
	root := t.TempDir()
	task := "persisted changed arguments"

	key := makeActionKey(
		"write_file",
		"math.pl",
		"return 10;\n",
	)

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateVerifyDiff,
		Iteration: 7,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key,
				Args: []string{
					"math.pl",
					"return 11;\n",
				},
				Result: "file written successfully",
				OK:     true,
				Exit:   0,
			},
		},
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("saveTaskState failed: %v", err)
	}

	if _, _, err := loadTaskState(root, task); err == nil {
		t.Fatal("persisted ActionLedger with changed Args was accepted")
	}
}

func TestPersistedStateRejectsTamperedSnapshotWithMatchingBaseHash(t *testing.T) {
	root := t.TempDir()
	task := "persisted tampered snapshot"

	originalContent := "return 5;\n"
	tamperedContent := "return 999;\n"

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateVerifyDiff,
		Iteration: 3,

		ReadFiles: map[string]string{
			"math.pl": originalContent,
		},
		FileHashes: map[string]string{
			"math.pl": contentHash(originalContent),
		},

		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "math.pl\ntest.pl",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "math.pl"),
				Args:      []string{"math.pl"},
				Result:    tamperedContent,
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 3,
				Tool:      "write_file",
				ActionKey: makeActionKey(
					"write_file",
					"math.pl",
					"return 10;\n",
				),
				Args: []string{
					"math.pl",
					"return 10;\n",
				},
				BaseHash: contentHash(tamperedContent),
				Result:   "file written successfully",
				OK:       true,
				Exit:     0,
			},
		},
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("saveTaskState failed: %v", err)
	}

	if _, _, err := loadTaskState(root, task); err == nil {
		t.Fatal("persisted state accepted tampered snapshot with matching BaseHash")
	}
}

func TestPersistedStateAcceptsValidActionKey(t *testing.T) {
	root := t.TempDir()
	task := "persisted valid action key"

	key := makeActionKey(
		"write_file",
		"math.pl",
		"return 10;\n",
	)

	state := &TaskState{
		Version:   taskStateVersion,
		Task:      task,
		TaskHash:  taskHash(task),
		Project:   root,
		State:     StateVerifyDiff,
		Iteration: 7,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key,
				Args: []string{
					"math.pl",
					"return 10;\n",
				},
				Result: "file written successfully",
				OK:     true,
				Exit:   0,
			},
		},
	}

	if err := saveTaskState(root, state); err != nil {
		t.Fatalf("saveTaskState failed: %v", err)
	}

	loaded, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("valid persisted state was rejected: %v", err)
	}

	if !ok {
		t.Fatal("expected valid persisted state to load")
	}

	if len(loaded.ActionLedger) != 1 {
		t.Fatalf(
			"expected one restored ledger entry, got %d",
			len(loaded.ActionLedger),
		)
	}

	record := loaded.ActionLedger[0]

	if record.ActionKey != key {
		t.Fatalf(
			"restored ActionKey mismatch: want %q, got %q",
			key,
			record.ActionKey,
		)
	}

	if makeActionKey(record.Tool, record.Args...) != record.ActionKey {
		t.Fatal("restored ActionKey does not match restored Tool + Args")
	}
}

func TestPersistedStateRejectsUnknownToolWithValidActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "unknown_tool",
			ActionKey: makeActionKey("unknown_tool", "x"),
			Args:      []string{"x"},
			Result:    "unexpected",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerIntegrity(ledger); err == nil {
		t.Fatal("expected unknown tool to be rejected")
	}
}

func TestPersistedStateRejectsWrongArgumentCountWithValidActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file"),
			Args:      nil,
			Result:    "unexpected",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerIntegrity(ledger); err == nil {
		t.Fatal("expected wrong argument count to be rejected")
	}
}

func TestPersistedStateRejectsEmptyPathWithValidActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "test",
			ActionKey: makeActionKey("test", ""),
			Args:      []string{""},
			Result:    "unexpected",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerIntegrity(ledger); err == nil {
		t.Fatal("expected empty path to be rejected")
	}
}

func TestPersistedStateAcceptsCanonicalSemanticActions(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			Result:    "math.pl\ntest.pl",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 2,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file", "math.pl"),
			Args:      []string{"math.pl"},
			Result:    "source",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.pl", "return 10;\n"),
			Args:      []string{"math.pl", "return 10;\n"},
			Result:    "file written successfully",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 4,
			Tool:      "git_diff",
			ActionKey: makeActionKey("git_diff"),
			Result:    "diff",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 5,
			Tool:      "test",
			ActionKey: makeActionKey("test", "test.pl"),
			Args:      []string{"test.pl"},
			Result:    "PASS",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 6,
			Tool:      "repair_read",
			ActionKey: makeActionKey("repair_read", "math.pl"),
			Args:      []string{"math.pl"},
			Result:    "source",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 7,
			Tool:      "repair_write",
			ActionKey: makeActionKey("repair_write", "math.pl", "return 10;\n"),
			Args:      []string{"math.pl", "return 10;\n"},
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
	}

	if err := validateActionLedgerIntegrity(ledger); err != nil {
		t.Fatalf("canonical semantic ledger rejected: %v", err)
	}
}

func TestPersistedStateAllowsLegacyEmptyActionKey(t *testing.T) {
	ledger := []ActionRecord{
		{
			Iteration: 4,
			Tool:      "write_file:math.pl",
			ActionKey: "",
			Result:    "file written successfully",
		},
	}

	if err := validateActionLedgerIntegrity(ledger); err != nil {
		t.Fatalf("legacy v2 record must remain loadable: %v", err)
	}
}
