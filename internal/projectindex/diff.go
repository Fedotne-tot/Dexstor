package projectindex

import "sort"

// Diff describes changes between two project indexes.
type DiffResult struct {
	Added    []string
	Modified []string
	Deleted  []string
}

// Diff compares two project indexes by file path and content hash.
//
// baseline is the index captured before the agent modifies the project.
// current is the index captured after the modifications.
//
// The indexes themselves are never modified.
func Diff(baseline, current *Index) DiffResult {
	result := DiffResult{
		Added:    make([]string, 0),
		Modified: make([]string, 0),
		Deleted:  make([]string, 0),
	}

	if baseline == nil {
		if current != nil {
			for _, file := range current.Files {
				result.Added = append(result.Added, file.Path)
			}
		}

		sort.Strings(result.Added)
		return result
	}

	if current == nil {
		for _, file := range baseline.Files {
			result.Deleted = append(result.Deleted, file.Path)
		}

		sort.Strings(result.Deleted)
		return result
	}

	baseFiles := make(map[string]File, len(baseline.Files))
	currentFiles := make(map[string]File, len(current.Files))

	for _, file := range baseline.Files {
		baseFiles[file.Path] = file
	}

	for _, file := range current.Files {
		currentFiles[file.Path] = file
	}

	for path, currentFile := range currentFiles {
		baseFile, exists := baseFiles[path]
		if !exists {
			result.Added = append(result.Added, path)
			continue
		}

		if baseFile.Hash != currentFile.Hash {
			result.Modified = append(result.Modified, path)
		}
	}

	for path := range baseFiles {
		if _, exists := currentFiles[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}

	sort.Strings(result.Added)
	sort.Strings(result.Modified)
	sort.Strings(result.Deleted)

	return result
}

// Changed reports whether the diff contains any added, modified, or deleted file.
func (d DiffResult) Changed() bool {
	return len(d.Added) > 0 ||
		len(d.Modified) > 0 ||
		len(d.Deleted) > 0
}
