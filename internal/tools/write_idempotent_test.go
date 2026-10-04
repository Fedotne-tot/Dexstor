package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteIsIdempotentWhenContentAlreadyMatches(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "math.pl")

	content := `sub add {
	my ($a, $b) = @_;
	return 10;
}

1;
`

	if err := os.WriteFile(path, []byte(content), 0444); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Write("math.pl", content)

	if !result.OK {
		t.Fatalf(
			"idempotent write failed: %s",
			result.Error,
		)
	}

	if result.Tool != "write_file" {
		t.Fatalf(
			"expected tool write_file, got %q",
			result.Tool,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != content {
		t.Fatalf(
			"file content changed:\n%s",
			data,
		)
	}
}

func TestWriteStillChangesDifferentContent(t *testing.T) {
	root := t.TempDir()

	path := filepath.Join(root, "math.pl")

	oldContent := "old\n"
	newContent := "new\n"

	if err := os.WriteFile(path, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	r := New(root)

	result := r.Write("math.pl", newContent)

	if !result.OK {
		t.Fatalf(
			"write failed: %s",
			result.Error,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != newContent {
		t.Fatalf(
			"expected new content %q, got %q",
			newContent,
			string(data),
		)
	}
}
