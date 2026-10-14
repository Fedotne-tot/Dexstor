package agent

import (
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

// PR-2: failedSourcePath must keep recognizing Perl output and, in addition,
// recognize Go test output of the form "file_test.go:42:".

func TestFailedSourcePathStillMatchesPerlOutput(t *testing.T) {
	a := &Agent{}

	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "perl colon form",
			output: "test.pl: FAILED\nexpected 10, got 5",
			want:   "test.pl",
		},
		{
			name:   "perl dot-slash form",
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

func TestFailedSourcePathMatchesGoTestOutput(t *testing.T) {
	a := &Agent{}

	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "go test t.Error form",
			output: "--- FAIL: TestAdd (0.00s)\n    math_test.go:42: expected 10, got 5\nFAIL\n",
			want:   "math_test.go",
		},
		{
			name:   "go test nested path with line",
			output: "src/math_test.go:7: Wrong: 5\nexit status 1\n",
			want:   "src/math_test.go",
		},
		{
			name:   "go test dot-slash prefix",
			output: "./calc_test.go:13: mismatch\n",
			want:   "calc_test.go",
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

func TestFailedSourcePathIgnoresGarbageOutput(t *testing.T) {
	a := &Agent{}

	cases := []struct {
		name   string
		output string
	}{
		{
			name:   "plain failure without file names",
			output: "expected 10, got 5\n",
		},
		{
			name:   "bare .go file without line marker is not a go test failure",
			output: "compiled main.go successfully\n",
		},
		{
			name:   "non-test go file with line number",
			output: "main.go:42: something happened\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No LastWritePath set: fallback must yield empty string,
			// i.e. garbage output does not fabricate a repair target.
			if got := a.failedSourcePath(tc.output); got != "" {
				t.Fatalf("failedSourcePath(%q) = %q, want empty", tc.output, got)
			}
		})
	}

	// With LastWritePath set, garbage output still falls back to it
	// (existing behavior, unchanged by the Go pattern).
	a.LastWritePath = "math.pl"
	if got := a.failedSourcePath("expected 10, got 5\n"); got != "math.pl" {
		t.Fatalf("garbage output should fall back to LastWritePath, got %q", got)
	}
}

func TestFailedSourcePathGoOutputUsesDependencyGraph(t *testing.T) {
	index := &projectindex.Index{
		Files: []projectindex.File{
			{Path: "math.go", Language: "Go"},
			{Path: "math_test.go", Language: "Go"},
		},
		Relations: []projectindex.Relation{
			{
				File:   "math_test.go",
				Target: "math.go",
				Kind:   "require",
			},
		},
	}

	a := &Agent{
		DependencyGraph: dependencygraph.Build(index),
		LastWritePath:   "",
	}

	got := a.failedSourcePath("math_test.go:42: expected 10, got 5\n")

	if got != "math.go" {
		t.Fatalf("expected repair target math.go, got %q", got)
	}
}
