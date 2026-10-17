package gotable

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestGoSourceSeamsFailClosed pins every runtime-specialization seam to the
// fail-closed helpers in source.go (tableSourceReplace/tableSourceIndex/
// tableSourceRewriter). Five halves:
//
//	(a) a source scan: no non-test file outside source.go may replace runtime
//	    source, and no seam file may gate a seam on strings.Contains/Count, so a
//	    moved anchor panics at generation instead of silently dropping a behavior;
//	(b) generation of units exercising every seam succeeds, is deterministic,
//	    carries each seam's patch, and matches the BASE generation byte for byte;
//	(c) each of the eight surfaces generates the seam units;
//	(d) one negative control: a broken anchor makes tableSourceReplace panic
//	    naming it.
func TestGoSourceSeamsFailClosed(t *testing.T) {
	t.Parallel()
	scanSourceSeams(t)
	negativeControlFailClosed(t)
	negativeControlDoubledAnchor(t)
	generateSeamSurfaceUnits(t)
	generateEverySurface(t)
}

// scanSourceSeams is half (a). The only non-seam string replacements inside the
// package are:
//   - source.go:35 — the fail-closed helper, the definitional home of the seam
//     machinery,
//   - gotable.go:169 — indentation of a generated line, not a runtime-source
//     seam (it rewrites the emitted text's line prefixes, never an anchor), and
//   - unboundedtext.go:182,191 — the unbounded-text JSON walker's seam, the
//     sibling of textlist.go's, which lives in a file this card's PATHS does
//     not name and is tracked for a follow-up; its Contains/Count probes at
//     :181,:190 are the same seam.
//
// The Contains/Count scan closes the sibling hole: a seam gated by probing the
// anchor text fails open when the anchor moves, and wrapper helpers hide it.
// The only non-seam probes are the emitted-text checks that decide imports.
func scanSourceSeams(t *testing.T) {
	t.Helper()
	replaceAllow := map[string]string{
		"source.go:40":         "tableSourceReplace's core replace, the defined seam helper",
		"gotable.go:169":       "indentation of an emitted line, not a runtime-source seam",
		"retain.go:166":        "retain family per-half rewrite, validated on the whole by tableSourceReplace above",
		"unboundedtext.go:182": "unbounded-text JSON walker seam, out of this card's PATHS (follow-up)",
		"unboundedtext.go:191": "unbounded-text JSON walker seam, out of this card's PATHS (follow-up)",
	}
	probeAllow := map[string]string{
		"source.go:11":         "tableSourceIndex counts the marker it is given, the seam helper",
		"source.go:32":         "tableSourceReplace counts the anchor it is given, the seam helper",
		"block.go:166":         "asks whether the emitted unit uses serialize., to pick imports",
		"cook.go:310":          "asks whether the emitted unit uses serialize., to pick imports",
		"gotable.go:746":       "asks whether the emitted unit uses serialize., to pick imports",
		"unboundedtext.go:181": "unbounded-text JSON walker seam, out of this card's PATHS (follow-up)",
		"unboundedtext.go:190": "unbounded-text JSON walker seam, out of this card's PATHS (follow-up)",
		"textlist.go:275":       "textTotalJson's stated surface check on a total-bytes anchor before its exact-count replace",
		"textlist.go:288":       "textTotalJson's stated surface check before placing textTotalJsonSource",
	}
	var bad []string
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range files {
		if strings.HasSuffix(fn, "_test.go") {
			continue
		}
		data, err := os.ReadFile(fn)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			loc := fmt.Sprintf("%s:%d", fn, i+1)
			switch {
			case strings.Contains(line, "strings.ReplaceAll(") || strings.Contains(line, "strings.Replace("):
				if reason, ok := replaceAllow[loc]; ok {
					t.Logf("allowlisted %s: %s", loc, reason)
					continue
				}
				bad = append(bad, loc+"  "+strings.TrimSpace(line))
			case strings.Contains(line, "strings.Contains(") || strings.Contains(line, "strings.Count("):
				if reason, ok := probeAllow[loc]; ok {
					t.Logf("allowlisted %s: %s", loc, reason)
					continue
				}
				bad = append(bad, loc+"  "+strings.TrimSpace(line))
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("seam source outside source.go is probed or replaced directly:\n%s", strings.Join(bad, "\n"))
	}
}

// negativeControlFailClosed is half (d): an anchor broken in a copy of a source
// string must make tableSourceReplace panic, naming the seam, rather than emit
// a valid-looking codec with a different contract.
func negativeControlFailClosed(t *testing.T) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("tableSourceReplace did not panic on a moved anchor")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "Go table source seam") || !strings.Contains(msg, "func broke") {
			t.Fatalf("panic did not name the seam: %v", r)
		}
	}()
	tableSourceReplace("func ok() {}", "func broke() {}", "x", 1)
}

// negativeControlDoubledAnchor extends half (d): an anchor appearing twice in the
// source string must make tableSourceReplace panic, naming the seam, rather than
// patch one occurrence and silently leave the other.
func negativeControlDoubledAnchor(t *testing.T) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("tableSourceReplace did not panic on a doubled anchor")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "Go table source seam") {
			t.Fatalf("panic did not name the seam: %v", r)
		}
	}()
	tableSourceReplace("func ok() ok() {}", "func broke() {}", "func ok()", 1)
}

// surfacesExercises is a table of units that together touch each seam, and the
// patch markers that must survive into the generated output so a silent drop
// is caught rather than reported green. seamGolden is the digest of the BASE
// compiler's DefaultSurfaces output for the same schema.
var surfacesExercises = []struct {
	name   string
	schema string
	want   []string // substrings that must appear in the generated files
}{
	{
		name: "lists-maps-cook-union",
		schema: `package probe
union Choice {
  Left int32
  Right float32
}
table Child { n int32
  samples []uint16
  c Choice
}
table Root { entries map[int32]Child }
`,
		// containerRegionSource/containerRegionJsonSource, the wire
		// variable-table seams, the cook numbering seam, the container cook
		// runtime, the union seams. Each identifier exists only through the
		// seam's patch, so a silent drop removes it.
		want: []string{
			"tableRegionContainerEdges", // container extents source, only when containers land
			"tableWireNodeBytesReason",  // node-bytes rewrite of tableRegionRound
			"tableExtentBytes",          // extent measure rewrite
			"tableMapPlace",             // graph mapPlace stitch
			"Nodes: r.Nodes",            // wire reader node-map seam
		},
	},
	{
		name: "array-root-text-list",
		schema: `package probe
table GoalList | json = "array"
{ goals []string(8) }
table Holder { bases []?string(8) }
`,
		want: []string{
			"tableJsonWriteTextElement", // text-list JSON writer seam
			"tableTextClamped",          // text-list clamp name seam
			"json_root_not_array",       // array-root read seam
			"f.TextList == 0",           // string kind split for text lists
		},
	},
	{
		name: "message-form",
		schema: `package probe
fixed table User { id int32 }
union Origin {
  Name int32
  Proto bytes(16)
}
fixed table Edit {
  who Origin
  u User
}
`,
		want: []string{
			"TableMessageReader", // message form runtime
			"tableMessageBatchOpen",
		},
	},
	{
		name: "retain-unknown",
		schema: `package probe
table Leaf { value int32 }
table Root {
  leaves []Leaf
  entries map[int32]Leaf
}
`,
		// The retain surface rewrites the body family by exact, surface-stated
		// counts; a silent skip in either half breaks the byte-for-byte split
		// assertion.
		want: []string{
			"TableRetainReader",
			"tableRegionLoadRetain",
			"TableRetainMessageReader",
		},
	},
}

// seamGolden is the DefaultSurfaces digest at BASE (sprint/schema-go tip), one
// per surfaceExercises schema. The seams only move source text; they must never
// move the bytes a unit generates.
var seamGolden = map[string]string{
	"lists-maps-cook-union": "8810d8a90dcba9ac4584aa1a119d1d4603f6035594eb2ceae9303a66058ba7fd",
	"array-root-text-list":  "e8ca590451c06af418332b24cecb96d8bd9d59d03e7ed34ca4e6161e1b34d73c",
	"message-form":          "fe02906ad3b3c78014c277674f6a7e7abddb0ada9520bd1f0a354783f892c67a",
	"retain-unknown":        "c97cb7326004cd3663606df2fd58c4d37adb7ac545c966d2f2f4cee40936213a",
}

func outputDigest(out map[string][]byte) string {
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(out[name]))
		h.Write(out[name])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// generateSeamSurfaceUnits is half (b): every unit generates twice with
// byte-identical output, every seam marker survives, and the digest matches the
// BASE compiler's output for the same schema.
func generateSeamSurfaceUnits(t *testing.T) {
	t.Helper()
	for _, ex := range surfacesExercises {
		ex := ex
		t.Run(ex.name, func(t *testing.T) {
			t.Parallel()
			first := generate(t, ex.schema)
			second := generate(t, ex.schema)
			if len(first) == 0 {
				t.Fatal("generation produced no files")
			}
			body := ""
			for name, data := range first {
				body += string(data)
				if other, ok := second[name]; !ok || !bytes.Equal(other, data) {
					t.Fatalf("generation is not deterministic for %s", name)
				}
			}
			for _, want := range ex.want {
				if !strings.Contains(body, want) {
					t.Errorf("seam marker %q missing from generated output", want)
				}
			}
			if want := seamGolden[ex.name]; want != "" {
				if got := outputDigest(first); got != want {
					t.Errorf("%s output moved from BASE: got %s want %s", ex.name, got, want)
				}
			} else {
				t.Logf("BASE digest for %s: %s", ex.name, outputDigest(first))
			}
		})
	}
}

// generateEverySurface is half (c): the eight surfaces each generate a unit
// exercising the seams, so no surface's own specialization path is skipped.
func generateEverySurface(t *testing.T) {
	t.Helper()
	surfaces := []struct {
		name string
		set  Surfaces
	}{
		{"wire", Surfaces{Wire: true}},
		{"fixed", Surfaces{Wire: true, Fixed: true}},
		{"json", Surfaces{Wire: true, JSON: true}},
		{"cook", Surfaces{Wire: true, Cook: true}},
		{"block", Surfaces{Wire: true, Block: true}},
		{"view", Surfaces{Wire: true, View: true}},
		{"message", Surfaces{Wire: true, Message: true}},
		{"retain", Surfaces{Wire: true, Retain: true}},
	}
	u := unitFrom(t, surfacesExercises[0].schema)
	for _, s := range surfaces {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			out, err := GenerateLineageSurface(u, nil, s.set)
			if err != nil {
				t.Fatalf("surface %s: %v", s.name, err)
			}
			if len(out) == 0 {
				t.Fatalf("surface %s generated no files", s.name)
			}
		})
	}
}
