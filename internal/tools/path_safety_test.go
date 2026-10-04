package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(
		outsideFile,
		[]byte("SECRET"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	result := r.Read("link/secret.txt")

	if result.OK {
		t.Fatal("read_file accepted a symlink path escaping project root")
	}

	if result.Content == "SECRET" {
		t.Fatal("read_file exposed content outside project root")
	}
}

func TestWriteRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	result := r.Write("link/escaped.txt", "ESCAPED")

	if result.OK {
		t.Fatal("write_file accepted a symlink path escaping project root")
	}

	outsideFile := filepath.Join(outside, "escaped.txt")
	if _, err := os.Stat(outsideFile); err == nil {
		t.Fatal("write_file created a file outside project root")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
