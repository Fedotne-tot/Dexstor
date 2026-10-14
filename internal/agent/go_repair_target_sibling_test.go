package agent

import (
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/projectindex"
)

// PR-10B: failedSourcePath must steer Go repair cycles at the sibling source
// file (foo_test.go -> foo.go), while Perl resolution and the *_test.go
// fallback stay exactly as before.

func TestGoRepairTargetResolvesSiblingSource(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "solution.go"), []byte("package solution\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{Root: root}

	output := "--- FAIL: TestAdd (0.00s)\n    solution_test.go:12: expected 5, got 4\nFAIL\n"

	got := a.failedSourcePath(output)
	if got != "solution.go" {
		t.Fatalf("failedSourcePath(%q) = %q, want %q", output, got, "solution.go")
	}
}

func TestGoRepairTargetResolvesViaProjectIndex(t *testing.T) {
	a := &Agent{
		ProjectIndex: &projectindex.Index{
			Files: []projectindex.File{
				{Path: "pkg/math.go"},
				{Path: "pkg/math_test.go"},
			},
		},
	}

	output := "pkg/math_test.go:7: Wrong: 5\nexit status 1\n"

	got := a.failedSourcePath(output)
	if got != "pkg/math.go" {
		t.Fatalf("failedSourcePath(%q) = %q, want %q", output, got, "pkg/math.go")
	}
}

func TestGoRepairTargetFallsBackToTestFileWhenNoSource(t *testing.T) {
	root := t.TempDir()

	// Only the test file exists; there is no standalone_math_test.go sibling.
	if err := os.WriteFile(filepath.Join(root, "standalone_math_test.go"), []byte("package m\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{Root: root}

	output := "standalone_math_test.go:9: expected 5, got 4\nFAIL\n"

	got := a.failedSourcePath(output)
	if got != "standalone_math_test.go" {
		t.Fatalf("failedSourcePath(%q) = %q, want fallback to %q", output, got, "standalone_math_test.go")
	}
}

func TestPerlRepairTargetUnchangedByGoResolution(t *testing.T) {
	root := t.TempDir()

	// Even if a same-named .go file exists, Perl paths must never enter the
	// Go sibling-source branch.
	if err := os.WriteFile(filepath.Join(root, "math.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{Root: root}

	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "perl colon form stays .pl",
			output: "test.pl: FAILED\nexpected 10, got 5",
			want:   "test.pl",
		},
		{
			name:   "perl dot-slash form stays .pl",
			output: "./math.pl did not return a true value at line 3",
			want:   "math.pl",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.failedSourcePath(tc.output)
			if got != tc.want {
				t.Fatalf("failedSourcePath(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}
