package tools

import "testing"

func TestExecRunsCommand(t *testing.T) {
r := New(t.TempDir())
result := r.Exec("printf EXEC_OK")
if !result.OK {
t.Fatalf("exec failed: %s", result.Error)
}
if result.Output != "EXEC_OK" {
t.Fatalf("unexpected output: %q", result.Output)
}
if result.Exit != 0 {
t.Fatalf("unexpected exit code: %d", result.Exit)
}
if result.Status != "PASS" {
t.Fatalf("unexpected status: %q", result.Status)
}
}
