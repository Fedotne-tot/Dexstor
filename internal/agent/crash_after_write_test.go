package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCrashAfterPhysicalWriteDoesNotRepeatWrite(t *testing.T) {
	root := t.TempDir()

	initial := `sub add {
	my ($a, $b) = @_;
	return $a + $b;
}

1;
`

	repaired := `sub add {
	my ($a, $b) = @_;
	return 10;
}

1;
`

	path := filepath.Join(root, "math.pl")

	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	/*
		Simulate the exact crash window:

		1. PendingAction is persisted.
		2. The physical write completes.
		3. The process crashes before PendingAction is cleared
		   and the post-write state is saved.
	*/
	original := New(root, "test-model", 10)
	original.State = StateWriteReq
	original.PendingAction = &PendingAction{
		Tool:     "write_file",
		BaseHash: contentHash(initial),
		Args: map[string]string{
			"path":    "math.pl",
			"content": repaired,
		},
	}

	/*
		The pending write is not yet a completed ActionLedger entry.
		The physical write is intentionally performed after the
		checkpoint below, exactly as it would be in the crash window.
	*/
	original.ActionLedger = nil
	original.History = nil

	task := "Change math.pl so add(2, 3) returns 10"

	if err := original.saveState(task, 3); err != nil {
		t.Fatalf("save pending write checkpoint: %v", err)
	}

	if err := os.WriteFile(path, []byte(repaired), 0644); err != nil {
		t.Fatalf("simulate physical write: %v", err)
	}

	/*
		New process after crash.
	*/
	resumed := New(root, "test-model", 10)

	if err := resumed.Run(task); err == nil {
		t.Fatal("expected Run to stop because no Ollama server is configured for this test")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != repaired {
		t.Fatalf(
			"recovered file content changed unexpectedly:\n%s",
			data,
		)
	}

	if resumed.State != StateVerifyDiff {
		t.Fatalf(
			"expected resumed state VERIFY_DIFF after recovered write, got %s",
			resumed.State,
		)
	}

	if resumed.PendingAction != nil {
		t.Fatal("expected PendingAction to be cleared after recovery")
	}

	if resumed.LastWritePath != "math.pl" {
		t.Fatalf(
			"expected recovered LastWritePath math.pl, got %q",
			resumed.LastWritePath,
		)
	}

	found := false
	for _, entry := range resumed.History {
		if entry == "write_file:math.pl" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected recovered write in history, got %v", resumed.History)
	}

	state, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("reload recovered state: %v", err)
	}

	if !ok {
		t.Fatal("expected recovered state to remain persisted")
	}

	if state.PendingAction != nil {
		t.Fatal("persisted state still contains completed PendingAction")
	}

	if state.State != StateVerifyDiff {
		t.Fatalf(
			"expected persisted state VERIFY_DIFF, got %s",
			state.State,
		)
	}

	/*
		The recovered write must be represented using the canonical
		v3 ActionLedger identity, not the old "write_file:math.pl"
		Tool encoding.
	*/
	foundWrite := false

	for _, entry := range state.ActionLedger {
		if entry.Tool != "write_file" {
			continue
		}

		if len(entry.Args) != 2 {
			continue
		}

		if entry.Args[0] != "math.pl" {
			continue
		}

		if entry.Args[1] != repaired {
			continue
		}

		if entry.ActionKey != makeActionKey(
			"write_file",
			"math.pl",
			repaired,
		) {
			t.Fatalf("recovered write has invalid ActionKey: %q", entry.ActionKey)
		}

		if !entry.OK || entry.Exit != 0 {
			t.Fatalf(
				"recovered write has invalid status: OK=%v Exit=%d",
				entry.OK,
				entry.Exit,
			)
		}

		foundWrite = true
	}

	if !foundWrite {
		t.Fatalf(
			"expected canonical recovered write_file action, got %+v",
			state.ActionLedger,
		)
	}
}
