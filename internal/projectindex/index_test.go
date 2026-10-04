package projectindex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProjectIndex(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(path, content string) {
		t.Helper()

		full := filepath.Join(root, path)

		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("math.pl", `sub add {
    my ($a, $b) = @_;
    return $a + $b;
}

1;
`)

	mustWrite("test.pl", `require "math.pl";

my $result = add(2, 3);
print $result;
`)

	mustWrite("lib/helper.pl", `1;
`)

	mustWrite("README", "test project\n")

	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	mustWrite(".git/ignored.txt", "must not be indexed\n")

	index, err := Build(root)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(index.Files) != 4 {
		t.Fatalf("expected 4 files, got %d: %+v", len(index.Files), index.Files)
	}

	foundMath := false
	foundTest := false
	foundHelper := false
	foundReadme := false

	for _, file := range index.Files {
		switch file.Path {
		case "math.pl":
			foundMath = true

			if file.Language != "Perl" {
				t.Fatalf("math.pl language = %q, want Perl", file.Language)
			}

			if file.Type != "pl" {
				t.Fatalf("math.pl type = %q, want pl", file.Type)
			}

			if file.Size == 0 {
				t.Fatal("math.pl size must be non-zero")
			}

			if file.Hash == "" {
				t.Fatal("math.pl hash must not be empty")
			}

		case "test.pl":
			foundTest = true

		case "lib/helper.pl":
			foundHelper = true

		case "README":
			foundReadme = true

		case ".git/ignored.txt":
			t.Fatal(".git contents must not be indexed")
		}
	}

	if !foundMath || !foundTest || !foundHelper || !foundReadme {
		t.Fatalf("expected project files were not all indexed: %+v", index.Files)
	}

	foundAdd := false

	for _, symbol := range index.Symbols {
		if symbol.File == "math.pl" &&
			symbol.Name == "add" &&
			symbol.Kind == "sub" {
			foundAdd = true
		}
	}

	if !foundAdd {
		t.Fatalf("expected Perl sub add in index: %+v", index.Symbols)
	}

	foundRequire := false

	for _, relation := range index.Relations {
		if relation.File == "test.pl" &&
			relation.Target == "math.pl" &&
			relation.Kind == "require" {
			foundRequire = true
		}
	}

	if !foundRequire {
		t.Fatalf("expected require relation in index: %+v", index.Relations)
	}
}

func TestBuildProjectIndexUpdatesHashAfterFileChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "math.pl")

	if err := os.WriteFile(path, []byte("return 5;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	first, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(first.Files))
	}

	firstHash := first.Files[0].Hash

	if err := os.WriteFile(path, []byte("return 10;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	second, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Files) != 1 {
		t.Fatalf("expected 1 file after rebuild, got %d", len(second.Files))
	}

	if second.Files[0].Hash == firstHash {
		t.Fatal("file hash did not change after file modification")
	}
}
