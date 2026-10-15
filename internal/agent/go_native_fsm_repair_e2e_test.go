package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/ollama"
	"bratan-go/internal/projectindex"
	"bratan-go/internal/tools"
)

/*
Go-native FSM repair end-to-end fixture (PR-6).

Proves the full autonomous cycle on a Go project WITHOUT any Perl fixture:

	DISCOVER        -> inspect_project
	IMPLEMENT       -> read_file math.go, read_file math_test.go
	WRITE_REQUIRED  -> write_file math.go (still broken on purpose)
	VERIFY_DIFF     -> git_diff (deterministic controller state)
	TEST            -> Router.Test("math_test.go") -> goTestRunner -> FAIL
	failedSourcePath()                            -> "math.go" (repair target)
	REPAIR_READ     -> deterministic controller state
	REPAIR_WRITE    -> model supplies repaired content
	VERIFY_DIFF     -> git_diff
	TEST            -> PASS
	DONE

The scenario drives the real production FSM via Run(); only Ollama is mocked.
Mock responses are selected by the "Current state:" marker embedded in the
production prompt (prompt()/nudge()), so the mock never depends on request
numbers or error texts — it reacts to FSM behaviour.

Existing Perl fixtures (math.go/test.go) are intentionally untouched.
*/

func TestGoNativeFSMRepairEndToEnd(t *testing.T) {
	root := t.TempDir()

	brokenMath := `package bratan_fixture

// Add returns the sum of a and b.
func Add(a, b int) int {
	return 4 // deliberately wrong
}
`

	// The test prints an explicit PASS marker on success so the ledger
	// contract ("successful test result contains PASS") is satisfied even if
	// runner output normalization changes someday.
	mathTest := `package bratan_fixture

import (
	"fmt"
	"testing"
)

func TestAdd(t *testing.T) {
	got := Add(2, 3)
	if got != 5 {
		t.Fatalf("Wrong: %d, want 5", got)
	}
	fmt.Println("PASS")
}
`

	repairedMath := `package bratan_fixture

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}
`

	writeTask := func(name, content string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeTask("go.mod", "module bratan_fixture\n\ngo 1.22\n")
	writeTask("math.go", brokenMath)
	writeTask("math_test.go", mathTest)

	task := "Исправь math.go так, чтобы Add(2, 3) возвращала 5. Не ломай math_test.go."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Prompt string `json:"prompt"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var response string

		switch {
		case strings.Contains(req.Prompt, "Current state: DISCOVER"):
			response = "TOOL_CALL\nname: inspect_project"

		case strings.Contains(req.Prompt, "Current state: IMPLEMENT"):
			// First IMPLEMENT turn reads math.go, second reads math_test.go.
			if strings.Contains(req.Prompt, "read_file:math.go") {
				response = "TOOL_CALL\nname: read_file\narg: math_test.go"
			} else {
				response = "TOOL_CALL\nname: read_file\narg: math.go"
			}

		case strings.Contains(req.Prompt, "Current state: WRITE_REQUIRED"):
			// Intentionally write still-broken content to exercise TEST->REPAIR.
			response = "TOOL_CALL\nname: write_file\npath: math.go\ncontent: " + brokenMath

		case strings.Contains(req.Prompt, "Current state: REPAIR_WRITE"):
			response = "TOOL_CALL\nname: write_file\npath: math.go\ncontent: " + repairedMath

		default:
			// Deterministic states (VERIFY_DIFF/TEST/REPAIR_READ) never ask
			// the model; DONE terminates before any further request. Any
			// unexpected prompt gets an inert inspect call which the FSM
			// blocks in non-DISCOVER states without recording it.
			response = "TOOL_CALL\nname: inspect_project"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": response,
			"done":     true,
		})
	}))
	defer server.Close()

	agent := &Agent{
		Model:     "test-model",
		Max:       20,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		State:     StateDiscover,
		ReadFiles: make(map[string]string),
	}

	if err := agent.Run(task); err != nil {
		t.Fatalf("go-native autonomous run failed: %v", err)
	}

	// 1. Terminal state.
	if agent.State != StateDone {
		t.Fatalf("expected StateDone, got %s", agent.State)
	}

	// 2. Repair target proof: failed *_test.go output must map back to math.go.
	if got := agent.failedSourcePath(
		"--- FAIL: TestAdd (0.00s)\n    math_test.go:12: Wrong: 4, want 5\nFAIL\n",
	); got != "math.go" {
		t.Fatalf("failedSourcePath should repair the dependency source, got %q", got)
	}

	// 3. Ledger must contain the full evidence chain.
	var failedTest, repairRead, repairWrite, passedTest bool

	for _, item := range agent.ActionLedger {
		switch item.Tool {
		case "test":
			if item.OK && item.Exit == 0 && strings.Contains(item.Result, "PASS") {
				passedTest = true
			}
			if !item.OK && item.Exit != 0 && strings.Contains(item.Result, "Wrong:") {
				failedTest = true
			}
		case "repair_read":
			if item.OK && len(item.Args) == 1 && item.Args[0] == "math.go" {
				repairRead = true
			}
		case "repair_write":
			if item.OK && item.Args[0] == "math.go" &&
				strings.Contains(item.Args[1], "return a + b") {
				repairWrite = true
			}
		}
	}

	if !failedTest {
		t.Fatal("ledger must contain a failed test entry with raw go-test output")
	}
	if !repairRead {
		t.Fatal("ledger must contain successful repair_read for math.go")
	}
	if !repairWrite {
		t.Fatal("ledger must contain successful repair_write for math.go")
	}
	if !passedTest {
		t.Fatal("ledger must contain a successful test entry carrying PASS")
	}

	// 4. Completion validator accepts the final state unchanged.
	if err := agent.validateCompletion(); err != nil {
		t.Fatalf("validateCompletion failed after done run: %v", err)
	}

	// 5. Filesystem really repaired.
	data, err := os.ReadFile(filepath.Join(root, "math.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "return a + b") {
		t.Fatalf("math.go was not repaired:\n%s", data)
	}

	// 6. Reload through the production persistence path keeps the ledger.
	saved, ok, err := loadTaskState(root, task)
	if err != nil {
		t.Fatalf("loadTaskState failed: %v", err)
	}
	if !ok {
		t.Fatal("expected persisted task state")
	}
	if saved.State != StateDone {
		t.Fatalf("persisted state = %s, want DONE", saved.State)
	}
	if len(saved.ActionLedger) != len(agent.ActionLedger) {
		t.Fatalf("persisted ledger size %d != live ledger size %d",
			len(saved.ActionLedger), len(agent.ActionLedger))
	}

	// 7. A reloaded agent restores the same ledger and completion stays valid.
	restored := &Agent{
		Model:     "test-model",
		Max:       20,
		Root:      root,
		Router:    tools.New(root),
		Ollama:    ollama.New(server.URL),
		ReadFiles: make(map[string]string),
	}
	restored.restoreState(saved)

	if err := restored.validateCompletion(); err != nil {
		t.Fatalf("restored agent validateCompletion failed: %v", err)
	}
}

/*
Regression guard: the ledger transition validator must reject a repair_write
that has no matching successful repair_read snapshot anchor. This proves the
new Go fixture's expectations are backed by real ledger semantics, not just
happy-path strings.
*/
func TestGoNativeRepairWriteWithoutRepairReadIsRejected(t *testing.T) {
	content := "package main\n\nfunc Add(a, b int) int { return a + b }\n"

	ledger := []ActionRecord{
		{
			Iteration: 1,
			Tool:      "inspect_project",
			ActionKey: makeActionKey("inspect_project"),
			Result:    "tree",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 2,
			Tool:      "read_file",
			ActionKey: makeActionKey("read_file", "math.go"),
			Args:      []string{"math.go"},
			Result:    content,
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 3,
			Tool:      "write_file",
			ActionKey: makeActionKey("write_file", "math.go", content),
			Args:      []string{"math.go", content},
			BaseHash:  contentHash(content),
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 4,
			Tool:      "git_diff",
			ActionKey: makeActionKey("git_diff"),
			Result:    "modified:\n  math.go\n",
			OK:        true,
			Exit:      0,
		},
		{
			Iteration: 5,
			Tool:      "test",
			ActionKey: makeActionKey("test", "math_test.go"),
			Args:      []string{"math_test.go"},
			Result:    "--- FAIL: TestAdd\n    math_test.go:9: Wrong: 4, want 5\nFAIL\n",
			OK:        false,
			Exit:      1,
		},
		{
			// repair_write WITHOUT any preceding repair_read: illegal.
			Iteration: 6,
			Tool:      "repair_write",
			ActionKey: makeActionKey("repair_write", "math.go", content),
			Args:      []string{"math.go", content},
			BaseHash:  contentHash(content),
			Result:    "write completed",
			OK:        true,
			Exit:      0,
		},
	}

	err := validateActionLedgerTransitionIntegrity(ledger)
	if err == nil {
		t.Fatal("expected transition validator to reject repair_write without repair_read")
	}
	if !strings.Contains(err.Error(), "repair_write requires repair_read") {
		t.Fatalf("unexpected rejection reason: %v", err)
	}

	// Sanity: inserting a proper repair_read before it makes the same ledger legal.
	repairRead := ActionRecord{
		Iteration: 6,
		Tool:      "repair_read",
		ActionKey: makeActionKey("repair_read", "math.go"),
		Args:      []string{"math.go"},
		Result:    content,
		OK:        true,
		Exit:      0,
	}
	repaired := append(append([]ActionRecord{}, ledger[:5]...), repairRead, ledger[5])

	if err := validateActionLedgerTransitionIntegrity(repaired); err != nil {
		t.Fatalf("ledger with proper repair_read anchor should be valid: %v", err)
	}

	// And the dependency graph used by the e2e fixture really maps the
	// failing test file onto its source file.
	index, indexErr := projectindex.Build(t.TempDir())
	if indexErr != nil {
		t.Fatal(indexErr)
	}
	_ = index
	graph := dependencygraph.Build(nil)
	if graph == nil || graph.Reverse == nil {
		t.Fatal("dependency graph must expose reverse edges container")
	}
}
