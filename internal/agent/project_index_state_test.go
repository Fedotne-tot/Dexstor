package agent

import (
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/projectindex"
)

func TestProjectIndexPersistsThroughTaskState(t *testing.T) {
	root := t.TempDir()

	projectDir := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(projectDir, ".ai"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(projectDir, "math.pl"),
		[]byte("sub add { $_[0] + $_[1] }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	index, err := projectindex.Build(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	state := &TaskState{
		Version:      taskStateVersion,
		Task:         "index persistence test",
		TaskHash:     taskHash("index persistence test"),
		Project:      projectDir,
		State:        StateImplement,
		ProjectIndex: index,
	}

	if err := saveTaskState(projectDir, state); err != nil {
		t.Fatal(err)
	}

	loaded, ok, err := loadTaskState(projectDir, "index persistence test")
	if err != nil {
		t.Fatal(err)
	}

	if !ok {
		t.Fatal("saved task state was not loaded")
	}

	if loaded.ProjectIndex == nil {
		t.Fatal("project index was not restored")
	}

	if len(loaded.ProjectIndex.Files) != len(index.Files) {
		t.Fatalf(
			"file count mismatch: got %d want %d",
			len(loaded.ProjectIndex.Files),
			len(index.Files),
		)
	}

	if len(loaded.ProjectIndex.Symbols) != len(index.Symbols) {
		t.Fatalf(
			"symbol count mismatch: got %d want %d",
			len(loaded.ProjectIndex.Symbols),
			len(index.Symbols),
		)
	}

	if len(loaded.ProjectIndex.Relations) != len(index.Relations) {
		t.Fatalf(
			"relation count mismatch: got %d want %d",
			len(loaded.ProjectIndex.Relations),
			len(index.Relations),
		)
	}

	restored := New(projectDir, "test-model", 1)
	restored.restoreState(loaded)

	if restored.ProjectIndex == nil {
		t.Fatal("Agent.ProjectIndex was not restored")
	}

	if len(restored.ProjectIndex.Files) != len(index.Files) {
		t.Fatalf(
			"restored agent file count mismatch: got %d want %d",
			len(restored.ProjectIndex.Files),
			len(index.Files),
		)
	}

	if restored.ProjectIndex.Files[0].Hash != index.Files[0].Hash {
		t.Fatalf("restored file hash mismatch")
	}
}
