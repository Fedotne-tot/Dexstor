package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"bratan-go/internal/agent"
)

func main() {
	model := flag.String("model", "", "Ollama model")
	project := flag.String("project", ".", "Project directory")
	max := flag.Int("max-iterations", 15, "Maximum iterations")

	flag.Parse()

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))

	if *model == "" {
		fmt.Fprintln(os.Stderr, "model is required")
		os.Exit(1)
	}

	if task == "" {
		fmt.Fprintln(os.Stderr, "task is required")
		os.Exit(1)
	}

	fmt.Println("[BRATAN GO] starting")
	fmt.Println("[BRATAN GO] project:", *project)
	fmt.Println("[BRATAN GO] model:", *model)

	a := agent.New(*project, *model, *max)

	if err := a.Run(task); err != nil {
		fmt.Fprintln(os.Stderr, "[BRATAN GO] ERROR:", err)
		os.Exit(1)
	}
}
