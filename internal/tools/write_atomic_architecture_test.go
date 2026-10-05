package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePreservesExistingFileOnWriteFailure(t *testing.T) {
	root := t.TempDir()
	r := New(root)

	path := filepath.Join(root, "math.pl")
	original := "ORIGINAL_CONTENT\n"

	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	/*
		Architecture requirement:

		A failed write must not leave the target file partially
		rewritten.

		This test deliberately uses a path whose parent cannot
		be written so Router.Write must fail before replacing the
		existing file.
	*/

	badParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(badParent, []byte("FILE\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := r.Write("not-a-directory/math.pl", "NEW_CONTENT\n")

	if result.OK {
		t.Fatal("expected write to fail")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != original {
		t.Fatalf(
			"existing source file changed after failed write:\nwant=%q\ngot=%q",
			original,
			string(got),
		)
	}
}
