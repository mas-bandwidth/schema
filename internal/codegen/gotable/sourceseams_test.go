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

// seamAllow is one allow-list entry: the exact number of times a file must
// hold the trimmed source line, and why that line is not a runtime-source seam.
type seamAllow struct {
	count  int
	reason string
}

// seamContentKey is the allow-list key: a file name and the trimmed source
// line, never a line number, so moving a seam cannot turn the scan red.
func seamContentKey(fn, line string) string {
	return fn + ": " + strings.TrimSpace(line)
}

var seamReplaceAllow = map[string]seamAllow{
	seamContentKey("source.go", `return strings.Replace(source, old, replacement, count)`):         {1, "tableSourceReplace's core replace, the defined seam helper"},
	seamContentKey("gotable.go", `s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)`):      {1, "indentation of an emitted line, not a runtime-source seam"},
	seamContentKey("retain.go", `source = strings.ReplaceAll(source, rewrites[i], rewrites[i+1])`): {1, "retain family per-half rewrite, validated on the whole by tableSourceReplace above"},
}

var seamProbeAllow = map[string]seamAllow{
	seamContentKey("source.go", `if count := strings.Count(source, marker); count != 1 {`):                            {1, "tableSourceIndex counts the marker it is given, the seam helper"},
	seamContentKey("source.go", `got := strings.Count(source, old)`):                                                  {1, "tableSourceReplace counts the anchor it is given, the seam helper"},
	seamContentKey("block.go", `if strings.Contains(g.structs.String(), "serialize.") {`):                             {1, "asks whether the emitted unit uses serialize., to pick imports"},
	seamContentKey("cook.go", `if strings.Contains(g.structs.String(), "serialize.") {`):                              {1, "asks whether the emitted unit uses serialize., to pick imports"},
	seamContentKey("gotable.go", `usesSerialize := strings.Contains(g.types.String()+g.body.String(), "serialize.")`): {1, "asks whether the emitted unit uses serialize., to pick imports"},
	seamContentKey("gotable.go", `si, sj := !strings.Contains(paths[i], "."), !strings.Contains(paths[j], ".")`):      {1, "sorts the emitted unit's import paths for ordering (stdlib first); operates on path strings, never on emitted or runtime source"},
}

// scanSourceSeams is half (a). The allow-listed non-seam string replacements
// inside the package are:
//   - source.go — the fail-closed helper, the definitional home of the seam
//     machinery,
//   - gotable.go — indentation of a generated line, not a runtime-source seam
//     (it rewrites the emitted text's line prefixes, never an anchor), and
//   - retain.go — the retain family's per-half rewrite, validated on the whole
//     by tableSourceReplace before it runs.
//
// The Contains/Count scan closes the sibling hole: a seam gated by probing the
// anchor text fails open when the anchor moves, and wrapper helpers hide it.
// A Replace inside a rep closure named by an allow-listed probe is that same
// seam, not a second one, so only the probe line is listed.
// The only allow-listed probes are the emitted-text checks that decide whether
// the emitted unit uses serialize., plus the emitted-import sort in gotable.go
// that orders import paths (stdlib first) and so touches no source text.
func scanSourceSeams(t *testing.T) {
	t.Helper()
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
		bad = append(bad, seamAllowFindings(fn, strings.Split(string(data), "\n"))...)
	}
	if len(bad) > 0 {
		t.Errorf("seam source outside source.go is probed or replaced directly:\n%s", strings.Join(bad, "\n"))
	}
}

// seamAllowFindings is the allow-list check over a file name and its lines. It
// fails closed both ways: a seam-looking line beyond an allow entry's count is
// a finding, and an allow entry whose file now holds fewer matches than its
// count is a finding too.
func seamAllowFindings(fn string, lines []string) []string {
	counts := map[string]int{}
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if strings.Contains(text, "strings.ReplaceAll(") || strings.Contains(text, "strings.Replace(") ||
			strings.Contains(text, "strings.Contains(") || strings.Contains(text, "strings.Count(") {
			counts[seamContentKey(fn, text)]++
		}
	}
	var bad []string
	for i, line := range lines {
		loc := fmt.Sprintf("%s:%d", fn, i+1)
		text := strings.TrimSpace(line)
		key := seamContentKey(fn, text)
		switch {
		case strings.Contains(line, "strings.ReplaceAll(") || strings.Contains(line, "strings.Replace("):
			if _, ok := seamReplaceAllow[key]; ok {
				continue
			}
			if _, ok := seamProbeAllow[key]; ok {
				continue
			}
			if _, ok := probeClosure(fn, lines, i); ok {
				continue
			}
			bad = append(bad, loc+"  "+text)
		case strings.Contains(line, "strings.Contains(") || strings.Contains(line, "strings.Count("):
			if _, ok := seamProbeAllow[key]; ok {
				continue
			}
			bad = append(bad, loc+"  "+text)
		}
	}
	for key, allow := range seamReplaceAllow {
		bad = append(bad, seamCountFindings(fn, key, counts, allow)...)
	}
	for key, allow := range seamProbeAllow {
		bad = append(bad, seamCountFindings(fn, key, counts, allow)...)
	}
	sort.Strings(bad)
	return bad
}

// seamCountFindings reports an allow-list entry whose file holds a different
// number of matches than the entry states, naming the file, text and counts.
func seamCountFindings(fn, key string, counts map[string]int, allow seamAllow) []string {
	prefix := fn + ": "
	if !strings.HasPrefix(key, prefix) || counts[key] == allow.count {
		return nil
	}
	return []string{fmt.Sprintf("%s %q occurs %d times, allow-list says %d", fn, strings.TrimPrefix(key, prefix), counts[key], allow.count)}
}

// probeClosure reports the allow-listed Contains/Count probe that a Replace on
// line i implements. A seam written as a rep closure probes its anchor and then
// replaces it; the probe line is the seam's single allow-list entry, keyed by
// its content.
func probeClosure(fn string, lines []string, i int) (string, bool) {
	for j := i - 1; j >= 0 && i-j <= 8; j-- {
		if !strings.Contains(lines[j], "strings.Contains(") && !strings.Contains(lines[j], "strings.Count(") {
			continue
		}
		if allow, ok := seamProbeAllow[seamContentKey(fn, lines[j])]; ok {
			return allow.reason, true
		}
	}
	return "", false
}

// TestGoSourceSeamsAllowListIsByContent pins the allow-list to file name, line
// text and count, never line number: a listed line moved stays green, one
// extra matching line is a finding, and a listed entry with fewer matches than
// its count is a finding.
func TestGoSourceSeamsAllowListIsByContent(t *testing.T) {
	t.Parallel()
	const (
		indent = `s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)`
		probe  = `usesSerialize := strings.Contains(g.types.String()+g.body.String(), "serialize.")`
		sortI  = `si, sj := !strings.Contains(paths[i], "."), !strings.Contains(paths[j], ".")`
	)
	listed := []string{
		"func (g *tableGen) pf(format string, args ...any) {",
		indent,
		"",
		probe,
		sortI,
		"}",
	}
	if bad := seamAllowFindings("gotable.go", listed); len(bad) != 0 {
		t.Fatalf("listed lines should stay green, got findings:\n%s", strings.Join(bad, "\n"))
	}
	moved := append([]string{"", "", "// padding", "", ""}, listed...)
	if bad := seamAllowFindings("gotable.go", moved); len(bad) != 0 {
		t.Fatalf("listed lines at other line numbers should stay green, got findings:\n%s", strings.Join(bad, "\n"))
	}
	extra := append(append([]string{}, listed...), "", indent)
	bad := seamAllowFindings("gotable.go", extra)
	if len(bad) != 1 || !strings.Contains(bad[0], "gotable.go") || !strings.Contains(bad[0], "occurs 2 times") || !strings.Contains(bad[0], "allow-list says 1") {
		t.Fatalf("an extra matching line should be one finding naming file, text and counts, got:\n%s", strings.Join(bad, "\n"))
	}
	bad = seamAllowFindings("gotable.go", []string{indent, sortI})
	if len(bad) != 1 || !strings.Contains(bad[0], "gotable.go") || !strings.Contains(bad[0], "occurs 0 times") || !strings.Contains(bad[0], "allow-list says 1") {
		t.Fatalf("a stale allow-list entry should be one finding naming file, text and counts, got:\n%s", strings.Join(bad, "\n"))
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
	{
		name: "text-total-seams",
		schema: `package probe
table Bounded { rules [..16]string(64) | total = 256 }
table Unbounded { repos []?string(64) | total = 4096 }
table Plain { names []string }
`,
		// The bounded total list keeps only the prefix within the total on a
		// JSON read; the unbounded total list erases the element past it through
		// the region graph's listDrop; the plain []string list reaches the
		// unbounded text reader. Each identifier exists only through its seam.
		want: []string{
			"tableJsonTextTotalKeep(", // bounded read keeps the prefix within the total
			"tableJsonTextBytes(",     // the write side sums the element bytes
			"listDrop:",               // region graph's erase closure for the unbounded list
			"tableJsonReadText(",      // the plain []string element reader
		},
	},
	{
		name: "optional-map",
		schema: `package probe
table Leaf { v int32 }
table Root { m ?map[int32]Leaf }
`,
		// The optional map's reset seam: a `null` reads as ABSENT and puts the
		// field back at its defaults. The marker exists only through the seam's
		// patch, so a silent drop removes it.
		want: []string{
			"if f.Map {",
		},
	},
}

// seamGolden is the DefaultSurfaces digest at BASE (sprint/schema-go tip), one
// per surfaceExercises schema. The seams only move source text; they must never
// move the bytes a unit generates.
var seamGolden = map[string]string{
	"lists-maps-cook-union": "dfde67642c0edbe585df1e14a8e84a804e4a82e14ff39047572e24bc969ea423",
	"array-root-text-list":  "c58290f38ec15085bfdee280707c7230a5b727b41436501816cf4491b7eab432",
	"message-form":          "a2e4f6ce0af6127cabb7bb0a816c535ff96f81b8f06b4f2e998299c40d60f097",
	"retain-unknown":        "066c8f027f7e21e4864add3d76d7d9f4c7df62779c4972f79b2e1f59b3b69668",
	"text-total-seams":      "a9073d1f02312301b3cfe370e2045d47e45edac802b22b9f09a37d5ed7a909ec",
	"optional-map":          "5b861a44292a6bdb39aba746c46eb39a2cd4683e56cf25a6fc536485572cdf8f",
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
