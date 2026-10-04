package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActionLedgerPersistsAcrossProcessRestart(t *testing.T) {
	root := t.TempDir()

	key := makeActionKey(
		"write_file",
		"math.pl",
		"return 10;\n",
	)

	first := &Agent{
		Root:  root,
		State: StateVerifyDiff,
		ActionLedger: []ActionRecord{
			{
				Iteration: 7,
				Tool:      "write_file",
				ActionKey: key,
				Args:      []string{"math.pl", "return 10;\n"},
				Result:    "file written successfully",
				OK:        true,
				Exit:      0,
			},
		},
		History: []string{
			"write_file:math.pl",
		},
		LastWritePath: "math.pl",
		Wrote:         true,
		DiffChecked:   false,
	}

	task := "persist action ledger across restart"

	if err := first.saveState(task, 7); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	// Simulate a completely new process/Agent instance.
	second := &Agent{
		Root:      root,
		ReadFiles: make(map[string]string),
	}

	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("loadTaskState failed: %v", err)
	}

	if !ok {
		t.Fatal("expected persisted task state to be found")
	}

	second.restoreState(saved)

	if len(second.ActionLedger) != 1 {
		t.Fatalf(
			"expected one restored ledger entry, got %d",
			len(second.ActionLedger),
		)
	}

	record := second.ActionLedger[0]

	if record.ActionKey != key {
		t.Fatalf(
			"restored ActionKey mismatch: want %q, got %q",
			key,
			record.ActionKey,
		)
	}

	if record.Tool != "write_file" {
		t.Fatalf(
			"restored tool mismatch: want %q, got %q",
			"write_file",
			record.Tool,
		)
	}

	if len(record.Args) != 2 {
		t.Fatalf("restored Args length mismatch: got %d", len(record.Args))
	}

	if record.Args[0] != "math.pl" {
		t.Fatalf("restored path arg mismatch: got %q", record.Args[0])
	}

	if record.Args[1] != "return 10;\n" {
		t.Fatalf("restored content arg mismatch: got %q", record.Args[1])
	}

	if makeActionKey(record.Tool, record.Args...) != record.ActionKey {
		t.Fatal("restored ActionKey does not match restored Tool + Args")
	}

	if !record.OK || record.Exit != 0 {
		t.Fatalf(
			"restored successful action changed status: OK=%v Exit=%d",
			record.OK,
			record.Exit,
		)
	}

	found, ok := second.hasSuccessfulAction("write_file", "math.pl", "return 10;\n")
	if !ok {
		t.Fatal("restored successful ActionKey was not detected")
	}

	if found.ActionKey != key {
		t.Fatalf(
			"detected record has wrong ActionKey: want %q, got %q",
			key,
			found.ActionKey,
		)
	}
}

func TestOldStateWithoutActionKeyLoadsNormally(t *testing.T) {
	root := t.TempDir()

	mathPath := filepath.Join(root, "math.pl")
	if err := os.WriteFile(
		mathPath,
		[]byte("return 10;\n"),
		0644,
	); err != nil {
		t.Fatalf("write math.pl: %v", err)
	}

	task := "load legacy action ledger"

	legacy := `{
  "version": 2,
  "task": "load legacy action ledger",
  "task_hash": "` + taskHash(task) + `",
  "project": "` + root + `",
  "state": "VERIFY_DIFF",
  "iteration": 4,
  "history": [
    "write_file:math.pl"
  ],
  "action_ledger": [
    {
      "iteration": 4,
      "tool": "write_file:math.pl",
      "result": "file written successfully"
    }
  ],
  "wrote": true,
  "diff_checked": false
}`

	statePath := taskStatePath(root)

	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatalf("create .ai directory: %v", err)
	}

	if err := os.WriteFile(
		statePath,
		[]byte(strings.TrimSpace(legacy)),
		0644,
	); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("legacy state failed to load: %v", err)
	}

	if !ok {
		t.Fatal("expected legacy task state to load")
	}

	if saved.Version != taskStateVersion {
		t.Fatalf(
			"expected migrated version %d, got %d",
			taskStateVersion,
			saved.Version,
		)
	}

	if len(saved.ActionLedger) != 1 {
		t.Fatalf(
			"expected one legacy ledger entry, got %d",
			len(saved.ActionLedger),
		)
	}

	record := saved.ActionLedger[0]

	if record.ActionKey != "" {
		t.Fatalf(
			"legacy record unexpectedly received ActionKey %q",
			record.ActionKey,
		)
	}

	if !record.OK || record.Exit != 0 {
		t.Fatalf(
			"legacy successful action was not migrated correctly: OK=%v Exit=%d",
			record.OK,
			record.Exit,
		)
	}
}
