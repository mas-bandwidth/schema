package main

import (
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/languages"
)

// aiHelp is the block `schema help ai` prints: what a project's own AI
// assistant needs before it touches a .schema file, short enough to paste into
// that project's AGENTS.md or CLAUDE.md. The file is the one copy; docs/AI.md
// links to it rather than repeating it. Its `{{targets}}` placeholder is the
// --lang list, filled from internal/languages so the block never carries a
// second, hand-typed copy of the registry.
//
//go:embed help/ai.md
var aiHelp string

// targetsPlaceholder marks where the --lang list goes in a help topic.
const targetsPlaceholder = "{{targets}}"

// helpLineWidth is where the filled --lang list wraps.
const helpLineWidth = 76

// helpTopics are the topics `schema help <topic>` answers.
var helpTopics = map[string]string{
	"ai": aiHelp,
}

// helpTargets is every --lang spelling this build generates, sorted: the
// languages internal/languages lists with no build tag.
func helpTargets() []string {
	var out []string
	for _, l := range languages.All() {
		if l.Tag == "" {
			out = append(out, l.Target)
		}
	}
	sort.Strings(out)
	return out
}

// fillTargets replaces the placeholder with the comma-separated targets,
// wrapped at helpLineWidth from the placeholder's own column.
func fillTargets(text string) string {
	at := strings.Index(text, targetsPlaceholder)
	if at < 0 {
		return text
	}
	col := at - (strings.LastIndex(text[:at], "\n") + 1)
	var b strings.Builder
	targets := helpTargets()
	for i, t := range targets {
		word := t
		if i < len(targets)-1 {
			word += ","
		}
		if i > 0 {
			if col+1+len(word) > helpLineWidth {
				b.WriteString("\n")
				col = 0
			} else {
				b.WriteString(" ")
				col++
			}
		}
		b.WriteString(word)
		col += len(word)
	}
	return text[:at] + b.String() + text[at+len(targetsPlaceholder):]
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
	_, err := io.WriteString(w, fillTargets(text))
	return err
}
