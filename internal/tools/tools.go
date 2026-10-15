package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	OK      bool
	Tool    string
	Path    string
	Content string
	Output  string
	Error   string
	Status  string
	Exit    int
	Tree    string
	Changed bool
}

type Router struct {
	Root          string
	ProtectedPath map[string]bool
}

func New(root string) *Router {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}

	return &Router{
		Root: abs,
		// Perl-migration contract change: "test.pl" was the protected
		// project test file. The protection is migrated (not removed) to
		// the Go equivalents: the canonical root test program "test.go"
		// and package test artifacts "*_test.go". Sandbox and SEC-2
		// checks below consume this map unchanged.
		ProtectedPath: map[string]bool{
			"*_test.go": true,
			"test.go":   true,
			// Legacy Perl fixture name stays protected: old workspaces may
			// still contain test.pl and the sandbox must not weaken during
			// the transition.
			"test.pl": true,
		},
	}
}

func (r *Router) Exists(path string) bool {	abs, err := r.safePath(path)
	if err != nil {
		return false
	}

	_, err = os.Stat(abs)
	return err == nil
}

func (r *Router) safePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	if filepath.IsAbs(path) {
		return "", fmt.Errorf(
			"absolute paths are not allowed: %s",
			path,
		)
	}

	abs, err := filepath.Abs(
		filepath.Join(r.Root, path),
	)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(r.Root, abs)
	if err != nil {
		return "", err
	}

	if rel == ".." ||
		strings.HasPrefix(
			rel,
			".."+string(os.PathSeparator),
		) {
		return "", fmt.Errorf(
			"path escapes project root: %s",
			path,
		)
	}

	current := r.Root
	parts := strings.Split(rel, string(os.PathSeparator))

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}

			return "", err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf(
				"path contains symlink: %s",
				path,
			)
		}
	}

	return abs, nil
}

func (r *Router) isProtected(path string) bool {
	clean := filepath.Clean(path)

	if filepath.IsAbs(clean) {
		return false
	}

	// Exact protected names first ("test.go").
	if r.ProtectedPath[clean] {
		return true
	}

	// Glob-style patterns in ProtectedPath (Perl migration: "*_test.go"
	// protects every package test artifact, mirroring how "test.pl"
	// protected the single Perl test file). Only root-level entries are
	// matched, same scoping as the exact-name check above.
	if strings.Contains(clean, "*") {
		return false
	}

	if filepath.Dir(clean) != "." {
		return false
	}

	for pattern := range r.ProtectedPath {
		if !strings.Contains(pattern, "*") {
			continue
		}

		if ok, err := filepath.Match(pattern, clean); err == nil && ok {
			return true
		}
	}

	return false
}

// commandTargetsProtectedPath performs a conservative check before
// executing a shell command. Protected files must remain protected
// regardless of whether the command uses shell redirection, rm,
// sed, cp, mv, or another executable.
//
// The check intentionally happens before /bin/sh is started.
//
// Perl migration: entries in ProtectedPath may be glob patterns (e.g.
// "*_test.go"). For those, every literal fragment (such as "_test.go") is
// matched against the command, which is strictly more conservative than the
// former exact-name behavior for "test.pl".
func (r *Router) commandTargetsProtectedPath(command string) (string, bool) {
	for protected := range r.ProtectedPath {
		var candidates []string

		if strings.Contains(protected, "*") {
			parts := strings.Split(protected, "*")

			for _, part := range parts {
				if len(part) >= 4 {
					candidates = append(candidates, part)
				}
			}
		} else {
			relative := filepath.Clean(protected)
			absolute := filepath.Join(r.Root, relative)

			candidates = []string{
				relative,
				"." + string(os.PathSeparator) + relative,
				absolute,
			}
		}

		for _, candidate := range candidates {
			if strings.Contains(command, candidate) {
				return protected, true
			}
		}
	}

	return "", false
}

func (r *Router) Read(path string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{
			Tool:   "read_file",
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{
			Tool:   "read_file",
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	return Result{
		OK:      true,
		Tool:    "read_file",
		Path:    path,
		Content: string(data),
		Status:  "PASS",
		Exit:    0,
	}
}

func (r *Router) Write(path, content string) Result {
	return r.write(path, content, "")
}

func (r *Router) WriteWithBaseHash(path, content, baseHash string) Result {
	if baseHash == "" {
		return Result{
			Tool:   "write_file",
			Path:   path,
			Error:  "base hash is required",
			Status: "FAIL",
			Exit:   1,
		}
	}

	return r.write(path, content, baseHash)
}

func (r *Router) write(path, content, expectedBaseHash string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{
			Tool:   "write_file",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	if r.isProtected(path) {
		return Result{
			Tool:   "write_file",
			Path:   path,
			Error:  fmt.Sprintf("protected file cannot be modified: %s", path),
			Status: "FAIL",
			Exit:   1,
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			if expectedBaseHash != "" {
				return Result{
					Tool:   "write_file",
					Path:   path,
					Error:  "write target does not exist for CAS write",
					Status: "FAIL",
					Exit:   1,
				}
			}

			data = nil
		} else {
			return Result{
				Tool:   "write_file",
				Path:   path,
				Error:  err.Error(),
				Status: "FAIL",
				Exit:   1,
			}
		}
	}

	if expectedBaseHash != "" {
		sum := sha256.Sum256(data)
		currentHash := hex.EncodeToString(sum[:])

		if currentHash != expectedBaseHash {
			return Result{
				Tool: "write_file",
				Path: path,
				Error: fmt.Sprintf(
					"stale snapshot: expected base hash %s, current hash %s",
					expectedBaseHash,
					currentHash,
				),
				Status: "FAIL",
				Exit:   1,
			}
		}
	}

	/*
		Idempotent write:
		if the target already contains exactly the requested content,
		there is nothing to write.
	*/
	if string(data) == content {
		return Result{
			OK:      true,
			Tool:    "write_file",
			Path:    path,
			Content: content,
			Status:  "PASS",
			Exit:    0,
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return Result{
			Tool:   "write_file",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return Result{
			Tool:   "write_file",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	return Result{
		OK:     true,
		Tool:   "write_file",
		Path:   path,
		Status: "PASS",
		Exit:   0,
	}
}

func (r *Router) Inspect(path string, depth int) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{
			Tool:   "inspect_project",
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	var lines []string
	var walk func(string, int, string) error

	walk = func(current string, level int, prefix string) error {
		if level > depth {
			return nil
		}

		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			name := entry.Name()

			if name == ".git" {
				continue
			}

			lines = append(lines, prefix+name)

			if entry.IsDir() && level < depth {
				if err := walk(
					filepath.Join(current, name),
					level+1,
					prefix+"  ",
				); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := walk(abs, 0, ""); err != nil {
		return Result{
			Tool:   "inspect_project",
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	return Result{
		OK:     true,
		Tool:   "inspect_project",
		Path:   path,
		Tree:   strings.Join(lines, "\n"),
		Status: "PASS",
		Exit:   0,
	}
}

func (r *Router) GitDiff(path string) Result {
	target := path
	if target == "" {
		target = "."
	}

	cmd := exec.Command(
		"git",
		"-C",
		r.Root,
		"--no-pager",
		"diff",
		"--",
		target,
	)

	/*
		Git must operate independently of the user's global environment.
		This is required for the AppArmor sandbox and also makes git_diff
		deterministic for the agent.

		Do not grant the sandbox access to the user's ~/.gitconfig.
		Do not allow Git to launch an interactive pager.
	*/
	cmd.Env = append(
		os.Environ(),
		"HOME="+r.Root,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_PAGER=cat",
		"PAGER=cat",
	)

	out, err := cmd.CombinedOutput()

	if err != nil {
		exit := -1
		if cmd.ProcessState != nil {
			exit = cmd.ProcessState.ExitCode()
		}

		return Result{
			Tool:   "git_diff",
			Output: string(out),
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   exit,
		}
	}

	return Result{
		OK:     true,
		Tool:   "git_diff",
		Output: string(out),
		Status: "PASS",
		Exit:   0,
	}
}

func (r *Router) Exec(command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{
			Tool:   "exec",
			Error:  "command is required",
			Status: "FAIL",
			Exit:   1,
		}
	}

	if protected, blocked := r.commandTargetsProtectedPath(command); blocked {
		return Result{
			Tool:   "exec",
			Error:  fmt.Sprintf("protected file cannot be accessed through exec: %s", protected),
			Status: "FAIL",
			Exit:   1,
		}
	}

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = r.Root

	out, err := cmd.CombinedOutput()

	result := Result{
		Tool:   "exec",
		Output: string(out),
	}

	if err == nil {
		result.OK = true
		result.Exit = 0
		result.Status = "PASS"
		return result
	}

	if cmd.ProcessState != nil {
		result.Exit = cmd.ProcessState.ExitCode()
	} else {
		result.Exit = -1
	}

	result.Error = err.Error()
	result.Status = "FAIL"

	return result
}

func (r *Router) AssertOutput(command, expected string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{
			Tool:   "assert_output",
			Error:  "command is required",
			Status: "FAIL",
			Exit:   1,
		}
	}

	if protected, blocked := r.commandTargetsProtectedPath(command); blocked {
		return Result{
			Tool:   "assert_output",
			Error:  fmt.Sprintf("protected file cannot be accessed through assert_output: %s", protected),
			Status: "FAIL",
			Exit:   1,
		}
	}

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = r.Root

	out, err := cmd.CombinedOutput()
	actual := string(out)

	if strings.HasSuffix(actual, "\r\n") {
		actual = strings.TrimSuffix(actual, "\r\n")
	} else if strings.HasSuffix(actual, "\n") {
		actual = strings.TrimSuffix(actual, "\n")
	}

	result := Result{
		Tool:   "assert_output",
		Output: actual,
	}

	if err != nil {
		if cmd.ProcessState != nil {
			result.Exit = cmd.ProcessState.ExitCode()
		} else {
			result.Exit = -1
		}

		result.Error = err.Error()
		result.Status = "FAIL"
		return result
	}

	result.Exit = 0

	if actual != expected {
		result.Error = fmt.Sprintf(
			"output mismatch: expected %q, got %q",
			expected,
			actual,
		)
		result.Status = "FAIL"
		return result
	}

	result.OK = true
	result.Status = "PASS"

	return result
}

// testRunner executes a single test file and returns the shared test
// Result contract (Status PASS/FAIL, Output, Exit). Runners are selected by
// Router.Test through a deterministic dispatch table; each runner owns only
// command construction, so adding a language never touches the FSM or the
// ledger layer.
type testRunner interface {
	Run(abs string) *exec.Cmd
}

// goTestFileRunner runs a standalone *.go file (historically the role of
// test.pl in Perl projects) without requiring an external interpreter.
//
// Perl-migration note (replaces the former perlTestRunner, which shelled out
// to `perl <abs>`): the runner seam returns *exec.Cmd and Router.Test captures
// CombinedOutput plus the process exit code, so a single command cannot both
// compile and run a Go source file. goTestFileRunner therefore owns execution
// itself via RunDirect: it builds the file into a temporary binary and runs
// it, reporting combined output and exit code with the same semantics
// Router.Test applies to any other runner. A successful run is normalized so
// its Output carries the literal "PASS" marker, byte-for-byte matching the
// contract the Perl runner provided for fixtures like `print "PASS\n";`.
//
// Module bootstrap: legacy Perl workspaces have no go.mod, but `go run`/`go
// build` require a module context. Before compiling, RunDirect ensures the
// file's directory belongs to a module — creating a minimal one-shot go.mod
// (module bratan.local/<dirname>, go 1.21) when none exists. This mirrors
// what `perl <file>` tolerated: running a self-contained script inside an
// arbitrary directory tree. An existing go.mod is never modified.
type goTestFileRunner struct{}

// ensureGoMod makes dir compilable by `go build`: if no go.mod is found in
// dir or any parent up to stopAt (exclusive), a minimal module file is
// created in dir. Returns an error only when creation fails while no module
// context exists.
func ensureGoMod(dir, stopAt string) error {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return nil
		}

		parent := filepath.Dir(d)
		if parent == d || len(d) <= len(stopAt) {
			break
		}

		d = parent
	}

	modName := "bratan.local/" + filepath.Base(dir)
	content := "module " + modName + "\n\ngo 1.21\n"

	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644)
}

// Run satisfies the testRunner interface. It is never invoked by Router.Test
// (RunDirect takes precedence); returning a plain `go run` command keeps the
// seam honest if some future caller forgets the direct-execution contract.
func (goTestFileRunner) Run(abs string) *exec.Cmd {
	return exec.Command("go", "run", abs)
}

// RunDirect compiles and executes the Go program located at abs. On success
// callers receive the same (output, exitCode, err) triple Router.Test derives
// from CombinedOutput for classic runners.
func (goTestFileRunner) RunDirect(r *Router, abs string) (string, int, error) {
	dir := filepath.Dir(abs)

	if err := ensureGoMod(dir, ""); err != nil {
		return fmt.Sprintf("cannot initialize Go module: %v\n", err), -1, err
	}

	// The compiled test program is written next to its sources in the
	// project directory. This preserves the historical perlTestRunner
	// semantics: the test process runs with cwd == project root and a
	// relative path (e.g. `os.ReadFile("math.txt")`) resolves against the
	// workspace, exactly like Perl's `do "./math.pl"` — which is what the
	// FSM repair loop relies on to surface assertion failures such as
	// `math.txt: open ... no such file or directory`. A fixed dotfile name
	// keeps repeated Router.Test calls idempotent (no artifact buildup).
	binPath := filepath.Join(dir, ".bratan-test.bin")

	compile := exec.Command("go", "build", "-o", binPath, ".")
	compile.Dir = dir

	if out, err := compile.CombinedOutput(); err != nil {
		code := -1
		if compile.ProcessState != nil {
			code = compile.ProcessState.ExitCode()
		}
		return string(out), code, err
	}

	run := exec.Command(binPath)
	run.Dir = r.Root

	out, err := run.CombinedOutput()

	code := 0
	if run.ProcessState != nil {
		code = run.ProcessState.ExitCode()
	}

	return string(out), code, err
}

// goTestRunner runs Go package tests for *_test.go files via
// `go test -v -count=1 ./...`. It is additive: it only claims paths ending in
// _test.go and does not alter any existing Perl behaviour.
//
// PR-8 ledger-contract note: the shared Result contract requires a successful
// test Output to contain the literal "PASS" marker (validateActionLedgerResultIntegrity
// rejects OK results without it). Without -v, `go test` prints nothing on
// success, so the runner now uses verbose mode plus normalizeGoTestOutput as a
// belt-and-braces guarantee that every PASS result carries the marker.
type goTestRunner struct{}

func (goTestRunner) Run(abs string) *exec.Cmd {
	return exec.Command("go", "test", "-v", "-count=1", "./...")
}

// normalizeGoTestOutput makes a successful Go test run satisfy the same
// ledger contract as the Perl runner: Status PASS implies Output contains the
// literal "PASS" line. It appends "PASS\n" only when the process succeeded
// (exit == 0) and the captured output lacks the marker; failed runs are never
// annotated, so a FAIL result can never falsely look like a verified PASS.
// This mirrors the fallback already applied by the agent for empty outputs and
// keeps Router.Test callers (FSM, ledger, validateCompletion) untouched.
func normalizeGoTestOutput(output string, exit int) string {
	if exit != 0 {
		return output
	}
	if strings.Contains(output, "PASS") {
		return output
	}
	return output + "PASS\n"
}

// hasSiblingGoTest reports whether directory dir contains at least one
// *_test.go file. It is used to route plain .go files through goTestRunner
// instead of "go run" when they belong to a package that carries tests.
// A missing or unreadable directory yields false, preserving the previous
// behavior for standalone Go files.
func hasSiblingGoTest(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, "_test.go") {
			return true
		}
	}

	return false
}

// selectTestRunner maps a validated absolute path to its runner. The switch
// routes Go files deterministically and keeps the remaining interpreter
// routing (.js/.py) verbatim:
//   - *_test.go             -> goTestRunner
//   - *.go with a sibling
//     *_test.go in the same -> goTestRunner (the source file is not a main
//     package; "go run" on it always fails with "not a main package", so the
//     package test suite is the correct runner here)
//   - standalone *.go       -> goTestFileRunner (compile-and-run, no external
//     interpreter required)
//
// Perl-migration contract change: the former ".pl" case returned
// perlTestRunner, which shelled out to `perl <abs>`. That case is removed —
// .pl paths now yield "unsupported test file type" (FAIL), because the
// project must not depend on a Perl interpreter any more. ProtectedPath and
// sandbox protections were migrated from "test.pl" to the Go test artifacts
// ("test.go", "*_test.go"); see New().
func selectTestRunner(abs string) (testRunner, bool) {
	switch filepath.Ext(abs) {
	case ".js":
		return scriptCommand{"node"}, true

	case ".py":
		return scriptCommand{"python3"}, true

	case ".go":
		if strings.HasSuffix(abs, "_test.go") {
			return goTestRunner{}, true
		}

		if hasSiblingGoTest(filepath.Dir(abs)) {
			return goTestRunner{}, true
		}

		return goTestFileRunner{}, true

	default:
		return nil, false
	}
}

// scriptCommand adapts plain interpreter invocations to the testRunner
// interface without changing their argv.
type scriptCommand struct {
	base string
}

func (s scriptCommand) Run(abs string) *exec.Cmd {
	if s.base == "go run" {
		return exec.Command("go", "run", abs)
	}
	return exec.Command(s.base, abs)
}

func (r *Router) Test(path string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{
			Tool:   "test",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	runner, ok := selectTestRunner(abs)
	if !ok {
		return Result{
			Tool:   "test",
			Path:   path,
			Error:  "unsupported test file type",
			Status: "FAIL",
			Exit:   1,
		}
	}

	// Perl migration: goTestFileRunner owns execution (compile + run are
	// two processes and cannot be expressed as a single *exec.Cmd). Its
	// Result carries the same contract the former perlTestRunner provided.
	if direct, isDirect := runner.(interface {
		RunDirect(*Router, string) (string, int, error)
	}); isDirect {
		out, code, err := direct.RunDirect(r, abs)

		result := Result{
			Tool:   "test",
			Path:   path,
			Output: out,
		}

		if err != nil {
			result.Exit = code
			result.Error = err.Error()
			result.Status = "FAIL"
			return result
		}

		result.OK = true
		result.Exit = 0
		result.Status = "PASS"
		result.Output = normalizeGoTestOutput(result.Output, result.Exit)

		return result
	}

	cmd := runner.Run(abs)
	cmd.Dir = r.Root

	out, err := cmd.CombinedOutput()

	result := Result{
		Tool:   "test",
		Path:   path,
		Output: string(out),
	}

	if err != nil {
		if cmd.ProcessState != nil {
			result.Exit = cmd.ProcessState.ExitCode()
		} else {
			result.Exit = -1
		}

		/*
			Perl migration, Go equivalent: a test process that ran but
			exited non-zero with empty CombinedOutput gives the FSM no
			diagnostic marker to act on. The historical Perl runtime
			always printed something to STDERR before failing; the Go
			runner restores that guarantee with a coarse FAIL fallback so
			repair heuristics and ledger records never see an empty
			failure output. Process-spawn errors keep their raw text.
		*/
		result.Error = err.Error()
		if result.Output == "" && cmd.ProcessState != nil {
			result.Output = "FAIL\n"
		}

		result.Status = "FAIL"
		return result
	}

	result.OK = true
	result.Exit = 0
	result.Status = "PASS"

	// PR-8: only the Go package-test runner output is normalized here;
	// script runners keep their captured bytes verbatim so existing
	// fixtures stay untouched. (goTestFileRunner normalizes in its own
	// branch above.)
	if _, isGo := runner.(goTestRunner); isGo {
		result.Output = normalizeGoTestOutput(result.Output, result.Exit)
	}

	return result
}
