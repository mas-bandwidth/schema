package ctable

// The negative controls in this package plant a bug back into the generated C
// by finding an exact piece of it and replacing that piece. The generated C is
// laid out by cfmt (internal/codegen/cfmt: one statement per line, indented by
// depth, a space inside each parenthesis), so a target spelled as the emitter
// writes it no longer appears verbatim in what Generate returns, and a control
// keyed on the raw spelling planted nothing and went red for the wrong reason.
//
// cCodePattern keeps the target EXACT in its tokens and free only in the
// whitespace between them, the one thing cfmt may move. cPlantOnce then holds
// the target to exactly one match across every emitted file, so a reshaped
// generator fails the control loudly ("target not found") and an anchor that
// grows ambiguous fails it too ("N matches"), instead of planting the
// wrong copy or none.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// cCodeTokens splits a fragment of C into its tokens: identifiers and numbers,
// comments and string or character literals whole, and punctuators longest
// first. Whitespace separates tokens and is dropped.
func cCodeTokens(code string) []string {
	var toks []string
	puncts := []string{"<<=", ">>=", "...", "->", "++", "--", "<<", ">>", "<=", ">=", "==", "!=", "&&", "||", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "##"}
	word := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(code[i:], "/*"):
			end := strings.Index(code[i+2:], "*/")
			if end < 0 {
				toks = append(toks, code[i:])
				i = len(code)
				continue
			}
			toks = append(toks, code[i:i+2+end+2])
			i += 2 + end + 2
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(code) && code[j] != c {
				if code[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(code) {
				j++
			}
			toks = append(toks, code[i:j])
			i = j
		case word(c):
			j := i
			for j < len(code) && word(code[j]) {
				j++
			}
			toks = append(toks, code[i:j])
			i = j
		default:
			n := 1
			for _, p := range puncts {
				if strings.HasPrefix(code[i:], p) {
					n = len(p)
					break
				}
			}
			toks = append(toks, code[i:i+n])
			i += n
		}
	}
	return toks
}

// cCodePattern is the regexp source matching code's exact token stream under
// any whitespace between the tokens: optional between two tokens cfmt may
// close up, required between two words (where none would join them into one).
func cCodePattern(code string) string {
	toks := cCodeTokens(code)
	var b strings.Builder
	isWord := func(s string) bool {
		c := s[0]
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	for i, tok := range toks {
		if i > 0 {
			if isWord(toks[i-1]) && isWord(tok) {
				b.WriteString(`\s+`)
			} else {
				b.WriteString(`\s*`)
			}
		}
		b.WriteString(regexp.QuoteMeta(tok))
	}
	return b.String()
}

// cPlantOnce replaces code with replacement in the generated files, where code
// must match EXACTLY ONCE across all of them under cCodePattern; any other
// count is an error that names the target, and nothing is planted. The
// replacement is literal C (the compiler does not mind its layout).
func cPlantOnce(files map[string][]byte, code, replacement string) error {
	re := regexp.MustCompile(cCodePattern(code))
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	count, at := 0, ""
	for _, name := range names {
		if n := len(re.FindAllIndex(files[name], -1)); n > 0 {
			count += n
			at = name
		}
	}
	if count != 1 {
		return fmt.Errorf("sabotage target not found exactly once (%d matches; the control would plant nothing or the wrong copy): %q", count, code)
	}
	files[at] = re.ReplaceAllLiteral(files[at], []byte(replacement))
	return nil
}

// TestCPlantOnceCountsTheTarget holds the planting rule the controls rely on:
// a target laid out differently by cfmt still plants, and a target absent or
// repeated plants nothing and says so. A unit test: no compiler, no clock.
func TestCPlantOnceCountsTheTarget(t *testing.T) {
	const laidOut = "static int f( int x )\n{\n    if ( x == 0 ) { return 1; }\n    return x;\n}\n"
	files := map[string][]byte{"A.h": []byte(laidOut), "B.c": []byte("int g;\n")}
	if err := cPlantOnce(files, "if(x==0){return 1;}", "if(0){return 1;}"); err != nil {
		t.Fatalf("a compact target did not match its laid-out code once: %v", err)
	}
	if !strings.Contains(string(files["A.h"]), "if(0){return 1;}") || strings.Contains(string(files["A.h"]), "x == 0") {
		t.Fatalf("the plant did not replace the target:\n%s", files["A.h"])
	}
	if string(files["B.c"]) != "int g;\n" {
		t.Fatalf("the plant touched a file without the target: %q", files["B.c"])
	}

	absent := map[string][]byte{"A.h": []byte(laidOut)}
	if err := cPlantOnce(absent, "if(x==1){return 1;}", "if(0){return 1;}"); err == nil || !strings.Contains(err.Error(), "0 matches") {
		t.Fatalf("an absent target was not refused: %v", err)
	}
	if string(absent["A.h"]) != laidOut {
		t.Fatal("a refused plant changed the file")
	}

	twice := map[string][]byte{"A.h": []byte(laidOut), "B.h": []byte(laidOut)}
	if err := cPlantOnce(twice, "if(x==0){return 1;}", "if(0){return 1;}"); err == nil || !strings.Contains(err.Error(), "2 matches") {
		t.Fatalf("a repeated target was not refused: %v", err)
	}
	if string(twice["A.h"]) != laidOut || string(twice["B.h"]) != laidOut {
		t.Fatal("a refused plant changed a file")
	}

	// two words stay two words: "return x" must not match "returnx"
	if regexp.MustCompile(cCodePattern("return x;")).MatchString("returnx;") {
		t.Fatal("the pattern joined two words")
	}
}
