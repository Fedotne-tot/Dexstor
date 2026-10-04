package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcilePendingWriteNotCompletedIsNotRecovered(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "math.pl")
	oldContent := "OLD\n"
	newContent := "NEW\n"

	if err := os.WriteFile(path, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:    taskStateVersion,
		State:      StateWriteReq,
		Project:    root,
		ReadFiles:  make(map[string]string),
		FileHashes: make(map[string]string),
		PendingAction: &PendingAction{
			Tool: "write_file",
			Args: map[string]string{
				"path":    "math.pl",
				"content": newContent,
			},
		},
	}

	state.FileHashes["math.pl"] = contentHash(oldContent)

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if reconciled {
		t.Fatal("reconcile incorrectly accepted incomplete pending write")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != oldContent {
		t.Fatalf(
			"reconcile modified filesystem: got %q want %q",
			string(data),
			oldContent,
		)
	}
}

func TestReconcilePendingWriteCompletedIsRecovered(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "math.pl")
	content := "NEW\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:    taskStateVersion,
		State:      StateWriteReq,
		Project:    root,
		ReadFiles:  make(map[string]string),
		FileHashes: make(map[string]string),
		PendingAction: &PendingAction{
			Tool: "write_file",
			Args: map[string]string{
				"path":    "math.pl",
				"content": content,
			},
		},
	}

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if !reconciled {
		t.Fatal("reconcile rejected completed pending write")
	}
}

func TestReconcilePendingWriteMissingFileIsNotRecovered(t *testing.T) {
	root := t.TempDir()

	state := &TaskState{
		Version:    taskStateVersion,
		State:      StateWriteReq,
		Project:    root,
		ReadFiles:  make(map[string]string),
		FileHashes: make(map[string]string),
		PendingAction: &PendingAction{
			Tool: "write_file",
			Args: map[string]string{
				"path":    "math.pl",
				"content": "NEW\n",
			},
		},
	}

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if reconciled {
		t.Fatal("reconcile accepted missing pending write")
	}
}

func TestReconcileFileHashMismatchIsStale(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "math.pl")
	actual := "ACTUAL\n"
	expected := "EXPECTED\n"

	if err := os.WriteFile(path, []byte(actual), 0644); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:   taskStateVersion,
		State:     StateVerifyDiff,
		Project:   root,
		ReadFiles: make(map[string]string),
		FileHashes: map[string]string{
			"math.pl": contentHash(expected),
		},
	}

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if reconciled {
		t.Fatal("reconcile accepted stale filesystem")
	}
}

func TestReconcileFileHashMatchAllowsResume(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "math.pl")
	content := "EXPECTED\n"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:   taskStateVersion,
		State:     StateVerifyDiff,
		Project:   root,
		ReadFiles: make(map[string]string),
		FileHashes: map[string]string{
			"math.pl": contentHash(content),
		},
	}

	a := New(root, "test-model", 10)

	reconciled, err := a.reconcile(state)
	if err != nil {
		t.Fatal(err)
	}

	if !reconciled {
		t.Fatal("reconcile rejected matching filesystem")
	}
}
