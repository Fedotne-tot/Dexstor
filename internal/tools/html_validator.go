package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const vnuJarRelativePath = ".tools/vnu/vnu.jar"

func (r *Router) ValidateHTML(path string) Result {
	abs, err := r.safePath(path)
	if err != nil {
		return Result{
			Tool:   "validate_html",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	if strings.ToLower(filepath.Ext(path)) != ".html" {
		return Result{
			Tool:   "validate_html",
			Path:   path,
			Error:  "HTML validation requires a .html file",
			Status: "FAIL",
			Exit:   1,
		}
	}

	jar, err := findVNUJar(r.Root)
	if err != nil {
		return Result{
			Tool:   "validate_html",
			Path:   path,
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	cmd := exec.Command(
		"java",
		"-jar",
		jar,
		abs,
	)
	cmd.Dir = r.Root

	out, err := cmd.CombinedOutput()

	result := Result{
		Tool:   "validate_html",
		Path:   path,
		Output: string(out),
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

	result.OK = true
	result.Exit = 0
	result.Status = "PASS"

	return result
}

func findVNUJar(root string) (string, error) {
	// Preserve the existing target-local installation when present.
	targetJar := filepath.Join(root, vnuJarRelativePath)
	if info, err := os.Stat(targetJar); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("vnu.jar path is a directory: %s", targetJar)
		}
		return targetJar, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot access vnu.jar: %w", err)
	}

	// Normal runtime installation: VNU belongs to the DEXSTER/BRATAN
	// installation next to the executable, not to the target project.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine bratan executable: %w", err)
	}

	runtimeJar := filepath.Join(filepath.Dir(exe), vnuJarRelativePath)
	if info, err := os.Stat(runtimeJar); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("vnu.jar path is a directory: %s", runtimeJar)
		}
		return runtimeJar, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot access vnu.jar: %w", err)
	}

	return "", fmt.Errorf(
		"vnu.jar not found in target or next to bratan: target=%s runtime=%s",
		targetJar,
		runtimeJar,
	)
}
