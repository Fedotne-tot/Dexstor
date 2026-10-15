package agent

import "testing"

func TestActionLedgerSkipsOnlyIdenticalSuccessfulAction(t *testing.T) {
	a := &Agent{}

	path := "math.go"
	content := "return 10;\n"
	key := makeActionKey("write_file", path, content)

	a.ActionLedger = append(a.ActionLedger, ActionRecord{
		Iteration: 3,
		Tool:      "write_file",
		ActionKey: key,
		Args:      []string{path, content},
		Result:    "write completed",
		OK:        true,
		Exit:      0,
	})

	found, ok := a.hasSuccessfulAction("write_file", path, content)
	if !ok {
		t.Fatal("expected successful identical action to be detected")
	}

	if found.ActionKey != key {
		t.Fatalf("unexpected ActionKey: got %q want %q", found.ActionKey, key)
	}

	differentContent := "return 11;\n"

	if _, ok := a.hasSuccessfulAction("write_file", path, differentContent); ok {
		t.Fatal("different content must not be treated as duplicate")
	}
}

func TestActionLedgerAllowsRetryAfterFailure(t *testing.T) {
	a := &Agent{}

	path := "math.go"
	content := "return 10;\n"
	key := makeActionKey("write_file", path, content)

	a.ActionLedger = append(a.ActionLedger, ActionRecord{
		Iteration: 3,
		Tool:      "write_file",
		ActionKey: key,
		Args:      []string{path, content},
		Result:    "write failed",
		Error:     "permission denied",
		OK:        false,
		Exit:      1,
	})

	if _, ok := a.hasSuccessfulAction("write_file", path, content); ok {
		t.Fatal("failed action must not suppress retry")
	}
}

func TestActionLedgerPersistsKeyInRecord(t *testing.T) {
	a := &Agent{}

	path := "math.go"
	content := "return 10;\n"
	key := makeActionKey("write_file", path, content)

	a.ActionLedger = append(a.ActionLedger, ActionRecord{
		Iteration: 4,
		Tool:      "write_file",
		ActionKey: key,
		Args:      []string{path, content},
		Result:    "write completed",
		OK:        true,
		Exit:      0,
	})

	record := a.ActionLedger[len(a.ActionLedger)-1]

	if record.ActionKey != key {
		t.Fatalf("unexpected ActionKey: got %q want %q", record.ActionKey, key)
	}

	if record.Tool != "write_file" {
		t.Fatalf("unexpected Tool: got %q", record.Tool)
	}

	if len(record.Args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(record.Args))
	}

	if record.Args[0] != path {
		t.Fatalf("unexpected path arg: got %q want %q", record.Args[0], path)
	}

	if record.Args[1] != content {
		t.Fatalf("unexpected content arg: got %q want %q", record.Args[1], content)
	}

	if makeActionKey(record.Tool, record.Args...) != record.ActionKey {
		t.Fatal("stored ActionKey does not match stored Tool + Args")
	}
}
