package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRejectsProtectedTestFile(t *testing.T) {
	root := t.TempDir()

	testPath := filepath.Join(root, "test.pl")
	original := `print "PASS\n";`

	if err := os.WriteFile(testPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.Write(
		"test.pl",
		`print "THIS MUST NOT BE WRITTEN\n";`,
	)

	if result.OK {
		t.Fatal("expected write_file to reject protected test.pl")
	}

	if !strings.Contains(result.Error, "protected") {
		t.Fatalf(
			"expected protected-file error, got: %q",
			result.Error,
		)
	}

	data, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != original {
		t.Fatalf(
			"protected test.pl was modified:\noriginal:\n%s\nafter:\n%s",
			original,
			data,
		)
	}
}

func TestWriteAllowsNormalSourceFile(t *testing.T) {
	root := t.TempDir()

	router := New(root)

	result := router.Write(
		"math.pl",
		`sub add { return 1 + 2; }`,
	)

	if !result.OK {
		t.Fatalf(
			"expected normal source write to succeed, got: %q",
			result.Error,
		)
	}

	data, err := os.ReadFile(filepath.Join(root, "math.pl"))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != `sub add { return 1 + 2; }` {
		t.Fatalf("math.pl contains unexpected content: %s", data)
	}
}
