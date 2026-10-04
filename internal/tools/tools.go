package tools

import (
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
}

type Router struct {
	Root string
}

func New(root string) *Router {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}

	return &Router{Root: abs}
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

func (r *Router) Read(path string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{Tool: "read_file", Error: err.Error()}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Tool: "read_file", Error: err.Error()}
	}

	return Result{
		OK:      true,
		Tool:    "read_file",
		Path:    path,
		Content: string(data),
	}
}

func (r *Router) Write(path, content string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{Tool: "write_file", Error: err.Error()}
	}

	/*
		Idempotent write:
		if the target already contains exactly the requested content,
		there is nothing to write. This also makes repeated tool calls
		safe after retries or restored FSM actions.
	*/
	if data, err := os.ReadFile(abs); err == nil {
		if string(data) == content {
			return Result{
				OK:      true,
				Tool:    "write_file",
				Path:    path,
				Content: content,
			}
		}
	} else if !os.IsNotExist(err) {
		return Result{
			Tool:  "write_file",
			Path:  path,
			Error: err.Error(),
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return Result{Tool: "write_file", Error: err.Error()}
	}

	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return Result{Tool: "write_file", Error: err.Error()}
	}

	return Result{
		OK:   true,
		Tool: "write_file",
		Path: path,
	}
}

func (r *Router) Inspect(path string, depth int) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{Tool: "inspect_project", Error: err.Error()}
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
			Tool:  "inspect_project",
			Error: err.Error(),
		}
	}

	return Result{
		OK:   true,
		Tool: "inspect_project",
		Path: path,
		Tree: strings.Join(lines, "\n"),
	}
}

func (r *Router) GitDiff(path string) Result {
	target := path
	if target == "" {
		target = "."
	}

	cmd := exec.Command("git", "-C", r.Root, "diff", "--", target)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return Result{
			Tool:   "git_diff",
			Output: string(out),
			Error:  err.Error(),
			Exit:   cmd.ProcessState.ExitCode(),
		}
	}

	return Result{
		OK:     true,
		Tool:   "git_diff",
		Output: string(out),
		Exit:   0,
	}
}

func (r *Router) Exec(command string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{Tool: "exec", Error: "command is required"}
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = r.Root
	out, err := cmd.CombinedOutput()
	result := Result{Tool: "exec", Output: string(out)}
	if err == nil {
		result.OK = true
		result.Exit = 0
		result.Status = "PASS"
		return result
	}
	result.Exit = cmd.ProcessState.ExitCode()
	result.Error = err.Error()
	result.Status = "FAIL"
	return result
}

func (r *Router) AssertOutput(command, expected string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{Tool: "assert_output", Error: "command is required", Status: "FAIL"}
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
		result.Exit = cmd.ProcessState.ExitCode()
		result.Error = err.Error()
		result.Status = "FAIL"
		return result
	}

	result.Exit = 0

	if actual != expected {
		result.Error = fmt.Sprintf("output mismatch: expected %q, got %q", expected, actual)
		result.Status = "FAIL"
		return result
	}

	result.OK = true
	result.Status = "PASS"
	return result
}

func (r *Router) Test(path string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{Tool: "test", Error: err.Error()}
	}

	cmd := exec.Command("perl", abs)
	cmd.Dir = r.Root

	out, err := cmd.CombinedOutput()

	result := Result{
		Tool:   "test",
		Path:   path,
		Output: string(out),
	}

	if err == nil {
		result.OK = true
		result.Exit = 0
		result.Status = "PASS"
		return result
	}

	result.Exit = cmd.ProcessState.ExitCode()
	result.Error = err.Error()
	result.Status = "FAIL"

	return result
}
