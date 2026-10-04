package parser

import "testing"

func TestParseInspectProject(t *testing.T) {
	text := `TOOL_CALL
name: inspect_project
END_TOOL_CALL`

	call, ok := Parse(text)
	if !ok {
		t.Fatal("expected TOOL_CALL to parse")
	}

	if call.Name != "inspect_project" {
		t.Fatalf("expected inspect_project, got %q", call.Name)
	}

	if len(call.Args) != 0 {
		t.Fatalf("expected no args, got %#v", call.Args)
	}
}

func TestParseReadFile(t *testing.T) {
	text := `TOOL_CALL
name: read_file
arg: math.pl
END_TOOL_CALL`

	call, ok := Parse(text)
	if !ok {
		t.Fatal("expected TOOL_CALL to parse")
	}

	if call.Name != "read_file" {
		t.Fatalf("expected read_file, got %q", call.Name)
	}

	if got := call.Args["arg"]; got != "math.pl" {
		t.Fatalf("expected arg math.pl, got %q", got)
	}
}

func TestParseWriteFileMultilineContent(t *testing.T) {
	text := `TOOL_CALL
name: write_file
path: math.pl
content: sub add {
    my ($a, $b) = @_;
    return 10;
}

1;
END_TOOL_CALL`

	call, ok := Parse(text)
	if !ok {
		t.Fatal("expected TOOL_CALL to parse")
	}

	if call.Name != "write_file" {
		t.Fatalf("expected write_file, got %q", call.Name)
	}

	if got := call.Args["path"]; got != "math.pl" {
		t.Fatalf("expected path math.pl, got %q", got)
	}

	want := `sub add {
    my ($a, $b) = @_;
    return 10;
}

1;`

	if got := call.Args["content"]; got != want {
		t.Fatalf("content mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseAdditionalArguments(t *testing.T) {
	text := `TOOL_CALL
name: exec
command: perl test.pl
expected: PASS
timeout: 30
END_TOOL_CALL`

	call, ok := Parse(text)
	if !ok {
		t.Fatal("expected TOOL_CALL to parse")
	}

	if call.Name != "exec" {
		t.Fatalf("expected exec, got %q", call.Name)
	}

	want := map[string]string{
		"command":  "perl test.pl",
		"expected": "PASS",
		"timeout":  "30",
	}

	for key, expected := range want {
		if got := call.Args[key]; got != expected {
			t.Fatalf("arg %s: expected %q, got %q", key, expected, got)
		}
	}
}

func TestParseRejectsMissingToolName(t *testing.T) {
	text := `TOOL_CALL
arg: math.pl
END_TOOL_CALL`

	if _, ok := Parse(text); ok {
		t.Fatal("expected malformed TOOL_CALL to be rejected")
	}
}

func TestParseRejectsTextWithoutToolCall(t *testing.T) {
	text := `The model answered normally without requesting a tool.`

	if _, ok := Parse(text); ok {
		t.Fatal("expected ordinary text to be rejected")
	}
}

func TestParseIgnoresTextAfterEndToolCall(t *testing.T) {
	text := `TOOL_CALL
name: read_file
arg: math.pl
END_TOOL_CALL

name: write_file
path: evil.pl`

	call, ok := Parse(text)
	if !ok {
		t.Fatal("expected TOOL_CALL to parse")
	}

	if call.Name != "read_file" {
		t.Fatalf("expected read_file, got %q", call.Name)
	}

	if got := call.Args["arg"]; got != "math.pl" {
		t.Fatalf("expected arg math.pl, got %q", got)
	}

	if _, exists := call.Args["path"]; exists {
		t.Fatal("parser must ignore data after END_TOOL_CALL")
	}
}
