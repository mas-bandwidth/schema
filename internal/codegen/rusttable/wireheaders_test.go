package rusttable

import (
	"sort"
	"strings"
	"testing"
)

// THE ARRAY HEADERS, as the wire fuzzer found them (docs/SPEC-TABLES.md
// §2.4, §2.8, §3; make tables-rust-release). Each
// rule is one the oracle (internal/tablewire) keeps and the generated Rust
// reader did not; the fuzz vectors in testdata/wire/tables/fuzz-vectors hold
// the behaviour against the oracle, and this test is the cheap, toolchain-free
// witness that the emitter still spells each rule. Every rule is checked
// twice: the generated source must carry it, and the REVERSED WITNESS puts the
// pre-fix spelling back and requires the same check to fail.

const wireHeaderProbe = `package probe

enum Slot { Left, Right }

union Hit
{
    spans  [2]int32
    damage int32
}

fixed table Keyed
{
    slots [Slot]int32
    hit   Hit
}

table Mapped
{
    seats map[int32]int32
    hit   Hit
}
`

func TestWireArrayHeadersFollowTheOracleRust(t *testing.T) {
	t.Parallel()
	files := generate(t, wireHeaderProbe)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	src := all.String()

	// a keyed array's and a map's header reads against the parent: no guard
	// that calls a count past the body's own L damage before the element kind
	// is judged
	keyedGone := "Some(_) if *off > sub => { report.malformed = true; }"
	keyed := rustFn(src, "keyed_load_body")
	mapped := rustFn(src, "mapped_load_body")
	// an arm's array header reads inside the arm's L
	armWant := "match leb_read(wire, off).filter(|_| *off <= aend) {"

	type rule struct {
		name       string
		text       string
		want, gone string
		revert     func(string) string
	}
	rules := []rule{
		{"keyed header against the parent", keyed, "match leb_read(wire, off) {", keyedGone,
			func(s string) string {
				return strings.Replace(s, "None => { report.malformed = true; }", "None => { report.malformed = true; }\n"+keyedGone, 1)
			}},
		{"map header against the parent", mapped, "match leb_read(wire, off) {", keyedGone,
			func(s string) string {
				return strings.Replace(s, "None => { report.malformed = true; }", "None => { report.malformed = true; }\n"+keyedGone, 1)
			}},
		{"arm array header inside the arm's L (fixed root)", keyed, armWant, "",
			func(s string) string { return strings.ReplaceAll(s, armWant, "match leb_read(wire, off) {") }},
		{"arm array header inside the arm's L (variable root)", mapped, armWant, "",
			func(s string) string { return strings.ReplaceAll(s, armWant, "match leb_read(wire, off) {") }},
	}
	holds := func(r rule, text string) bool {
		return strings.Contains(text, r.want) && (r.gone == "" || !strings.Contains(text, r.gone))
	}
	for _, r := range rules {
		if r.text == "" {
			t.Fatalf("%s: the generated function is missing", r.name)
		}
		if !holds(r, r.text) {
			t.Errorf("%s: the generated reader does not spell the rule", r.name)
		}
		if holds(r, r.revert(r.text)) {
			t.Errorf("%s: the reversed witness still passes, so the check cannot go red", r.name)
		}
	}
}
