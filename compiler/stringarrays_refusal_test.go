// Text lists and the array root are carried by Go first (docs/SPEC-TABLES.md
// §2.13, §16.10): every other target refuses each construct BY NAME, centrally,
// in Generate, and a unit that uses neither generates exactly what it did
// before the constructs existed.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

// neitherSchema uses neither construct and touches the shapes they share
// machinery with: a string field, a bounded and an unbounded array of tables,
// a map, and a json key on a field.
const neitherSchema = "package probe\n\ntable Word\n{\n    s string(8)\n}\n\ntable Holder\n{\n    name string(16) | json = \"label\"\n    words [..4]Word\n    more []Word\n    index map[string(8)]int32\n}\n"

// neitherDigest is the sha256 of the go and cpp outputs of neitherSchema at
// the base this card started from (sprint/schema-go @ f001d67e2086), file
// names sorted, each name then its bytes. The go digest is recomputed on the
// merge of sprint/schema-go @ 3f4d111f4442: it moves from that base only by
// the integrity_mismatch refusal name every Go unit's runtime carries
// (docs/SPEC-TABLES.md §2.14): the enum value, its Error case and its Err line.
var neitherDigest = map[string]string{
	"go":  "664c3c5adbde609d7a9906548259352053d802f121397a068628a55988acc588",
	"cpp": "5aabda2cf12ca342bd480b18084def13f06d87a85b6bdddeea12f955f761da1d",
}

func outputDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(files[name])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestStringArraysRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"bounded text list", "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n", "text list on field Lists.rules"},
		{"optional text list", "package probe\ntable Lists\n{\n    first [..4]?string(8)\n}\n", "text list on field Lists.first"},
		{"unbounded text list", "package probe\ntable Lists\n{\n    repos []string(8)\n}\n", "text list on field Lists.repos"},
		{"unbounded optional text list", "package probe\ntable Lists\n{\n    bases []?string(8)\n}\n", "text list on field Lists.bases"},
		{"array root", "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals []Goal\n}\n", "json = \"array\" on table GoalList"},
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
	u := unitFromSource(t, neitherSchema)
	for lang, want := range neitherDigest {
		files, err := c.Generate(u, lang, nil)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if got := outputDigest(files); got != want {
			t.Errorf("%s: a unit using neither construct moved its output: digest %s, base %s", lang, got, want)
		}
	}
}
