package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bratan-go/internal/dependencygraph"
	"bratan-go/internal/projectindex"
)

func TestPromptIncludesDependencyGraph(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "math.pl"),
		[]byte("sub add { $_[0] + $_[1] }\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "test.pl"),
		[]byte("require 'math.pl';\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	index, err := projectindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}

	agent := New(root, "test-model", 1)
	agent.ProjectIndex = index
	agent.DependencyGraph = dependencygraph.Build(index)

	prompt := agent.prompt("inspect project dependencies")

	if !strings.Contains(prompt, "DEPENDENCY GRAPH:") {
		t.Fatal("prompt does not contain dependency graph section")
	}

	if !strings.Contains(prompt, "test.pl -> math.pl") {
		t.Fatalf(
			"prompt does not contain expected dependency: %q",
			prompt,
		)
	}
}
