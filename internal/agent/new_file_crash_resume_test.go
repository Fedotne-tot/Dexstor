package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bratan-go/internal/projectindex"
	"bratan-go/internal/tools"
)

func TestCrashResumeNewFileUsesNonCASWriteAndDetectsAdded(t *testing.T) {
	root := t.TempDir()

	// Network isolation: New() resolves the Ollama endpoint from
	// BRATAN_OLLAMA_URL. This recovery scenario must never reach a real
	// 127.0.0.1:11434 — any unexpected model request here is a contract
	// violation, so fail loudly instead of dialing out.
	modelCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		http.Error(w, "unexpected model request in crash-resume test", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("BRATAN_OLLAMA_URL", server.URL)

	task := "Create hello.txt with HELLO_BRATAN"

	baseline, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build empty baseline: %v", err)
	}

	agent := &Agent{
		Model:  "test-model",
		Max:    10,
		Root:   root,
		Router: tools.New(root),

		State:        StateWriteReq,
		ProjectIndex: baseline,

		ReadFiles: map[string]string{},

		PendingAction: &PendingAction{
			Tool:     "write_file",
			BaseHash: "",
			Args: map[string]string{
				"path":    "hello.txt",
				"content": "HELLO_BRATAN",
			},
		},

		History: []string{
			"inspect_project",
		},

		ActionLedger: []ActionRecord{
			{
				Iteration: 1,
				Tool:      "inspect_project",
				ActionKey: makeActionKey("inspect_project"),
				Result:    "hello.txt",
				OK:        true,
				Exit:      0,
			},
		},
	}

	if err := agent.saveState(task, 2); err != nil {
		t.Fatalf("save pending new file checkpoint: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "hello.txt"),
		[]byte("HELLO_BRATAN"),
		0644,
	); err != nil {
		t.Fatalf("simulate physical write: %v", err)
	}

	resumed := New(root, "test-model", 10)

	if err := resumed.Run(task); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	if resumed.State != StateDone {
		t.Fatalf(
			"expected StateDone, got %s",
			resumed.State,
		)
	}

	if !resumed.DiffChecked {
		t.Fatal("expected VERIFY_DIFF after recovery")
	}

	current, err := projectindex.Build(root)
	if err != nil {
		t.Fatalf("build current index: %v", err)
	}

	diff := projectindex.Diff(
		resumed.ProjectIndex,
		current,
	)

	foundAdded := false

	for _, path := range diff.Added {
		if path == "hello.txt" {
			foundAdded = true
			break
		}
	}

	if !foundAdded {
		t.Fatalf(
			"expected hello.txt in Added set, got %+v",
			diff,
		)
	}

	foundWrite := false

	for _, action := range resumed.ActionLedger {
		if action.Tool == "write_file" &&
			action.OK &&
			action.Exit == 0 &&
			len(action.Args) > 0 &&
			action.Args[0] == "hello.txt" {

			foundWrite = true
			break
		}
	}

	if !foundWrite {
		t.Fatal("expected recovered write_file action")
	}

	data, err := os.ReadFile(
		filepath.Join(root, "hello.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "HELLO_BRATAN" {
		t.Fatalf(
			"unexpected recovered content: %q",
			data,
		)
	}
}
