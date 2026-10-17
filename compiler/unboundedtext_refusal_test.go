// Unbounded text (docs/SPEC-TABLES.md §2.13.1) is carried by Go first: every
// other target refuses each construct BY NAME, centrally, in Generate, and a
// unit that uses neither generates exactly what it did before the construct
// existed.
package compiler

import (
	"strings"
	"testing"
)

// boundedTextSchema uses neither construct and touches every shape the two
// share machinery with: bounded text lists of both spellings, a bounded-key
// map, a string field and a *string blob. Go carries all of it; cpp refuses
// the text lists (§2.13), so its unit is the same table without them.
var boundedTextSchema = map[string]string{
	"go":  "package probe\n\ntable Lists\n{\n    rules [..4]string(8)\n    first []?string(8)\n    byname map[string(8)]int32\n    name string(16)\n    note *string\n}\n",
	"cpp": "package probe\n\ntable Lists\n{\n    byname map[string(8)]int32\n    name string(16)\n    note *string\n}\n",
}

// boundedTextDigest is the sha256 of the go and cpp outputs of
// boundedTextSchema at the base this card started from (sprint/schema-go @
// caea41783), file names sorted, each name then its bytes (outputDigest).
// The go digest was re-pinned when TableFieldInfo gained Unbounded
// (docs/SPEC-TABLES.md §8.1), which moves every unit's descriptor and runtime.
var boundedTextDigest = map[string]string{
	"go":  "0523f561ed7f1465c68360786037602a9e07b39810cf39181e94d04f6725d558",
	"cpp": "ff5e30ac14304bb0ab07460f917a19a1efa46fa1c5dac34498d4bd856dfe9605",
}

func TestUnboundedTextRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"[]string", "package probe\ntable Where\n{\n    rules []string\n}\n", "unbounded text-list element on field Where.rules"},
		{"[]?string", "package probe\ntable Where\n{\n    first []?string\n}\n", "unbounded text-list element on field Where.first"},
		{"[..N]string", "package probe\ntable Where\n{\n    recent [..4]string\n}\n", "unbounded text-list element on field Where.recent"},
		{"[..N]?string", "package probe\ntable Where\n{\n    pending [..4]?string\n}\n", "unbounded text-list element on field Where.pending"},
		{"map[string]V", "package probe\ntable Where\n{\n    byname map[string]int32\n}\n", "unbounded map key on field Where.byname"},
		{"map[string]V in a map value", "package probe\ntable Where\n{\n    nested map[uint8]map[string]int32\n}\n", "unbounded map key on field Where.nested.value"},
	}
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		for _, lang := range c.Targets() {
			_, err := c.Generate(u, lang, nil)
			if lang == "go" {
				if err != nil {
					t.Errorf("%s: go refused: %v", tc.name, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s: target %s accepted the construct", tc.name, lang)
				continue
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") || !strings.Contains(msg, "§2.13.1") {
				t.Errorf("%s: target %s refusal does not name the construct, the field and the target: %s", tc.name, lang, msg)
			}
		}
	}
	for lang, want := range boundedTextDigest {
		u := unitFromSource(t, boundedTextSchema[lang])
		files, err := c.Generate(u, lang, nil)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if got := outputDigest(files); got != want {
			t.Errorf("%s: a unit using neither construct moved its output: digest %s, base %s", lang, got, want)
		}
	}
}
