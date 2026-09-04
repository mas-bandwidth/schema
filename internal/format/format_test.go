// The SPELLING gate. schemafmt runs over every source the compiler touches,
// before it is processed, so a spelling the formatter does not know is a
// spelling it silently rewrites — the author's file moves away from the page
// and stays moved. These tests hold the two spellings whose junctions are
// tight: the MAP's `map[K]V` (docs/SPEC-TABLES.md §2.8) and the POINTER star
// against the type it targets (§2.1).
package format

import (
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// mapExampleFormatted is §2.8's own example in schemafmt-canonical form: the
// page's declarations, byte for byte, with each trailing comment at the one
// space the formatter puts after a line's code.
const mapExampleFormatted = `package docs

table ShipConfig
{
    name   string(64)
    health int32
}

table Item { count int32 }

table Fleet
{
    ships    map[string(32)]ShipConfig // a lookup by name
    by_id    map[uint32]*ShipConfig // by number; two keys may share one node
    loadouts map[string(16)]map[uint8]Item // a map is a value, so maps nest
}
`

// TestFormatsTheMapExampleOfThePage reads §2.8's example OFF THE PAGE and
// formats it. The comparison is byte for byte, because the defect this gate
// exists for is invisible any other way: `map [string(32)]ShipConfig` parses,
// checks, generates and round-trips through this formatter unchanged, so
// nothing but the bytes says the author's source no longer reads as the page
// spells it.
func TestFormatsTheMapExampleOfThePage(t *testing.T) {
	src := "package docs\n\n" + fencedBlockContaining(t, "../../docs/SPEC-TABLES.md", "table Item { count int32 }", 1)
	out, err := Format("Probe.schema", []byte(src))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if string(out) != mapExampleFormatted {
		t.Errorf("§2.8's example does not format to its own spelling:\n--- got ---\n%s\n--- want ---\n%s", out, mapExampleFormatted)
	}
}

// TestFormatCanonicalizesTightJunctions is the NEGATIVE CONTROL for the test
// above: every input here is a spelling schemafmt itself used to produce, and
// each must come back as the one the page states. A gate that only checked
// its own output could pass with the junction rules removed — these cannot,
// because they start from the wrong side of each rule.
func TestFormatCanonicalizesTightJunctions(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"map takes no space before its bracket", "    m map [uint32]int32\n", "    m map[uint32]int32\n"},
		{"a map's pointer value binds to its type", "    m map [uint32] * Node\n", "    m map[uint32]*Node\n"},
		{"a map's optional array value binds too", "    m map [uint32] ?[..4]int32\n", "    m map[uint32]?[..4]int32\n"},
		{"a nested map binds at both brackets", "    m map [uint8] map [uint8]int32\n", "    m map[uint8]map[uint8]int32\n"},
		{"a bounded array of pointers binds its star", "    p [..4] * Node\n", "    p [..4]*Node\n"},
		{"a fixed array of pointers binds its star", "    p [2] * Node\n", "    p [2]*Node\n"},
		{"a bare pointer field is unchanged", "    p *Node\n", "    p *Node\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package probe\n\ntable Node\n{\n" + tc.in + "}\n"
			want := "package probe\n\ntable Node\n{\n" + tc.want + "}\n"
			out, err := Format("Probe.schema", []byte(src))
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			if string(out) != want {
				t.Errorf("formatted to %q, want %q", out, want)
			}
		})
	}
}

// TestFormatFingerprintSeesAMap holds the SAFETY CHECK honest. Format's
// promise is that it re-parses its own output and refuses on any structural
// difference — and a fingerprint that renders no map would report two
// different maps, or a map and a bare field, as the same structure. The
// control is direct: two declarations that differ only inside the map must
// fingerprint differently.
func TestFormatFingerprintSeesAMap(t *testing.T) {
	prints := map[string]string{}
	for _, src := range []string{
		"package probe\n\ntable T\n{\n    m map[uint32]int32\n}\n",
		"package probe\n\ntable T\n{\n    m map[uint8]int32\n}\n",
		"package probe\n\ntable T\n{\n    m map[uint32]uint32\n}\n",
		"package probe\n\ntable T\n{\n    m map[uint32][..4]int32\n}\n",
		"package probe\n\ntable T\n{\n    m map[uint32]?int32\n}\n",
		"package probe\n\ntable T\n{\n    m map[uint32]map[uint8]int32\n}\n",
		"package probe\n\ntable T\n{\n    m int32\n}\n",
	} {
		f := mustFingerprint(t, src)
		if prev, dup := prints[f]; dup {
			t.Errorf("%q and %q fingerprint alike — the formatter's structural check cannot tell them apart", strings.TrimSpace(prev), strings.TrimSpace(src))
		}
		prints[f] = src
	}
}

func mustFingerprint(t *testing.T, src string) string {
	t.Helper()
	if _, err := Format("Probe.schema", []byte(src)); err != nil {
		t.Fatalf("format %q: %v", src, err)
	}
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse %q: %v", src, errs[0])
	}
	return fingerprint(f)
}

// fencedBlockContaining returns the body of the nth ``` block in page holding
// a line equal to anchor once trimmed, and fails when there is none: an
// anchor that stopped existing means this gate stopped covering the example
// it was written for.
func fencedBlockContaining(t *testing.T, page, anchor string, nth int) string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	inside, found, seen := false, false, 0
	var body []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "```") {
			if inside && found {
				if seen++; seen == nth {
					return strings.Join(body, "\n") + "\n"
				}
			}
			inside = !inside
			body, found = nil, false
			continue
		}
		if !inside {
			continue
		}
		body = append(body, line)
		if strings.TrimSpace(line) == anchor {
			found = true
		}
	}
	t.Fatalf("%s: no fenced block holds the line %q", page, anchor)
	return ""
}
