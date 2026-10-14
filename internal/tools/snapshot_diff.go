package tools

import (
	"fmt"
	"strings"

	"bratan-go/internal/projectindex"
)

// SnapshotDiff compares the current project state against a project index
// captured before the agent started modifying the project.
func (r *Router) SnapshotDiff(baseline *projectindex.Index) Result {
	if baseline == nil {
		return Result{
			Tool:   "git_diff",
			Error:  "snapshot diff requires a baseline project index",
			Status: "FAIL",
			Exit:   1,
		}
	}

	current, err := projectindex.Build(r.Root)
	if err != nil {
		return Result{
			Tool:   "git_diff",
			Error:  err.Error(),
			Status: "FAIL",
			Exit:   1,
		}
	}

	diff := projectindex.Diff(baseline, current)

	var output strings.Builder

	if len(diff.Added) > 0 {
		fmt.Fprintf(&output, "added:\n")
		for _, path := range diff.Added {
			fmt.Fprintf(&output, "  %s\n", path)
		}
	}

	if len(diff.Modified) > 0 {
		fmt.Fprintf(&output, "modified:\n")
		for _, path := range diff.Modified {
			fmt.Fprintf(&output, "  %s\n", path)
		}
	}

	if len(diff.Deleted) > 0 {
		fmt.Fprintf(&output, "deleted:\n")
		for _, path := range diff.Deleted {
			fmt.Fprintf(&output, "  %s\n", path)
		}
	}

	if !diff.Changed() {
		output.WriteString("no changes detected\n")
	}

	return Result{
		OK:      true,
		Tool:    "git_diff",
		Output:  output.String(),
		Status:  "PASS",
		Exit:    0,
		Changed: diff.Changed(),
	}
}
