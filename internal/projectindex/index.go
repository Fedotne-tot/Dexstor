package projectindex

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type File struct {
	Path     string
	Type     string
	Language string
	Size     int64
	Hash     string
}

type Symbol struct {
	File string
	Name string
	Kind string
}

type Relation struct {
	File   string
	Target string
	Kind   string
}

type Index struct {
	Root      string
	Files     []File
	Symbols   []Symbol
	Relations []Relation
}

var (
	perlSubRe = regexp.MustCompile(`(?m)^\s*sub\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	perlUseRe = regexp.MustCompile(`(?m)^\s*use\s+([A-Za-z_][A-Za-z0-9_:]*)`)
	perlReqRe = regexp.MustCompile(`(?m)^\s*require\s+["']([^"']+)["']`)

	goFuncRe      = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\b`)
	goTypeRe      = regexp.MustCompile(`(?m)^type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	goImportRe    = regexp.MustCompile(`(?m)^import\s+"([^"]+)"`)
	goImportBlkRe = regexp.MustCompile("(?s)import\\s*\\(([^)]*)\\)")
	goQuotedRe    = regexp.MustCompile(`"([^"]+)"`)
)

func Build(root string) (*Index, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("project root is not a directory: %s", root)
	}

	index := &Index{
		Root:      abs,
		Files:     make([]File, 0),
		Symbols:   make([]Symbol, 0),
		Relations: make([]Relation, 0),
	}

	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".ai" || entry.Name() == "target") {
			return filepath.SkipDir
		}

		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		file := File{
			Path:     rel,
			Type:     fileType(path),
			Language: language(path),
			Size:     int64(len(data)),
			Hash:     fmt.Sprintf("%x", sha256.Sum256(data)),
		}

		index.Files = append(index.Files, file)

		if file.Language == "Perl" {
			parsePerl(index, rel, string(data))
		}

		if strings.EqualFold(filepath.Ext(path), ".go") {
			parseGo(index, abs, rel, string(data))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(index.Files, func(i, j int) bool {
		return index.Files[i].Path < index.Files[j].Path
	})

	sort.Slice(index.Symbols, func(i, j int) bool {
		if index.Symbols[i].File != index.Symbols[j].File {
			return index.Symbols[i].File < index.Symbols[j].File
		}

		if index.Symbols[i].Kind != index.Symbols[j].Kind {
			return index.Symbols[i].Kind < index.Symbols[j].Kind
		}

		return index.Symbols[i].Name < index.Symbols[j].Name
	})

	sort.Slice(index.Relations, func(i, j int) bool {
		if index.Relations[i].File != index.Relations[j].File {
			return index.Relations[i].File < index.Relations[j].File
		}

		if index.Relations[i].Kind != index.Relations[j].Kind {
			return index.Relations[i].Kind < index.Relations[j].Kind
		}

		return index.Relations[i].Target < index.Relations[j].Target
	})

	return index, nil
}

func parsePerl(index *Index, path, content string) {
	for _, match := range perlSubRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			index.Symbols = append(index.Symbols, Symbol{
				File: path,
				Name: match[1],
				Kind: "sub",
			})
		}
	}

	for _, match := range perlUseRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			index.Relations = append(index.Relations, Relation{
				File:   path,
				Target: match[1],
				Kind:   "use",
			})
		}
	}

	for _, match := range perlReqRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			target := filepath.ToSlash(match[1])
			target = strings.TrimPrefix(target, "./")

			index.Relations = append(index.Relations, Relation{
				File:   path,
				Target: target,
				Kind:   "require",
			})
		}
	}
}

func parseGo(index *Index, root, path, content string) {
	for _, match := range goFuncRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			index.Symbols = append(index.Symbols, Symbol{
				File: path,
				Name: match[1],
				Kind: "func",
			})
		}
	}

	for _, match := range goTypeRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			index.Symbols = append(index.Symbols, Symbol{
				File: path,
				Name: match[1],
				Kind: "type",
			})
		}
	}

	imports := make([]string, 0)
	for _, match := range goImportRe.FindAllStringSubmatch(content, -1) {
		if len(match) == 2 {
			imports = append(imports, match[1])
		}
	}
	for _, block := range goImportBlkRe.FindAllStringSubmatch(content, -1) {
		if len(block) == 2 {
			for _, quoted := range goQuotedRe.FindAllStringSubmatch(block[1], -1) {
				if len(quoted) == 2 {
					imports = append(imports, quoted[1])
				}
			}
		}
	}

	for _, imp := range imports {
		candidate := filepath.Join(filepath.Dir(path), imp)
		rel := filepath.ToSlash(candidate)
		if strings.HasPrefix(rel, "../") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue
		}
		index.Relations = append(index.Relations, Relation{
			File:   path,
			Target: rel,
			Kind:   "use",
		})
	}
}

func fileType(path string) string {
	if strings.EqualFold(filepath.Ext(path), "") {
		return "file"
	}

	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

func language(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pl", ".pm":
		return "Perl"
	case ".go":
		return "Go"
	case ".rs":
		return "Rust"
	case ".py":
		return "Python"
	case ".js":
		return "JavaScript"
	case ".ts":
		return "TypeScript"
	case ".json":
		return "JSON"
	case ".yaml", ".yml":
		return "YAML"
	case ".md":
		return "Markdown"
	default:
		return ""
	}
}
