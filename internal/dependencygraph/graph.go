package dependencygraph

import (
	"path/filepath"
	"sort"

	"bratan-go/internal/projectindex"
)

type Graph struct {
	Forward map[string][]string
	Reverse map[string][]string
}

func Build(index *projectindex.Index) *Graph {
	graph := &Graph{
		Forward: make(map[string][]string),
		Reverse: make(map[string][]string),
	}

	if index == nil {
		return graph
	}

	for _, relation := range index.Relations {
		if relation.Kind != "require" && relation.Kind != "use" {
			continue
		}

		target := filepath.ToSlash(relation.Target)
		if target == "" {
			continue
		}

		graph.Forward[relation.File] = appendUnique(
			graph.Forward[relation.File],
			target,
		)

		graph.Reverse[target] = appendUnique(
			graph.Reverse[target],
			relation.File,
		)
	}

	for path := range graph.Forward {
		sort.Strings(graph.Forward[path])
	}

	for path := range graph.Reverse {
		sort.Strings(graph.Reverse[path])
	}

	return graph
}

func (g *Graph) Dependencies(path string) []string {
	if g == nil {
		return nil
	}

	return append([]string(nil), g.Forward[path]...)
}

func (g *Graph) Dependents(path string) []string {
	if g == nil {
		return nil
	}

	return append([]string(nil), g.Reverse[path]...)
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}

	return append(items, value)
}
