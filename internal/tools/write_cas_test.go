package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func testContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestWriteRejectsStaleSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "math.pl")

	if err := os.WriteFile(path, []byte("ORIGINAL\n"), 0644); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	// Trusted snapshot A.
	read := router.Read("math.pl")
	if !read.OK {
		t.Fatalf("read failed: %+v", read)
	}

	// The file changes after the trusted read.
	if err := os.WriteFile(path, []byte("EXEC_CHANGED\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A write based on the stale snapshot must be rejected.
	baseHash := testContentHash(read.Content)

	result := router.WriteWithBaseHash(
		"math.pl",
		"ORIGINAL\n",
		baseHash,
	)

	if result.OK {
		t.Fatalf(
			"stale write was accepted: result=%+v",
			result,
		)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != "EXEC_CHANGED\n" {
		t.Fatalf(
			"stale write modified the file: %q",
			string(after),
		)
	}
}
