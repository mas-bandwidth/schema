package main

import (
	"fmt"
	"strings"
)

// A GitHub Actions workflow is block YAML, and the questions this package asks
// of one are structural: which job a leg needs, and what its `strategy.matrix`
// is. A substring scan answers neither. It reports that the text appears
// somewhere in the file, so a leg that replaced its matrix expression with a
// hand-typed include list and left the old expression in a comment reads the
// same as a leg that runs the plan.
//
// So the workflow is parsed. This reader covers the subset the workflows in
// this tree are written in: block mappings, block sequences, block scalars
// (`|` and `>`), plain and quoted scalars, and comment lines. It has no
// anchors, no multi-document files and no tag resolution; a flow collection
// (`branches: [ main ]`) stays the scalar text it was written as, because
// nothing here reads one. Every scalar stays a string, quotes and inline
// comments included, for the same reason. A spelling this reader does not
// understand is an error rather than a silent omission.

// yamlLine is one significant line: its indentation, its text with that
// indentation stripped, whether it is a whole-line comment, and where it came
// from.
type yamlLine struct {
	indent  int
	text    string
	comment bool
	number  int
}

// significantLines drops blank lines and measures the indentation of what is
// left. A comment line is kept and marked: it carries no structure, so every
// walk below steps over it, but a comment inside a block scalar is part of the
// script that scalar holds and a reader that dropped it would hand back a
// script the workflow does not run. A blank line carries neither, so dropping
// it costs the reader nothing.
func significantLines(body string) []yamlLine {
	var out []yamlLine
	for i, raw := range strings.Split(body, "\n") {
		text := strings.TrimLeft(raw, " ")
		if text == "" {
			continue
		}
		out = append(out, yamlLine{
			indent:  len(raw) - len(text),
			text:    strings.TrimRight(text, " "),
			comment: strings.HasPrefix(text, "#"),
			number:  i + 1,
		})
	}
	return out
}

// skipComments advances past the comment lines standing at the cursor, so a
// walk that reads structure never has to look at one.
func skipComments(lines []yamlLine, at *int) {
	for *at < len(lines) && lines[*at].comment {
		*at++
	}
}

// parseWorkflow reads a workflow file into nested values: a mapping is a
// map[string]any, a sequence is a []any, and a scalar is a string.
func parseWorkflow(body string) (map[string]any, error) {
	lines := significantLines(body)
	at := 0
	skipComments(lines, &at)
	if at == len(lines) {
		return nil, fmt.Errorf("the workflow holds nothing but comments")
	}
	value, err := parseNode(lines, &at, lines[at].indent)
	if err != nil {
		return nil, err
	}
	skipComments(lines, &at)
	if at != len(lines) {
		return nil, fmt.Errorf("line %d: %q sits at an indentation no block opened", lines[at].number, lines[at].text)
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the workflow's top level is not a mapping")
	}
	return doc, nil
}

// parseNode reads one block at the given indentation: a sequence when the
// first line opens with a dash, a mapping otherwise.
func parseNode(lines []yamlLine, at *int, indent int) (any, error) {
	skipComments(lines, at)
	if *at >= len(lines) {
		return nil, fmt.Errorf("a block was expected and the file ended")
	}
	if isSequenceItem(lines[*at].text) {
		return parseSequence(lines, at, indent)
	}
	return parseMapping(lines, at, indent)
}

func isSequenceItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

// parseMapping reads the entries standing at one indentation. A value on the
// key's own line is a scalar; `|` or `>` opens a block scalar, whose lines are
// every line indented past the key; an empty value opens a nested block, which
// a sequence may share the key's indentation with.
func parseMapping(lines []yamlLine, at *int, indent int) (map[string]any, error) {
	out := map[string]any{}
	for {
		skipComments(lines, at)
		if *at >= len(lines) || lines[*at].indent != indent || isSequenceItem(lines[*at].text) {
			break
		}
		line := lines[*at]
		key, rest, ok := splitMappingKey(line.text)
		if !ok {
			return nil, fmt.Errorf("line %d: %q is neither a mapping entry nor a sequence item", line.number, line.text)
		}
		if _, seen := out[key]; seen {
			return nil, fmt.Errorf("line %d: the key %q appears twice in one mapping", line.number, key)
		}
		*at++
		if strings.HasPrefix(rest, "|") || strings.HasPrefix(rest, ">") {
			// A block scalar's lines are its script, comment lines included.
			var block []string
			for *at < len(lines) && lines[*at].indent > indent {
				block = append(block, lines[*at].text)
				*at++
			}
			out[key] = strings.Join(block, "\n")
			continue
		}
		if rest != "" {
			out[key] = rest
			continue
		}
		peek := *at
		skipComments(lines, &peek)
		switch {
		case peek < len(lines) && lines[peek].indent > indent:
			*at = peek
			child, err := parseNode(lines, at, lines[peek].indent)
			if err != nil {
				return nil, err
			}
			out[key] = child
		case peek < len(lines) && lines[peek].indent == indent && isSequenceItem(lines[peek].text):
			*at = peek
			child, err := parseSequence(lines, at, indent)
			if err != nil {
				return nil, err
			}
			out[key] = child
		default:
			out[key] = ""
		}
	}
	return out, nil
}

// parseSequence reads the items standing at one indentation. An item's first
// line carries its content after the dash, and the item's remaining lines are
// indented two past the dash, so the item is read as a block of its own.
func parseSequence(lines []yamlLine, at *int, indent int) ([]any, error) {
	var out []any
	for {
		skipComments(lines, at)
		if *at >= len(lines) || lines[*at].indent != indent || !isSequenceItem(lines[*at].text) {
			break
		}
		line := lines[*at]
		inner := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		*at++
		if inner == "" {
			peek := *at
			skipComments(lines, &peek)
			if peek >= len(lines) || lines[peek].indent <= indent {
				out = append(out, "")
				continue
			}
			*at = peek
			child, err := parseNode(lines, at, lines[peek].indent)
			if err != nil {
				return nil, err
			}
			out = append(out, child)
			continue
		}
		item := []yamlLine{{indent: indent + 2, text: inner, number: line.number}}
		for *at < len(lines) && lines[*at].indent > indent {
			item = append(item, lines[*at])
			*at++
		}
		if _, _, ok := splitMappingKey(inner); !ok {
			if len(item) > 1 {
				return nil, fmt.Errorf("line %d: a scalar sequence item carries an indented block", line.number)
			}
			out = append(out, inner)
			continue
		}
		innerAt := 0
		child, err := parseNode(item, &innerAt, indent+2)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, nil
}

// splitMappingKey cuts a line at the colon that ends its key: the first colon
// outside quotes that a space or the end of the line follows. A colon inside a
// quoted scalar is part of the value, which is what keeps `run: echo "a: b"`
// one entry.
func splitMappingKey(text string) (key, rest string, ok bool) {
	var quote byte
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ':' && (i+1 == len(text) || text[i+1] == ' '):
			key = strings.TrimSpace(text[:i])
			if key == "" {
				return "", "", false
			}
			return key, strings.TrimSpace(text[i+1:]), true
		}
	}
	return "", "", false
}

// mappingAt walks a path of mapping keys and returns the value at its end,
// naming the step that failed rather than the whole path.
func mappingAt(doc map[string]any, path ...string) (any, error) {
	var current any = doc
	for i, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s is not a mapping", strings.Join(path[:i], "."))
		}
		next, ok := m[key]
		if !ok {
			return nil, fmt.Errorf("%s has no %q", strings.Join(append([]string{"the workflow"}, path[:i]...), "."), key)
		}
		current = next
	}
	return current, nil
}

// stringsOf reads a field that YAML lets a workflow write either as one scalar
// or as a sequence of them, which is how `needs:` is spelled both ways.
func stringsOf(value any) ([]string, error) {
	switch v := value.(type) {
	case string:
		return []string{v}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("a sequence item is not a scalar")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("the value is neither a scalar nor a sequence of them")
}
