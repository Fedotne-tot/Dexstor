package parser

import (
	"regexp"
	"strings"
)

type ToolCall struct {
	Name string
	Args map[string]string
}

var nameRe = regexp.MustCompile(`(?im)^\s*name\s*:\s*(\S+)\s*$`)
var argRe = regexp.MustCompile(`^\s*(path|file|arg|command|expected|timeout)\s*:\s*(.*?)\s*$`)
var toolCallLineRe = regexp.MustCompile(`(?m)^\s*TOOL_CALL\s*$`)

func Parse(text string) (ToolCall, bool) {
	start := strings.Index(text, "TOOL_CALL")
	if start < 0 {
		return ToolCall{}, false
	}

	text = text[start:]

	if end := strings.Index(text, "END_TOOL_CALL"); end >= 0 {
		text = text[:end]
	}

	/*
		A single model response must contain exactly one TOOL_CALL.
		Reject multiple calls instead of allowing the second TOOL_CALL
		to become part of the first write_file content.
	*/
	if len(toolCallLineRe.FindAllStringIndex(text, -1)) != 1 {
		return ToolCall{}, false
	}

	call := ToolCall{
		Args: make(map[string]string),
	}

	if m := nameRe.FindStringSubmatch(text); len(m) == 2 {
		call.Name = strings.TrimSpace(m[1])
	}

	if call.Name == "" {
		return ToolCall{}, false
	}

	lines := strings.Split(text, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if m := argRe.FindStringSubmatch(line); len(m) == 3 {
			key := strings.TrimSpace(m[1])
			value := strings.TrimSpace(m[2])
			call.Args[key] = value
			continue
		}

		// content is special: it may occupy multiple lines.
		if strings.HasPrefix(strings.TrimSpace(line), "content:") {
			prefix := strings.TrimSpace(line)
			value := strings.TrimSpace(
				strings.TrimPrefix(prefix, "content:"),
			)

			var contentLines []string

			if value != "" {
				contentLines = append(contentLines, value)
			}

			for j := i + 1; j < len(lines); j++ {
				contentLines = append(contentLines, lines[j])
			}

			// The newline immediately before END_TOOL_CALL is a
			// protocol separator, not part of the file content.
			if len(contentLines) > 0 && contentLines[len(contentLines)-1] == "" {
				contentLines = contentLines[:len(contentLines)-1]
			}

			call.Args["content"] = strings.Join(contentLines, "\n")
			break
		}
	}

	return call, true
}
