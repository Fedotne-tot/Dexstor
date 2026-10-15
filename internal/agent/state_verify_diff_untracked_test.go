package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/projectindex"
)

func TestVerifyDiffAcceptsGitRepoWithOnlyUntrackedChange(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 10 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte{},
		0644,
	); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = root

		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf(
				"git %v failed: %v\n%s",
				args,
				err,
				output,
			)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	runGit("add", "math.go", "test.go", ".gitignore")
	runGit("commit", "-m", "baseline")

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "new.pl"),
		[]byte("print \"UNTRACKED\\n\";\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 4)
	agent.State = StateVerifyDiff
	agent.ProjectIndex = baseline

	/*
		VERIFY_DIFF is being tested in isolation, so seed only
		the mandatory successful actions that precede it in the
		completion contract. These are valid ledger records, not
		History-only markers.
	*/
	agent.ActionLedger = []ActionRecord{
		completionAction(
			0,
			"inspect_project",
			"files=2",
		),
		completionAction(
			0,
			"write_file",
			"write completed",
			"math.go",
			math,
		),
	}

	task := "verify the project change"

	if err := agent.Run(task); err != nil {
		t.Fatalf(
			"semantic change verification rejected untracked-only change: %v",
			err,
		)
	}

	if !agent.DiffChecked {
		t.Fatal("expected DiffChecked=true after semantic change verification")
	}

	if agent.State != StateDone {
		t.Fatalf(
			"expected final state DONE, got %s",
			agent.State,
		)
	}

	foundGitDiff := false
	foundTestPass := false

	for _, action := range agent.ActionLedger {
		if action.Tool == "git_diff" &&
			action.OK &&
			action.Exit == 0 {
			foundGitDiff = true
		}

		if action.Tool == "test" &&
			action.OK &&
			action.Exit == 0 &&
			strings.Contains(action.Result, "PASS") {
			foundTestPass = true
		}
	}

	if !foundGitDiff {
		t.Fatal("expected successful git_diff ledger action")
	}

	if !foundTestPass {
		t.Fatal("expected successful test PASS ledger action")
	}
}

func TestVerifyDiffAcceptsGitRepoWithModifiedTrackedFile(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
}
`

	modifiedMath := `package main

func Add(a, b int) int {
	return 11
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 11 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = root

		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf(
				"git %v failed: %v\n%s",
				args,
				err,
				output,
			)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	runGit("add", "math.go", "test.go")
	runGit("commit", "-m", "baseline")

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(modifiedMath),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 4)
	agent.State = StateVerifyDiff
	agent.ProjectIndex = baseline

	agent.ActionLedger = []ActionRecord{
		completionAction(
			0,
			"inspect_project",
			"files=2",
		),
		completionAction(
			0,
			"write_file",
			"write completed",
			"math.go",
			modifiedMath,
		),
	}

	if err := agent.Run("verify the modified project change"); err != nil {
		t.Fatalf(
			"semantic change verification rejected modified tracked file: %v",
			err,
		)
	}

	if !agent.DiffChecked {
		t.Fatal("expected DiffChecked=true after semantic change verification")
	}

	if agent.State != StateDone {
		t.Fatalf("expected final state DONE, got %s", agent.State)
	}

	foundModified := false
	foundTestPass := false

	for _, action := range agent.ActionLedger {
		if action.Tool == "test" &&
			action.OK &&
			action.Exit == 0 &&
			strings.Contains(action.Result, "PASS") {
			foundTestPass = true
		}
	}

	current, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build current project index: %v", err)
	}

	diff := projectindex.Diff(baseline, current)
	for _, path := range diff.Modified {
		if path == "math.go" {
			foundModified = true
			break
		}
	}

	if !foundModified {
		t.Fatalf("expected math.go in Modified, got %#v", diff)
	}

	if !foundTestPass {
		t.Fatal("expected successful test PASS ledger action")
	}
}

func TestVerifyDiffAcceptsGitRepoWithDeletedTrackedFile(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 10 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	helper := `print "helper\n";
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "helper.pl"),
		[]byte(helper),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	runGit := func(args ...string) {
		t.Helper()

		cmd := exec.Command("git", args...)
		cmd.Dir = root

		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf(
				"git %v failed: %v\n%s",
				args,
				err,
				output,
			)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")
	runGit("add", "math.go", "test.go", "helper.pl")
	runGit("commit", "-m", "baseline")

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "helper.pl")); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 4)
	agent.State = StateVerifyDiff
	agent.ProjectIndex = baseline

	agent.ActionLedger = []ActionRecord{
		completionAction(
			0,
			"inspect_project",
			"files=3",
		),
		completionAction(
			0,
			"write_file",
			"write completed",
			"math.go",
			math,
		),
	}

	if err := agent.Run("verify the deleted project change"); err != nil {
		t.Fatalf(
			"semantic change verification rejected deleted tracked file: %v",
			err,
		)
	}

	if !agent.DiffChecked {
		t.Fatal("expected DiffChecked=true after semantic change verification")
	}

	if agent.State != StateDone {
		t.Fatalf("expected final state DONE, got %s", agent.State)
	}

	current, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build current project index: %v", err)
	}

	diff := projectindex.Diff(baseline, current)

	foundDeleted := false
	for _, path := range diff.Deleted {
		if path == "helper.pl" {
			foundDeleted = true
			break
		}
	}

	if !foundDeleted {
		t.Fatalf("expected helper.pl in Deleted, got %#v", diff)
	}

	foundTestPass := false
	for _, action := range agent.ActionLedger {
		if action.Tool == "test" &&
			action.OK &&
			action.Exit == 0 &&
			strings.Contains(action.Result, "PASS") {
			foundTestPass = true
			break
		}
	}

	if !foundTestPass {
		t.Fatal("expected successful test PASS ledger action")
	}
}

func TestVerifyDiffAcceptsNonGitProjectChange(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
}
`

	changedMath := `package main

func Add(a, b int) int {
	return 11
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 11 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(changedMath),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 4)
	agent.State = StateVerifyDiff
	agent.ProjectIndex = baseline

	agent.ActionLedger = []ActionRecord{
		completionAction(
			0,
			"inspect_project",
			"files=2",
		),
		completionAction(
			0,
			"write_file",
			"write completed",
			"math.go",
			changedMath,
		),
	}

	if err := agent.Run("verify non-git project change"); err != nil {
		t.Fatalf(
			"semantic change verification rejected non-git project: %v",
			err,
		)
	}

	if !agent.DiffChecked {
		t.Fatal("expected DiffChecked=true after semantic change verification")
	}

	if agent.State != StateDone {
		t.Fatalf("expected final state DONE, got %s", agent.State)
	}

	foundSnapshotDiff := false
	foundTestPass := false

	for _, action := range agent.ActionLedger {
		if action.Tool == "git_diff" &&
			action.OK &&
			action.Exit == 0 {
			foundSnapshotDiff = true
		}

		if action.Tool == "test" &&
			action.OK &&
			action.Exit == 0 &&
			strings.Contains(action.Result, "PASS") {
			foundTestPass = true
		}
	}

	if !foundSnapshotDiff {
		t.Fatal("expected successful git_diff ledger action")
	}

	if !foundTestPass {
		t.Fatal("expected successful test PASS ledger action")
	}

	current, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build current project index: %v", err)
	}

	diff := projectindex.Diff(baseline, current)

	foundModified := false
	for _, path := range diff.Modified {
		if path == "math.go" {
			foundModified = true
			break
		}
	}

	if !foundModified {
		t.Fatalf(
			"expected math.go in Modified, got %#v",
			diff,
		)
	}
}

func TestVerifyDiffRejectsNoProjectChange(t *testing.T) {
	root := t.TempDir()

	math := `package main

func Add(a, b int) int {
	return 10
}
`

	testProgram := `package main

import "fmt"

func main() {
	result := Add(2, 3)

	if result != 10 {
		fmt.Printf("WRONG: %d\n", result)
		panic("test failed")
	}

	fmt.Println("PASS")
}
`

	if err := os.WriteFile(
		filepath.Join(root, "math.go"),
		[]byte(math),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.go"),
		[]byte(testProgram),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	agent := New(root, "test-model", 4)
	agent.State = StateVerifyDiff
	agent.ProjectIndex = baseline

	agent.ActionLedger = []ActionRecord{
		completionAction(
			0,
			"inspect_project",
			"files=2",
		),
	}

	err = agent.Run("verify unchanged project")

	if err == nil {
		t.Fatal("expected VERIFY_DIFF to reject unchanged project")
	}

	if agent.DiffChecked {
		t.Fatal("DiffChecked must remain false when no project change exists")
	}

	if agent.State == StateTest || agent.State == StateDone {
		t.Fatalf(
			"VERIFY_DIFF must not advance to TEST/DONE on unchanged project, got %s",
			agent.State,
		)
	}

	for _, action := range agent.ActionLedger {
		if action.Tool == "test" {
			t.Fatal("test must not run when VERIFY_DIFF detects no project change")
		}
	}
}
