// The one-shot migration mode (SPEC §7.4 rule 3b): `schema fmt -migrate`
// accepts the RETIRED spellings and re-emits the file in the current
// canonical form — the trailing [ ... ] attribute block becomes a |
// qualification section, the specified default moves before the pipe, a
// qualified declaration's body brace moves to the next line, and the
// [<= N] bound respells as [..N]. Plain fmt refuses retired spellings
// exactly as the compiler does; this mode exists so downstream schemas
// respell mechanically, once.
package format

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

// Migrate rewrites retired spellings in src into the current grammar, then
// formats the result (Format's own safety net — re-parse, fingerprint,
// idempotence — runs over the migrated text).
func Migrate(path string, src []byte) ([]byte, error) {
	raw, errs := scanner.ScanRaw(path, src)
	if len(errs) > 0 {
		return nil, errs[0]
	}

	// assemble physical lines, the render pass's own move
	type mline struct {
		tokens  []scanner.Token
		comment string
	}
	var lines []mline
	cur := mline{}
	flush := func() {
		lines = append(lines, cur)
		cur = mline{}
	}
	for _, t := range raw {
		switch t.Kind {
		case scanner.Newline:
			flush()
		case scanner.EOF:
			if len(cur.tokens) > 0 || cur.comment != "" {
				flush()
			}
		case scanner.Comment:
			cur.comment = strings.TrimRight(t.Text, " \t")
		default:
			cur.tokens = append(cur.tokens, t)
		}
	}

	var out []string
	emit := func(tokens []scanner.Token, comment string) {
		var b strings.Builder
		for i, t := range tokens {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(t.Text)
		}
		if comment != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(comment)
		}
		out = append(out, b.String())
	}

	for _, l := range lines {
		tokens := l.tokens

		// [<= N] -> [..N] (the respelled bound)
		for i := range tokens {
			if tokens[i].Kind == scanner.LessEq && i > 0 && tokens[i-1].Kind == scanner.LBrack {
				tokens[i] = scanner.Token{Kind: scanner.DotDot, Text: "..", Pos: tokens[i].Pos}
			}
		}

		// find a trailing attribute block on this line
		attrAt := -1
		for i, t := range tokens {
			if t.Kind == scanner.LBrack && isAttrOpen(tokens, i) {
				attrAt = i
				break
			}
		}
		if attrAt < 0 {
			emit(tokens, l.comment)
			continue
		}
		// the group must close on this line: a wrapped block can carry
		// interior comments that have no home right of a pipe (SPEC §7.4)
		depth := 0
		closeAt := -1
		for j := attrAt; j < len(tokens); j++ {
			switch tokens[j].Kind {
			case scanner.LBrack:
				depth++
			case scanner.RBrack:
				depth--
				if depth == 0 {
					closeAt = j
				}
			}
			if closeAt >= 0 {
				break
			}
		}
		if closeAt < 0 {
			return nil, fmt.Errorf("%s: the attribute block wraps across lines — unwrap the attribute block first, then migrate (SPEC §7.4)", tokens[attrAt].Pos)
		}

		head := tokens[:attrAt]
		inner := tokens[attrAt+1 : closeAt]
		rest := tokens[closeAt+1:]

		pipe := scanner.Token{Kind: scanner.Pipe, Text: "|"}
		switch {
		case len(rest) == 0:
			// a field line: definition | qualifiers
			line := append(append([]scanner.Token{}, head...), pipe)
			emit(append(line, inner...), l.comment)
		case rest[0].Kind == scanner.Assign:
			// the old default-after-attributes order: the default moves
			// BEFORE the pipe — it defines the fresh value (SPEC §4.2)
			line := append(append([]scanner.Token{}, head...), rest...)
			line = append(line, pipe)
			emit(append(line, inner...), l.comment)
		default:
			// a declaration line: the section claims the line, and the body
			// (brace or variant list) opens on the next line (SPEC §4.2)
			line := append(append([]scanner.Token{}, head...), pipe)
			emit(append(line, inner...), l.comment)
			emit(rest, "")
		}
	}

	migrated := []byte(strings.Join(out, "\n") + "\n")
	return Format(path, migrated)
}
