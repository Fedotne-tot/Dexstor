package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PR-12 regression: the prompt must not hard-wire "test.pl" as a required
// file for Go projects, while Perl projects keep recognizing test.pl.

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
	if strings.Contains(prompt, "test.pl") {
		t.Fatalf("Go project prompt still mentions test.pl:\n%s", prompt)
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

	// StateTest path selection must detect the Go test file, not test.pl.
	if got := agent.detectTestFile(); got != "math_test.go" {
		t.Fatalf("detectTestFile() = %q, want %q", got, "math_test.go")
	}
}

func TestPerlProjectStillDetectsTestPL(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "math.pl"),
		[]byte("sub add { $_[0] + $_[1] }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.pl"),
		[]byte("require 'math.pl';\nprint 'PASS\\n';\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 1)

	// Existing Perl fixtures keep working: test.pl remains the first
	// candidate and stays mentionable in the prompt examples.
	if got := agent.detectTestFile(); got != "test.pl" {
		t.Fatalf("detectTestFile() = %q, want %q", got, "test.pl")
	}

	prompt := agent.prompt("fix perl project")

	if !strings.Contains(prompt, "test.pl") {
		t.Fatalf("Perl project prompt lost test.pl example:\n%s", prompt)
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
	if got := agent.detectTestFile(); got != "test.pl" {
		t.Fatalf("detectTestFile() = %q, want fallback %q", got, "test.pl")
	}
}
