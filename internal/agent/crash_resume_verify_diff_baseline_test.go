package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"bratan-go/internal/projectindex"
	"bratan-go/internal/tools"
)

func TestCrashResumeVerifyDiffUsesPersistedBaseline(t *testing.T) {
	root := t.TempDir()

	initial := `package main

func Add(a, b int) int {
	return a + b
}
`

	changed := `package main

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

	mathPath := filepath.Join(root, "math.go")
	testPath := filepath.Join(root, "test.go")

	if err := os.WriteFile(mathPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(testPath, []byte(testProgram), 0644); err != nil {
		t.Fatal(err)
	}

	/*
		Use a real Git repository so VERIFY_DIFF exercises the same
		git_diff path used by production.
	*/
	gitCommands := [][]string{
		{"git", "-C", root, "init"},
		{"git", "-C", root, "config", "user.email", "test@example.com"},
		{"git", "-C", root, "config", "user.name", "DEXSTER Test"},
		{"git", "-C", root, "add", "math.go", "test.go"},
		{"git", "-C", root, "commit", "-m", "initial state"},
	}

	for _, cmd := range gitCommands {
		if output, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", cmd, err, output)
		}
	}

	/*
		The ProjectIndex is deliberately built BEFORE the physical write.
		This is the pre-task semantic baseline that must survive restart.
	*/
	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build baseline project index: %v", err)
	}

	var baselineMathHash string
	for _, file := range baseline.Files {
		if file.Path == "math.go" {
			baselineMathHash = file.Hash
			break
		}
	}

	if baselineMathHash == "" {
		t.Fatal("math.go missing from baseline ProjectIndex")
	}

	task := "Change math.go so add(2, 3) returns 10"

	/*
		Simulate the exact crash window already covered by
		TestCrashAfterPhysicalWriteDoesNotRepeatWrite:

		  1. PendingAction is persisted.
		  2. The physical write completes.
		  3. The process crashes before the post-write checkpoint.

		The important addition here is that the persisted state also
		contains the pre-task ProjectIndex.
	*/
	original := &Agent{
		Model:  "test-model",
		Max:    10,
		Root:   root,
		Router: tools.New(root),

		State:        StateWriteReq,
		ProjectIndex: baseline,

		ReadFiles: map[string]string{
			"math.go": initial,
		},

		PendingAction: &PendingAction{
			Tool:     "write_file",
			BaseHash: contentHash(initial),
			Args: map[string]string{
				"path":    "math.go",
				"content": changed,
			},
		},

		History: []string{
			"inspect_project",
			"read_file:math.go",
		},

		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "math.go\ntest.go",
				OK:        true,
				Exit:      0,
			},
			{
				Iteration: 2,
				Tool:      "read_file",
				ActionKey: makeActionKey("read_file", "math.go"),
				Args:      []string{"math.go"},
				Result:    initial,
				OK:        true,
				Exit:      0,
			},
		},
	}

	if err := original.saveState(task, 3); err != nil {
		t.Fatalf("save pending write checkpoint: %v", err)
	}

	/*
		Physical write happens AFTER persistence and BEFORE restart.
	*/
	if err := os.WriteFile(mathPath, []byte(changed), 0644); err != nil {
		t.Fatalf("simulate physical write: %v", err)
	}

	/*
		New Agent = new process after crash.
		No Ollama server is needed because VERIFY_DIFF and TEST are
		deterministic controller states.
	*/
	resumed := New(root, "test-model", 10)

	if err := resumed.Run(task); err != nil {
		t.Fatalf("resumed Run failed: %v", err)
	}

	if resumed.State != StateDone {
		t.Fatalf(
			"expected StateDone after crash recovery, got %s",
			resumed.State,
		)
	}

	if !resumed.DiffChecked {
		t.Fatal("expected DiffChecked=true after VERIFY_DIFF")
	}

	if resumed.ProjectIndex == nil {
		t.Fatal("restored ProjectIndex is nil")
	}

	/*
		The baseline MUST still contain the original hash even though
		the filesystem now contains the changed file.
	*/
	var restoredMathHash string
	for _, file := range resumed.ProjectIndex.Files {
		if file.Path == "math.go" {
			restoredMathHash = file.Hash
			break
		}
	}

	if restoredMathHash != baselineMathHash {
		t.Fatalf(
			"persisted baseline changed after recovery: got %q want %q",
			restoredMathHash,
			baselineMathHash,
		)
	}

	/*
		Independently verify that baseline -> current produces the
		expected semantic Modified entry.
	*/
	current, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build current project index: %v", err)
	}

	diff := projectindex.Diff(resumed.ProjectIndex, current)

	if !diff.Changed() {
		t.Fatal("expected semantic project diff after recovered write")
	}

	foundModified := false
	for _, path := range diff.Modified {
		if path == "math.go" {
			foundModified = true
			break
		}
	}

	if !foundModified {
		t.Fatalf(
			"expected math.go in semantic Modified set, got %+v",
			diff,
		)
	}

	/*
		The recovered write must be represented in the ledger and the
		final deterministic sequence must contain git_diff + PASS.
	*/
	var sawWrite bool
	var sawDiff bool
	var sawPass bool

	for _, action := range resumed.ActionLedger {
		switch {
		case action.Tool == "write_file" && action.OK && action.Exit == 0:
			sawWrite = true
		case action.Tool == "git_diff" && action.OK && action.Exit == 0:
			sawDiff = true
		case action.Tool == "test" && action.OK && action.Exit == 0:
			sawPass = true
		}
	}

	if !sawWrite {
		t.Fatal("expected recovered write_file action")
	}

	if !sawDiff {
		t.Fatal("expected git_diff action")
	}

	if !sawPass {
		t.Fatal("expected final test PASS")
	}

	data, err := os.ReadFile(mathPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != changed {
		t.Fatalf(
			"recovered file content mismatch:\n%s",
			data,
		)
	}

	t.Logf("recovered history: %v", resumed.History)
}
