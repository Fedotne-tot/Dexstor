package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecCannotModifyProtectedTestFile(t *testing.T) {
	root := t.TempDir()

	original := "ORIGINAL_TEST_CONTENT\n"
	testPath := filepath.Join(root, "test.pl")

	if err := os.WriteFile(testPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Exec("cat > test.pl <<'EOF'\nMODIFIED_BY_EXEC\nEOF")
	if result.OK {
		t.Fatalf("expected exec protection failure, got success: %+v", result)
	}

	got, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != original {
		t.Fatalf("protected test.pl was modified through exec:\nwant:\n%s\ngot:\n%s", original, got)
	}
}

func TestExecCannotDeleteProtectedTestFile(t *testing.T) {
	root := t.TempDir()

	original := "ORIGINAL_TEST_CONTENT\n"
	testPath := filepath.Join(root, "test.pl")

	if err := os.WriteFile(testPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Exec("rm -f test.pl")

	if result.OK {
		t.Fatalf("expected exec protection failure, got success: %+v", result)
	}

	got, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("protected test.pl was deleted through exec: %v", err)
	}

	if string(got) != original {
		t.Fatalf("protected test.pl was modified through exec:\nwant:\n%s\ngot:\n%s", original, got)
	}
}

func TestExecCannotModifyProtectedTestFileWithPerl(t *testing.T) {
	root := t.TempDir()

	original := "ORIGINAL_TEST_CONTENT\n"
	testPath := filepath.Join(root, "test.pl")

	if err := os.WriteFile(testPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Exec(`perl -pi -e 's/ORIGINAL/MODIFIED/' test.pl`)

	if result.OK {
		t.Fatalf("expected exec protection failure, got success: %+v", result)
	}

	got, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != original {
		t.Fatalf("protected test.pl was modified through perl exec:\nwant:\n%s\ngot:\n%s", original, got)
	}
}

func TestExecProtectedFailureIsExplicit(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "test.pl"),
		[]byte("ORIGINAL\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Exec("printf 'BAD' > test.pl")

	if result.OK {
		t.Fatal("expected protected-file exec to fail")
	}

	if strings.TrimSpace(result.Error) == "" &&
		strings.TrimSpace(result.Output) == "" {
		t.Fatalf("expected an explicit error/result for protected exec, got: %+v", result)
	}
}
