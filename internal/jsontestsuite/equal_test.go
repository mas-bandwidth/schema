package jsontestsuite

import (
	"bytes"
	"fmt"
	"testing"
)

// TestJsonSemanticEqual pins the semantic-equal oracle over the corpus the
// policy accepts (docs/SPEC-TABLES.md §16.12): every accepted file equals
// itself and a whitespace-only rewrite, an ASCII character respelled as its
// \u escape still equals; and the reversed witness mutates a number's digit, a
// string's character and an object's first two keys and requires inequality.
// The huge-exponent compare is pinned bounded-in-allocation.
func TestJsonSemanticEqual(t *testing.T) {
	t.Parallel()
	s, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	didAny := false
	for _, name := range s.Names() {
		data := s.Files[name]
		if !s.Accept[name] {
			continue
		}
		didAny = true
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if ok, why := Equal(data, data); !ok {
				t.Errorf("Equal(file, file): %s", why)
			}
			if ok, why := Equal(data, addWhitespace(data)); !ok {
				t.Errorf("whitespace-only rewrite not equal: %s", why)
			}
			if esc, changed := rewriteEscapeASCII(data); changed {
				if ok, why := Equal(data, esc); !ok {
					t.Errorf("\\u escape rewrite not equal: %s", why)
				}
			}
			if m, ok := mutateDigit(data); ok {
				if eq, why := Equal(data, m); eq {
					t.Errorf("digit-mutated number is still equal")
				} else if why == "" {
					t.Errorf("digit-mutated number: reason is empty")
				}
			}
			if m, ok := mutateStringChar(data); ok {
				if eq, why := Equal(data, m); eq {
					t.Errorf("character-mutated string is still equal")
				} else if why == "" {
					t.Errorf("character-mutated string: reason is empty")
				}
			}
			if m := swapFirstTwoKeys(data); m != nil {
				if bytes.Equal(m, data) {
					// both keys are the same text, so the swap changes nothing
				} else if eq, why := Equal(data, m); eq {
					t.Errorf("first-two-keys-swapped object is still equal")
				} else if why == "" {
					t.Errorf("first-two-keys-swapped object: reason is empty")
				}
			}
		})
	}
	if !didAny {
		t.Fatal("the policy accepts no files; nothing to compare")
	}
}

// TestJsonSemanticEqualAllocs pins the huge-exponent compare bounded in
// allocation. It is the one test in this package that does not open with
// t.Parallel(): testing.AllocsPerRun refuses to run under a parallel test, and
// the bound it measures is the point of the test.
func TestJsonSemanticEqualAllocs(t *testing.T) {
	if ok, _ := Equal([]byte("[1e999999999]"), []byte("[1e999999998]")); ok {
		t.Error("1e999999999 equals 1e999999998")
	}
	if ok, why := Equal([]byte("[1e999999999]"), []byte("[1e999999999]")); !ok {
		t.Errorf("1e999999999 not equal to itself: %s", why)
	}
	if ok, why := Equal([]byte("[123.0]"), []byte("[123]")); !ok {
		t.Errorf("123.0 not equal to 123: %s", why)
	}
	if ok, why := Equal([]byte("[0e0]"), []byte("[-0.0]")); !ok {
		t.Errorf("0e0 not equal to -0.0: %s", why)
	}
	alloc := testing.AllocsPerRun(1, func() {
		if ok, _ := Equal([]byte("[1e999999999]"), []byte("[1e999999998]")); ok {
			t.Error("1e999999999 equals 1e999999998")
		}
	})
	if alloc > 64 {
		t.Errorf("huge-exponent compare allocated %v times; want bounded", alloc)
	}
}

// addWhitespace wraps a text in whitespace, a rewrite that changes only
// whitespace and never the value (§16.12). The leading byte-order mark, when
// present, stays first.
func addWhitespace(data []byte) []byte {
	out := make([]byte, 0, len(data)+2)
	i := 0
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		out = append(out, data[0], data[1], data[2])
		i = 3
	}
	out = append(out, ' ')
	out = append(out, data[i:]...)
	out = append(out, ' ')
	return out
}

// rewriteEscapeASCII respells the first plain ASCII letter-or-digit of a string
// as its \uXXXX escape; the decoded string is unchanged so the value is equal.
func rewriteEscapeASCII(data []byte) ([]byte, bool) {
	for _, tk := range scanTokens(data) {
		if tk.kind != tkString {
			continue
		}
		if j := firstPlainAlnum(data, tk.start+1, tk.end-1); j >= 0 {
			c := data[j]
			esc := []byte(fmt.Sprintf("\\u%04x", c))
			out := make([]byte, 0, len(data)-1+len(esc))
			out = append(out, data[:j]...)
			out = append(out, esc...)
			out = append(out, data[j+1:]...)
			return out, true
		}
	}
	return nil, false
}

// mutateDigit flips the first digit of the first number token.
func mutateDigit(data []byte) ([]byte, bool) {
	for _, tk := range scanTokens(data) {
		if tk.kind != tkNumber {
			continue
		}
		for j := tk.start; j < tk.end; j++ {
			c := data[j]
			if c >= '0' && c <= '9' {
				out := make([]byte, len(data))
				copy(out, data)
				out[j] = '0' + (c-'0'+1)%10
				return out, true
			}
		}
	}
	return nil, false
}

// mutateStringChar changes one plain ASCII character of the first string.
func mutateStringChar(data []byte) ([]byte, bool) {
	for _, tk := range scanTokens(data) {
		if tk.kind != tkString {
			continue
		}
		if j := firstPlainAlnum(data, tk.start+1, tk.end-1); j >= 0 {
			c := data[j]
			want := byte('A')
			if c == 'A' {
				want = 'B'
			}
			out := make([]byte, len(data))
			copy(out, data)
			out[j] = want
			return out, true
		}
	}
	return nil, false
}

// firstPlainAlnum returns the index of the first plain (unescaped) ASCII
// letter-or-digit in data[start:end], skipping whole escape sequences.
func firstPlainAlnum(data []byte, start, end int) int {
	for i := start; i < end; {
		if data[i] == '\\' {
			i++
			if i >= end {
				return -1
			}
			if data[i] == 'u' {
				i += 5
			} else {
				i++
			}
			continue
		}
		if isAlnum(data[i]) {
			return i
		}
		i++
	}
	return -1
}

// swapFirstTwoKeys swaps the first two top-level keys of the first object that
// has at least two; key order is part of the text (§16.12), so the value
// differs. It returns nil when no such object exists.
func swapFirstTwoKeys(data []byte) []byte {
	toks := scanTokens(data)
	for i, tk := range toks {
		if tk.kind != tkPunct || tk.ch != '{' {
			continue
		}
		depth := 0
		var keys [][2]int
		for j := i + 1; j < len(toks); j++ {
			t := toks[j]
			if t.kind == tkPunct {
				switch t.ch {
				case '{', '[':
					depth++
				case '}', ']':
					if depth == 0 && t.ch == '}' {
						goto found
					}
					depth--
				}
				continue
			}
			if depth == 0 && t.kind == tkString && j+1 < len(toks) && toks[j+1].kind == tkPunct && toks[j+1].ch == ':' {
				keys = append(keys, [2]int{t.start, t.end})
				if len(keys) == 2 {
					goto found
				}
			}
		}
	found:
		if len(keys) >= 2 {
			a, b := keys[0], keys[1]
			out := make([]byte, 0, len(data))
			out = append(out, data[:a[0]]...)
			out = append(out, data[b[0]:b[1]]...)
			out = append(out, data[a[1]:b[0]]...)
			out = append(out, data[a[0]:a[1]]...)
			out = append(out, data[b[1]:]...)
			return out
		}
		break
	}
	return nil
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

type tkKind int

const (
	tkPunct tkKind = iota
	tkString
	tkNumber
	tkLit
)

type token struct {
	kind       tkKind
	ch         byte
	start, end int
}

// scanTokens splits a JSON text into tokens, string-aware, for the mutation
// helpers above.
func scanTokens(data []byte) []token {
	var out []token
	i, n := 0, len(data)
	for i < n {
		switch c := data[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '"':
			s := i
			i++
			for i < n {
				if data[i] == '"' {
					i++
					break
				}
				if data[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
			out = append(out, token{tkString, 0, s, i})
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ':' || c == ',':
			out = append(out, token{tkPunct, c, i, i + 1})
			i++
		case c == 't':
			out = append(out, token{tkLit, 0, i, i + 4})
			i += 4
		case c == 'f':
			out = append(out, token{tkLit, 0, i, i + 5})
			i += 5
		case c == 'n':
			out = append(out, token{tkLit, 0, i, i + 4})
			i += 4
		default:
			s := i
			for i < n && data[i] != ' ' && data[i] != '\t' && data[i] != '\n' && data[i] != '\r' && data[i] != ',' && data[i] != ']' && data[i] != '}' {
				i++
			}
			out = append(out, token{tkNumber, 0, s, i})
		}
	}
	return out
}
