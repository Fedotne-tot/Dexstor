package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (r *Router) ValidateProject() Result {
	htmlFiles, err := findHTMLFiles(r.Root)
	if err != nil {
		return Result{
			Tool:   "validate_project",
			Status: "FAIL",
			Error:  err.Error(),
			Exit:   1,
		}
	}

	if len(htmlFiles) > 0 {
		sort.Strings(htmlFiles)

		for _, file := range htmlFiles {
			result := r.ValidateHTML(file)

			if !result.OK || result.Exit != 0 {
				result.Tool = "validate_project"
				return result
			}
		}

		return Result{
			Tool:   "validate_project",
			Status: "PASS",
			OK:     true,
			Exit:   0,
			Output: fmt.Sprintf(
				"validated html files: %d",
				len(htmlFiles),
			),
		}
	}

	goFiles := []string{}

	err = filepath.Walk(r.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			goFiles = append(goFiles, path)
		}

		return nil
	})

	if err != nil {
		return Result{
			Tool:   "validate_project",
			Status: "FAIL",
			Error:  err.Error(),
			Exit:   1,
		}
	}

	if len(goFiles) > 0 {
		result := r.Exec("go test ./...")

		result.Tool = "validate_project"
		return result
	}

	return Result{
		Tool:   "validate_project",
		Status: "PASS",
		OK:     true,
		Exit:   0,
		Output: "no validator required",
	}
}

func findHTMLFiles(root string) ([]string, error) {
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if info.Name() == ".ai" {
				return filepath.SkipDir
			}
			return nil
		}

		if strings.EqualFold(filepath.Ext(path), ".html") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			files = append(files, rel)
		}

		return nil
	})

	return files, err
}
