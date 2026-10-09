package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitDiffTestCommand(t *testing.T, root string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func TestGitDiffDetectsTrackedModificationAndNewFile(t *testing.T) {
	root := t.TempDir()

	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}

	runGitDiffTestCommand(t, root, "init", "-q")
	runGitDiffTestCommand(t, root, "config", "user.email", "test@example.com")
	runGitDiffTestCommand(t, root, "config", "user.name", "DEXSTER Test")
	runGitDiffTestCommand(t, root, "add", "tracked.txt")
	runGitDiffTestCommand(t, root, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(tracked, []byte("new\n"), 0644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}

	newFile := filepath.Join(root, "new.txt")
	if err := os.WriteFile(newFile, []byte("new file\n"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	router := New(root)
	result := router.GitDiff(".")

	if !result.OK {
		t.Fatalf("GitDiff() failed: %s", result.Error)
	}

	if result.Exit != 0 {
		t.Fatalf("GitDiff() exit = %d, want 0", result.Exit)
	}

	if !strings.Contains(result.Output, "tracked.txt") {
		t.Fatalf("GitDiff() output does not contain modified tracked file:\n%s", result.Output)
	}

	if strings.Contains(result.Output, "new.txt") {
		t.Fatalf(
			"GitDiff() unexpectedly reports untracked file; test documents current git diff semantics:\n%s",
			result.Output,
		)
	}
}

func TestGitDiffReturnsEmptyOutputWhenOnlyNewFileExists(t *testing.T) {
	root := t.TempDir()

	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("stable\n"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}

	runGitDiffTestCommand(t, root, "init", "-q")
	runGitDiffTestCommand(t, root, "config", "user.email", "test@example.com")
	runGitDiffTestCommand(t, root, "config", "user.name", "DEXSTER Test")
	runGitDiffTestCommand(t, root, "add", "tracked.txt")
	runGitDiffTestCommand(t, root, "commit", "-q", "-m", "initial")

	newFile := filepath.Join(root, "new.txt")
	if err := os.WriteFile(newFile, []byte("new file\n"), 0644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	router := New(root)
	result := router.GitDiff(".")

	if !result.OK {
		t.Fatalf("GitDiff() failed: %s", result.Error)
	}

	if result.Exit != 0 {
		t.Fatalf("GitDiff() exit = %d, want 0", result.Exit)
	}

	if result.Output != "" {
		t.Fatalf(
			"GitDiff() output = %q, want empty output for only an untracked file",
			result.Output,
		)
	}
}
