package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/projectindex"
)

func TestSnapshotDiffDetectsChanges(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "index.html"),
		[]byte("<html>old</html>\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "old.css"),
		[]byte("body {}\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "index.html"),
		[]byte("<html>new</html>\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "app.js"),
		[]byte("console.log('ok');\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "old.css")); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.SnapshotDiff(baseline)

	if !result.OK {
		t.Fatalf("SnapshotDiff failed: %#v", result)
	}

	if result.Exit != 0 {
		t.Fatalf("SnapshotDiff exit=%d, want 0", result.Exit)
	}

	for _, want := range []string{
		"added:",
		"app.js",
		"modified:",
		"index.html",
		"deleted:",
		"old.css",
	} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("SnapshotDiff output missing %q:\n%s", want, result.Output)
		}
	}
}

func TestSnapshotDiffDetectsNoChanges(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "index.html"),
		[]byte("<html>ok</html>\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.SnapshotDiff(baseline)

	if !result.OK {
		t.Fatalf("SnapshotDiff failed: %#v", result)
	}

	if !strings.Contains(result.Output, "no changes detected") {
		t.Fatalf("unexpected output:\n%s", result.Output)
	}
}

func TestSnapshotDiffRequiresBaseline(t *testing.T) {
	root := t.TempDir()
	router := New(root)

	result := router.SnapshotDiff(nil)

	if result.OK {
		t.Fatal("SnapshotDiff unexpectedly passed without baseline")
	}

	if result.Exit != 1 {
		t.Fatalf("SnapshotDiff exit=%d, want 1", result.Exit)
	}
}
