package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PR-8 scope check: the Go runner must satisfy the shared ledger contract
// (successful test => Output contains PASS; failed test => FAIL without a
// fabricated PASS marker) without touching the Perl runner or any FSM/ledger
// code. These tests pin both halves of that contract end-to-end through
// Router.Test plus unit coverage of normalizeGoTestOutput itself.

func writeScratchModule(t *testing.T, root string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module scratch\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Ordinary Go test WITHOUT any fmt.Println("PASS") in the source: the runner
// must still produce an Output carrying the PASS marker so the agent can
// record a ledger-valid successful verification.
func TestGoTestRunnerPassOutputContainsPASSMarker(t *testing.T) {
	root := t.TempDir()
	writeScratchModule(t, root)

	mod := "package main\n\nfunc Add(a, b int) int { return a + b }\n"
	// Deliberately no printing at all inside the test body.
	testSrc := "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad sum\")\n\t}\n}\n"

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "math_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("math_test.go")

	if !res.OK || res.Status != "PASS" || res.Exit != 0 {
		t.Fatalf("go pass run broken: OK=%v Status=%q Exit=%d Error=%q Output=%q",
			res.OK, res.Status, res.Exit, res.Error, res.Output)
	}
	if !strings.Contains(res.Output, "PASS") {
		t.Fatalf("ledger contract violated: successful go test output lacks PASS marker: %q", res.Output)
	}
}

// Failing Go test: result must be FAIL and normalization must never inject a
// PASS marker into diagnostics.
func TestGoTestRunnerFailOutputHasNoPASSMarker(t *testing.T) {
	root := t.TempDir()
	writeScratchModule(t, root)

	// Failure message deliberately avoids the letters "PASS" anywhere.
	testSrc := "package main\n\nimport \"testing\"\n\nfunc TestBoom(t *testing.T) {\n\tt.Fatal(\"kaboom\")\n}\n"

	if err := os.WriteFile(filepath.Join(root, "math_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("math_test.go")

	if res.OK || res.Status != "FAIL" || res.Exit == 0 {
		t.Fatalf("go fail contract broken: OK=%v Status=%q Exit=%d", res.OK, res.Status, res.Exit)
	}
	if strings.Contains(res.Output, "PASS") {
		t.Fatalf("failed go test must not gain a PASS marker: %q", res.Output)
	}
	if res.Output == "" {
		t.Fatal("failed go test must keep diagnostics in Output")
	}
}

// Perl path must remain byte-for-byte unchanged by PR-8 normalization.
func TestPerlRunnerOutputUnchangedByNormalization(t *testing.T) {
	root := t.TempDir()

	plPath := filepath.Join(root, "test.pl")
	if err := os.WriteFile(plPath, []byte("print \"PASS\\n\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("test.pl")

	if !res.OK || res.Status != "PASS" || res.Exit != 0 {
		t.Fatalf("perl path regressed: OK=%v Status=%q Exit=%d Error=%q", res.OK, res.Status, res.Exit, res.Error)
	}
	if res.Output != "PASS\n" {
		t.Fatalf("perl output changed by PR-8: got %q, want %q", res.Output, "PASS\n")
	}
}

// Unit coverage for the normalization rule itself, including the guard that
// non-zero exits are never annotated.
func TestNormalizeGoTestOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
		exit   int
		want   string
	}{
		{"success without marker appends PASS", "ok  \tscratch\n", 0, "ok  \tscratch\nPASS\n"},
		{"success with marker untouched", "=== RUN   TestAdd\n--- PASS: TestAdd\nPASS\n", 0, "=== RUN   TestAdd\n--- PASS: TestAdd\nPASS\n"},
		{"failure never annotated", "--- FAIL: TestBoom\n", 1, "--- FAIL: TestBoom\n"},
		{"empty success gets marker", "", 0, "PASS\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeGoTestOutput(tc.output, tc.exit); got != tc.want {
				t.Fatalf("normalizeGoTestOutput(%q, %d) = %q, want %q", tc.output, tc.exit, got, tc.want)
			}
		})
	}
}
