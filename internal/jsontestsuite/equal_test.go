package jsontestsuite

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// outcome is what one Equal call must say under docs/SPEC-TABLES.md §16.13.
type outcome int

const (
	outEqual outcome = iota
	outDiffer
	outRefuse
)

// TestJsonSemanticEqual pins the semantic-equal oracle over the corpus the
// policy accepts and over the §16.13 EQUAL rules the corpus does not spell:
// every accepted file equals itself, a whitespace-only rewrite, and a rewrite
// that respells one ASCII character as its \u escape; numbers compare by token
// text (§16.13); the dialect's trailing commas and comments are not data; a
// lone surrogate, malformed number, key past 255 decoded bytes, number token
// past 511 bytes, bad UTF-8 and past-cap depth are refused, never compared;
// and the reversed witness mutates a number's digit, a string's character and
// an object's first two keys and requires inequality with a named place.
func TestJsonSemanticEqual(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		a, b   string
		want   outcome
		reason string
	}{
		{name: "identity", a: `[1]`, b: `[1]`, want: outEqual},
		{name: "number 1 vs 1.0", a: `[1]`, b: `[1.0]`, want: outDiffer, reason: "number"},
		{name: "number -0 vs 0", a: `[-0]`, b: `[0]`, want: outDiffer, reason: "number"},
		{name: "number 1E2 vs 1e2", a: `[1E2]`, b: `[1e2]`, want: outDiffer, reason: "number"},
		{name: "number 100 vs 1e2", a: `[100]`, b: `[1e2]`, want: outDiffer, reason: "number"},
		{name: "number 1.0 vs 1e0", a: `[1.0]`, b: `[1e0]`, want: outDiffer, reason: "number"},
		{name: "number 10e-1 vs 1", a: `[10e-1]`, b: `[1]`, want: outDiffer, reason: "number"},
		{name: "huge exponent differs", a: `[1e999999999]`, b: `[1e999999998]`, want: outDiffer, reason: "number"},
		{name: "same huge exponent", a: `[1e999999999]`, b: `[1e999999999]`, want: outEqual},
		{name: "array trailing comma", a: `[1,]`, b: `[1]`, want: outEqual},
		{name: "empty string trailing comma", a: `["",]`, b: `[""]`, want: outEqual},
		{name: "object trailing comma", a: `{"id":0,}`, b: `{"id":0}`, want: outEqual},
		{name: "block comment between key and value", a: `{"a":/*comment*/"b"}`, b: `{"a":"b"}`, want: outEqual},
		{name: "line comment to end", a: `{"a":"b"}//`, b: `{"a":"b"}`, want: outEqual},
		{name: "block comment to end", a: `{"a":"b"}/**/`, b: `{"a":"b"}`, want: outEqual},
		{name: "slash escape equals slash", a: `["\/"]`, b: `["/"]`, want: outEqual},
		{name: "ascii escape equals literal", a: `["A"]`, b: `["\u0041"]`, want: outEqual},
		{name: "surrogate pair equals literal", a: `["\uD83D\uDE00"]`, b: `["😀"]`, want: outEqual},
		{name: "key order is data", a: `{"a":1,"b":2}`, b: `{"b":2,"a":1}`, want: outDiffer, reason: "key"},
		{name: "repeated keys are positional", a: `{"a":1,"a":2}`, b: `{"a":2,"a":1}`, want: outDiffer, reason: "number"},
		{name: "string ab vs ac", a: `["ab"]`, b: `["ac"]`, want: outDiffer, reason: "string"},
		{name: "array length differs", a: `[1]`, b: `[1,2]`, want: outDiffer, reason: "array length"},
		{name: "object member count differs", a: `{}`, b: `{"a":1}`, want: outDiffer, reason: "member count"},
		{name: "malformed leading zero", a: `[01]`, b: `[01]`, want: outRefuse, reason: "expected"},
		{name: "unequal but malformed number in b", a: `[1]`, b: `[01]`, want: outRefuse, reason: "refused:"},
		{name: "unequal but malformed surrogate in a", a: `[1,"\uD800"]`, b: `[2,"x"]`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "unequal malformed array comma", a: `[1,,]`, b: `[1]`, want: outRefuse, reason: "refused:"},
		{name: "unequal malformed empty array comma", a: `[,]`, b: `[]`, want: outRefuse, reason: "refused:"},
		{name: "unequal malformed literal tail", a: `[true]`, b: `[truex]`, want: outRefuse, reason: "expected"},
		{name: "both inputs malformed after early diff", a: `[1,01]`, b: `[2,01]`, want: outRefuse, reason: "in a"},
		{name: "kind diff with trailing malformed in b", a: `[1,2]`, b: `["x",01]`, want: outRefuse, reason: "in b"},
		{name: "array length diff with trailing malformed in b", a: `[1]`, b: `[1,2,01]`, want: outRefuse, reason: "in b"},
		{name: "object member count diff with trailing malformed in b", a: `{"a":1}`, b: `{"a":1,"b":01}`, want: outRefuse, reason: "in b"},
		{name: "object key diff with trailing malformed in b", a: `{"a":1,"c":2}`, b: `{"b":1,"c":01}`, want: outRefuse, reason: "in b"},
		{name: "string diff with trailing malformed in b", a: `["a",2]`, b: `["b",01]`, want: outRefuse, reason: "in b"},
		{name: "literal diff with trailing malformed in b", a: `[true,2]`, b: `[false,01]`, want: outRefuse, reason: "in b"},
		{name: "array missing comma in b", a: `[1,2]`, b: `[1 2]`, want: outRefuse, reason: "in b"},
		{name: "array missing comma in a", a: `[1 2]`, b: `[1,2]`, want: outRefuse, reason: "in a"},
		{name: "object missing comma in b", a: `{"a":1,"b":2}`, b: `{"a":1 "b":2}`, want: outRefuse, reason: "in b"},
		{name: "object missing comma in a", a: `{"a":1 "b":2}`, b: `{"a":1,"b":2}`, want: outRefuse, reason: "in a"},
		{name: "lone high surrogate", a: `["\uD800"]`, b: `["\uD800"]`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "lone low surrogate", a: `["\uDFFF"]`, b: `["\uDFFF"]`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "high vs low surrogate", a: `["\uD800"]`, b: `["\uDFFF"]`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "lone surrogate vs replacement", a: `["\uD800"]`, b: `["�"]`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "lone surrogate in key", a: `{"\uD800":1}`, b: `{"\uD800":1}`, want: outRefuse, reason: "string_lone_surrogate"},
		{name: "invalid utf-8", a: "[\"\xff\"]", b: "[\"\xff\"]", want: outRefuse, reason: "string_not_utf8"},
		{name: "unclosed object", a: `{"a":1`, b: `{"a":1}`, want: outRefuse, reason: "ends inside"},
		{name: "key of 255 bytes", a: keyText(255), b: keyText(255), want: outEqual},
		{name: "key of 256 bytes", a: keyText(256), b: keyText(256), want: outRefuse, reason: "key past the cap of 255"},
		{name: "number token of 511 bytes", a: numberText(511), b: numberText(511), want: outEqual},
		{name: "number token of 512 bytes", a: numberText(512), b: numberText(512), want: outRefuse, reason: "number token past the cap of 511"},
		{name: "129 nested arrays", a: nestedArrays(129), b: nestedArrays(129), want: outEqual},
		{name: "130 nested arrays", a: nestedArrays(130), b: nestedArrays(130), want: outRefuse, reason: "depth past the cap of 128"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, why := Equal([]byte(tc.a), []byte(tc.b))
			switch tc.want {
			case outEqual:
				if !ok {
					t.Errorf("Equal(%q, %q) = false: %s", tc.a, tc.b, why)
				}
				if why != "" {
					t.Errorf("Equal(%q, %q) equal but reason %q", tc.a, tc.b, why)
				}
			case outDiffer:
				if ok {
					t.Errorf("Equal(%q, %q) = true; want different", tc.a, tc.b)
				} else if strings.HasPrefix(why, refusedPrefix) {
					t.Errorf("Equal(%q, %q) refused; want a difference: %s", tc.a, tc.b, why)
				} else if !strings.Contains(why, tc.reason) {
					t.Errorf("Equal(%q, %q) reason %q does not name %q", tc.a, tc.b, why, tc.reason)
				}
			case outRefuse:
				if ok {
					t.Errorf("Equal(%q, %q) = true; want a refusal", tc.a, tc.b)
				} else if !strings.HasPrefix(why, refusedPrefix) {
					t.Errorf("Equal(%q, %q) = false, %q; want a refusal", tc.a, tc.b, why)
				} else if !strings.Contains(why, tc.reason) {
					t.Errorf("Equal(%q, %q) refusal %q does not name %q", tc.a, tc.b, why, tc.reason)
				}
			}
		})
	}

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
					t.Error("digit-mutated number is still equal")
				} else if why == "" || strings.HasPrefix(why, refusedPrefix) {
					t.Errorf("digit-mutated number: want a named difference, got %q", why)
				}
			}
			if m, ok := mutateStringChar(data); ok {
				if eq, why := Equal(data, m); eq {
					t.Error("character-mutated string is still equal")
				} else if why == "" || strings.HasPrefix(why, refusedPrefix) {
					t.Errorf("character-mutated string: want a named difference, got %q", why)
				}
			}
			if m := swapFirstTwoKeys(data); m != nil {
				if bytes.Equal(m, data) {
					// Both keys are the same text, so the swap changes nothing.
				} else if eq, why := Equal(data, m); eq {
					t.Error("first-two-keys-swapped object is still equal")
				} else if why == "" || strings.HasPrefix(why, refusedPrefix) {
					t.Errorf("first-two-keys-swapped object: want a named difference, got %q", why)
				}
			}
		})
	}
	if !didAny {
		t.Fatal("the policy accepts no files; nothing to compare")
	}
}

// TestJsonSemanticEqualAllocs pins the huge-exponent compare bounded in
// allocation. testing.AllocsPerRun is sampled before t.Parallel() releases the
// parallel barrier (AllocsPerRun panics once parallel tests are active).
func TestJsonSemanticEqualAllocs(t *testing.T) {
	alloc := testing.AllocsPerRun(10, func() {
		if ok, _ := Equal([]byte("[1e999999999]"), []byte("[1e999999998]")); ok {
			t.Error("1e999999999 equals 1e999999998")
		}
	})
	t.Parallel()
	if ok, _ := Equal([]byte("[1e999999999]"), []byte("[1e999999998]")); ok {
		t.Error("1e999999999 equals 1e999999998")
	}
	if ok, why := Equal([]byte("[1e999999999]"), []byte("[1e999999999]")); !ok {
		t.Errorf("1e999999999 not equal to itself: %s", why)
	}
	if alloc > 128 {
		t.Errorf("huge-exponent compare allocated %v times; want bounded", alloc)
	}
}

func keyText(n int) string {
	return `{"` + strings.Repeat("a", n) + `":1}`
}

func numberText(n int) string {
	return `[1` + strings.Repeat("0", n-1) + `]`
}

func nestedArrays(n int) string {
	return strings.Repeat("[", n) + strings.Repeat("]", n)
}

// addWhitespace wraps a text in whitespace, a rewrite that changes only
// whitespace and never the value (§16.13). The leading byte-order mark, when
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
// has at least two; key order is part of the value (§16.13), so the value
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

// scanTokens splits a JSON text into tokens for the mutation helpers above. It
// skips whitespace and the §16.2 dialect's comments so a word in a comment is
// never mistaken for a token of the value.
func scanTokens(data []byte) []token {
	var out []token
	i, n := 0, len(data)
	for i < n {
		c := data[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < n && data[i+1] == '/':
			i += 2
			for i < n && data[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && data[i+1] == '*':
			i += 2
			for i+1 < n && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			}
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
			for i < n && !tokenDelimiter(data, i) {
				i++
			}
			out = append(out, token{tkNumber, 0, s, i})
		}
	}
	return out
}

func tokenDelimiter(data []byte, i int) bool {
	c := data[i]
	if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
		return true
	}
	switch c {
	case ',', ']', '}', ':', '{', '[':
		return true
	case '/':
		return i+1 < len(data) && (data[i+1] == '/' || data[i+1] == '*')
	}
	return false
}
