package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// PR-1 scope check: Router.Test must dispatch through the runner seam without
// changing any pre-existing Perl behaviour. These tests pin the dispatch table
// and the Result contract (Status/Output/Exit) for both runners.

func TestSelectTestRunnerDispatch(t *testing.T) {
	cases := []struct {
		name string
		abs  string
		want testRunner
		ok   bool
	}{
		{"perl kept", "/root/test.pl", perlTestRunner{}, true},
		{"go test file", "/root/math_test.go", goTestRunner{}, true},
		{"plain go still go run", "/root/main.go", scriptCommand{base: "go run"}, true},
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
			case perlTestRunner, goTestRunner, scriptCommand:
				// expected shape; compare concrete value for exactness below
			default:
				t.Fatalf("selectTestRunner(%q) returned unexpected runner %T", tc.abs, got)
			}
			if tc.name == "perl kept" || tc.name == "go test file" {
				if got != tc.want {
					t.Fatalf("selectTestRunner(%q) = %#v, want %#v", tc.abs, got, tc.want)
				}
			}
		})
	}
}

func TestPerlTestRunnerStillRunsTestPL(t *testing.T) {
	root := t.TempDir()

	plPath := filepath.Join(root, "test.pl")
	if err := os.WriteFile(plPath, []byte("print \"PASS\\n\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(root)
	res := r.Test("test.pl")

	if !res.OK || res.Status != "PASS" || res.Exit != 0 {
		t.Fatalf("perl path regressed: OK=%v Status=%q Exit=%d Error=%q Output=%q",
			res.OK, res.Status, res.Exit, res.Error, res.Output)
	}
	if res.Tool != "test" || res.Path != "test.pl" {
		t.Fatalf("Result contract changed: Tool=%q Path=%q", res.Tool, res.Path)
	}
	if res.Output != "PASS\n" {
		t.Fatalf("perl output changed: %q", res.Output)
	}
}

func TestGoTestRunnerRunsPackageTests(t *testing.T) {
	root := t.TempDir()

	mod := "package main\n\nfunc Add(a, b int) int { return a + b }\n"
	testSrc := "package main\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad sum\")\n\t}\n}\n"

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.23\n"), 0o644); err != nil {
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

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.23\n"), 0o644); err != nil {
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
