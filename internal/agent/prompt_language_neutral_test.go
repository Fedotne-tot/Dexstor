package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PR-12 regression: the prompt must not hard-wire "test.go" as a required
// file for Go projects, while Perl projects keep recognizing test.go.

func TestGoProjectPromptIsLanguageNeutral(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.com/math\n\ngo 1.21\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte("package math\n\nfunc Add(a, b int) int { return a + b }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "math_test.go"),
		[]byte("package math\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {}\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 1)

	prompt := agent.prompt("fix go project")

	// The old mandatory-file wording must be gone from a pure Go project.
	// Contract note (Perl migration): the forbidden literal is the
	// standalone root entry file name "test.go" exactly as written in the
	// historical mandatory line "You MUST edit test.go ...". After the
	// migration that line is controller-generated, so the only remaining
	// occurrence of the substring in a pure-Go prompt is inside concrete
	// package-test names like "math_test.go" (the underscore-prefixed
	// suffix). Bare occurrences — not part of an *_test.go name — are what
	// must never appear.
	if bareTestGoInPrompt(prompt) {
		t.Fatalf("Go project prompt still mentions test.go:\n%s", prompt)
	}

	// Language-neutral replacement sections must be present.
	for _, want := range []string{
		"PROJECT TEST FILE:",
		"detected test file",
		"*_test.go",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing language-neutral marker %q", want)
		}
	}

	// StateTest path selection must detect the Go test file, not test.go.
	if got := agent.detectTestFile(); got != "math_test.go" {
		t.Fatalf("detectTestFile() = %q, want %q", got, "math_test.go")
	}
}

func TestPerlProjectStillDetectsTestPL(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte("sub add { $_[0] + $_[1] }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte("require 'math.go';\nprint 'PASS\\n';\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 1)

	// Existing Perl fixtures keep working: test.go remains the first
	// candidate and stays mentionable in the prompt examples.
	if got := agent.detectTestFile(); got != "test.go" {
		t.Fatalf("detectTestFile() = %q, want %q", got, "test.go")
	}

	prompt := agent.prompt("fix perl project")

	if !strings.Contains(prompt, "test.go") {
		t.Fatalf("Perl project prompt lost test.go example:\n%s", prompt)
	}

	if !strings.Contains(prompt, "PROJECT TEST FILE:") {
		t.Fatalf("prompt missing language-neutral section:\n%s", prompt)
	}
}

func TestDetectTestFileFallsBackWhenNoTestFileExists(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "main.txt"),
		[]byte("no tests here\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 1)

	// Fallback keeps the historical default; the caller then switches to
	// LastWritePath via the existing os.Stat check (unchanged FSM logic).
	if got := agent.detectTestFile(); got != "test.go" {
		t.Fatalf("detectTestFile() = %q, want fallback %q", got, "test.go")
	}
}
