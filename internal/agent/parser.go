package agent

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontMatter parses an agent definition file.
// The file format is YAML front matter delimited by "---" lines,
// followed by a Markdown body that serves as the system prompt.
//
// Format:
//
//	---
//	id: weather-bot
//	name: Weather Bot
//	...
//	---
//	Markdown body as system prompt
func ParseFrontMatter(data []byte) (*AgentConfig, error) {
	// 1. Find the first "---" line
	lines := bytes.Split(data, []byte("\n"))
	firstDash := -1
	secondDash := -1

	for i, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte("---")) {
			if firstDash == -1 {
				firstDash = i
			} else if secondDash == -1 {
				secondDash = i
				break
			}
		}
	}

	// 2. Check if both delimiters were found
	if firstDash == -1 || secondDash == -1 {
		return nil, ErrNoFrontMatter
	}

	// 3. Extract YAML between the delimiters
	yamlLines := lines[firstDash+1 : secondDash]
	yamlContent := bytes.Join(yamlLines, []byte("\n"))

	// 4. Parse YAML into AgentConfig
	var config AgentConfig
	if err := yaml.Unmarshal(yamlContent, &config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
	}

	// 5. Extract body after second "---" as SystemPrompt
	if secondDash+1 < len(lines) {
		bodyLines := lines[secondDash+1:]
		config.SystemPrompt = strings.TrimSpace(string(bytes.Join(bodyLines, []byte("\n"))))
	}

	// 6. Validate required fields
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}
