package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileFileHashMissingFileIsStale(t *testing.T) {
	root := t.TempDir()

	existingPath := filepath.Join(root, "math.pl")
	if err := os.WriteFile(
		existingPath,
		[]byte("EXPECTED\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:   taskStateVersion,
		State:     StateVerifyDiff,
		Project:   root,
		ReadFiles: make(map[string]string),
		FileHashes: map[string]string{
			"math.pl": contentHash("EXPECTED\n"),
			"missing.pl": contentHash(
				"EXPECTED MISSING\n",
			),
		},
	}

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if reconciled {
		t.Fatal("reconcile accepted state with a missing hashed file")
	}
}
