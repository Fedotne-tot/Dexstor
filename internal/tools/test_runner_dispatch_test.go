package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScratchGoMod creates a minimal module file so the runners can build
// fixtures without requiring network toolchain downloads.
func writeScratchGoMod(t *testing.T, root string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module scratch\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PR-1 scope check (updated for the Perl migration): Router.Test must
// dispatch through the runner seam. The former ".pl" -> perlTestRunner entry
// was removed by the migration: the project no longer depends on a Perl
// interpreter, so .pl paths are now unsupported (FAIL). These tests pin the
// dispatch table and the Result contract (Status/Output/Exit) for the Go
// runners that replaced it.

func TestSelectTestRunnerDispatch(t *testing.T) {
	cases := []struct {
		name string
		abs  string
		want testRunner
		ok   bool
	}{
		{"pl no longer supported", "/root/test.pl", nil, false},
		{"go test file", "/root/math_test.go", goTestRunner{}, true},
		{"standalone go uses compile runner", "/root/main.go", goTestFileRunner{}, true},
		{"node kept", "/root/t.js", scriptCommand{base: "node"}, true},
		{"python kept", "/root/t.py", scriptCommand{base: "python3"}, true},
		{"unsupported", "/root/t.rb", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectTestRunner(tc.abs)
			if ok != tc.ok {
				t.Fatalf("selectTestRunner(%q) ok = %v, want %v", tc.abs, ok, tc.ok)
			}
			if !ok {
				return
			}
			switch got.(type) {
			case goTestRunner, goTestFileRunner, scriptCommand:
				// expected shape; compare concrete value for exactness below
			default:
				t.Fatalf("selectTestRunner(%q) returned unexpected runner %T", tc.abs, got)
			}
			if got != tc.want {
				t.Fatalf("selectTestRunner(%q) = %#v, want %#v", tc.abs, got, tc.want)
			}
		})
	}
}

// Migration replacement for TestPerlTestRunnerStillRunsTestPL: the same
// end-to-end contract (Router.Test on the root test program yields PASS with
// the literal "PASS\n" marker) is now provided by the standalone Go runner
// instead of perl + test.pl.
func TestGoFileRunnerRunsRootTestProgram(t *testing.T) {
	root := t.TempDir()

	writeScratchGoMod(t, root)

	goPath := filepath.Join(root, "test.go")
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"PASS\")\n}\n"
	if err := os.WriteFile(goPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("test.go")

	if !res.OK || res.Status != "PASS" || res.Exit != 0 {
		t.Fatalf("go file runner regressed: OK=%v Status=%q Exit=%d Error=%q Output=%q",
			res.OK, res.Status, res.Exit, res.Error, res.Output)
	}
	if res.Tool != "test" || res.Path != "test.go" {
		t.Fatalf("Result contract changed: Tool=%q Path=%q", res.Tool, res.Path)
	}
	if res.Output != "PASS\n" {
		t.Fatalf("go file output changed: %q", res.Output)
	}
}

// The migrated contract must be enforced in both directions: a standalone Go
// test program that fails (non-zero exit) yields FAIL with diagnostics, just
// as a dying perl script did.
func TestGoFileRunnerReportsFailingProgram(t *testing.T) {
	root := t.TempDir()

	writeScratchGoMod(t, root)

	goPath := filepath.Join(root, "test.go")
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfmt.Println(\"WRONG: 5\")\n\tos.Exit(1)\n}\n"
	if err := os.WriteFile(goPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("test.go")

	if res.OK || res.Status != "FAIL" || res.Exit == 0 {
		t.Fatalf("go file failure contract broken: OK=%v Status=%q Exit=%d", res.OK, res.Status, res.Exit)
	}
	if !strings.Contains(res.Output, "WRONG: 5") {
		t.Fatalf("failing program lost its diagnostics: %q", res.Output)
	}
}

// Perl-migration regression guard: .pl files must never be executed again.
func TestPlFileIsUnsupportedWithoutPerl(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "test.pl"),
		[]byte("print \"PASS\\n\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("test.pl")

	if res.OK || res.Status != "FAIL" {
		t.Fatalf("perl path must be unsupported after migration: %+v", res)
	}
	if !strings.Contains(res.Error, "unsupported test file type") {
		t.Fatalf("expected unsupported-type error, got %q", res.Error)
	}
}

func TestGoTestRunnerRunsPackageTests(t *testing.T) {
	root := t.TempDir()

	mod := "package main\n\nfunc Add(a, b int) int { return a + b }\n"
	testSrc := "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad sum\")\n\t}\n}\n"

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "math_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("math_test.go")

	if !res.OK || res.Status != "PASS" || res.Exit != 0 {
		t.Fatalf("go runner failed: OK=%v Status=%q Exit=%d Error=%q Output=%q",
			res.OK, res.Status, res.Exit, res.Error, res.Output)
	}
	if res.Output == "" {
		t.Fatal("go runner must capture combined output")
	}
}

func TestGoTestRunnerReportsFailureContract(t *testing.T) {
	root := t.TempDir()

	testSrc := "package main\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) {\n\tt.Fatal(\"boom\")\n}\n"

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "math_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("math_test.go")

	if res.OK || res.Status != "FAIL" || res.Exit == 0 {
		t.Fatalf("go failure contract broken: OK=%v Status=%q Exit=%d", res.OK, res.Status, res.Exit)
	}
	if res.Output == "" {
		t.Fatal("failed go test must keep diagnostics in Output")
	}
}
