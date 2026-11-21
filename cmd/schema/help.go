package main

import (
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"
)

// aiHelp is the block `schema help ai` prints: what a project's own AI
// assistant needs before it touches a .schema file, short enough to paste into
// that project's AGENTS.md or CLAUDE.md. The file is the one copy; docs/AI.md
// links to it rather than repeating it.
//
//go:embed help/ai.md
var aiHelp string

// helpTopics are the topics `schema help <topic>` answers, each printed to
// stdout exactly as embedded so a project can diff what it pasted.
var helpTopics = map[string]string{
	"ai": aiHelp,
}

// helpTopic writes one topic to w, or refuses an unknown topic by name with
// the topics that exist.
func helpTopic(w io.Writer, topic string) error {
	text, ok := helpTopics[topic]
	if !ok {
		names := make([]string, 0, len(helpTopics))
		for name := range helpTopics {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Errorf("help: no topic %q; the topics are %s", topic, strings.Join(names, ", "))
	}
	_, err := io.WriteString(w, text)
	return err
}
