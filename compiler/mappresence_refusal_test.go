// The OPTIONAL MAP is carried by Go first (docs/SPEC-TABLES.md §2.8): every
// other target refuses `?map[K]V` BY NAME, centrally, in Generate, including
// the targets that carry a plain map, and a unit holding only a plain map
// generates exactly what it did before the construct existed.
package compiler

import (
	"strings"
	"testing"
)

// plainMapSchema holds a plain map beside the shapes the optional map shares
// machinery with: an optional scalar, an optional counted array and a map
// whose VALUE is optional.
const plainMapSchema = "package probe\n\ntable Holder\n{\n    tags map[string(8)]int32\n    maybe ?int32\n    some ?[..4]int32\n    values map[uint32]?int32\n}\n"

// plainMapDigest is the sha256 of the go and cpp outputs of plainMapSchema at
// the base this card started from (sprint/schema-go @ f820a0c1f5, re-pinned on the merge of that tip: measured with the base compiler, which has no ?map, the base moved the go output from 5864563260e2 at 3f4d111f44, this card does not), file
// names sorted, each name then its bytes (outputDigest). The go digest was
// re-pinned at head: the strict timestamp parser now rides in every Go text
// form (docs/SPEC-TABLES.md §2.11, §16.2).
// Then docs/SPEC-TABLES.md 16.2 JSON options (schema-go-legacy-json-options)
// moved the go digest again: they change only the Go text runtime every unit
// carries (<pkg>TableJson.go). Re-pinned on the merge of sprint/schema-go @
// 9c5fc1244.
var plainMapDigest = map[string]string{
	"go":  "4d9e4f4b8d4cc212b0f98f60b873d8535e7b58692654862f264fbdddc1841023",
	"cpp": "89878cf368053213bfe10cbe20a4fe38eae626ce4a6d3af244008f811675365e",
}

func TestMapPresenceRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"optional map", "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n", "optional map on field M.tags"},
		{"optional map behind a table", "package probe\ntable Inner\n{\n    by_id ?map[uint32]int32\n}\ntable Outer\n{\n    inner Inner\n}\n", "optional map on field Inner.by_id"},
		{"optional map of maps", "package probe\ntable M\n{\n    nest ?map[uint8]map[uint8]int32\n}\n", "optional map on field M.nest"},
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
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") {
				t.Errorf("%s: target %s refusal does not name the construct and the target: %s", tc.name, lang, msg)
			}
		}
	}
	u := unitFromSource(t, plainMapSchema)
	for lang, want := range plainMapDigest {
		files, err := c.Generate(u, lang, nil)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if got := outputDigest(files); got != want {
			t.Errorf("%s: a unit holding only a plain map moved its output: digest %s, base %s", lang, got, want)
		}
	}
}
