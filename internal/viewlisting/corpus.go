package viewlisting

import "strings"

// CorpusEntries reads the VIEW_CORPUS assignment out of the Makefile,
// following its backslash continuations.
func CorpusEntries(makefile string) []string {
	var entries []string
	inside := false
	for line := range strings.SplitSeq(makefile, "\n") {
		if !inside {
			rest, ok := strings.CutPrefix(line, "VIEW_CORPUS :=")
			if !ok {
				continue
			}
			inside, line = true, rest
		}
		trimmed, more := strings.CutSuffix(strings.TrimRight(line, " \t"), "\\")
		entries = append(entries, strings.Fields(trimmed)...)
		if !more {
			break
		}
	}
	return entries
}
