package gotable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoSourceSeamsFailClosed pins every runtime-specialization seam to the
// fail-closed helpers in source.go (tableSourceReplace/tableSourceIndex/
// tableSourceRewriter). Three halves:
//
//	(a) a source scan: no non-test file outside source.go may call
//	    strings.Replace/strings.ReplaceAll on runtime source, so a moved
//	    anchor panics at generation instead of silently dropping a behavior;
//	(b) generation of units exercising every seam succeeds, is deterministic,
//	    and carries the seam's patch (nothing silently dropped);
//	(c) one negative control: a broken anchor makes tableSourceReplace panic
//	    naming it.
func TestGoSourceSeamsFailClosed(t *testing.T) {
	t.Parallel()
	scanSourceSeams(t)
	negativeControlFailClosed(t)
	generateSeamSurfaceUnits(t)
}

// scanSourceSeams is half (a). The only non-seam string replacements inside
// the package are:
//   - source.go:35 and source.go:52 — the fail-closed helpers themselves, the
//     definitional home of the seam machinery, and
//   - gotable.go:166 — indentation of a generated line, not a runtime-source
//     seam (it rewrites the emitted text's line prefixes, never an anchor).
func scanSourceSeams(t *testing.T) {
	t.Helper()
	allow := map[string]string{
		"source.go:35":   "tableSourceReplace's core replace, the defined seam helper",
		"source.go:52":   "tableSourceReplaceAllAny's core replace, the defined split helper",
		"gotable.go:166": "indentation of an emitted line, not a runtime-source seam",
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
			if !strings.Contains(line, "strings.ReplaceAll(") && !strings.Contains(line, "strings.Replace(") {
				continue
			}
			loc := fmt.Sprintf("%s:%d", fn, i+1)
			if reason, ok := allow[loc]; ok {
				t.Logf("allowlisted %s: %s", loc, reason)
				continue
			}
			bad = append(bad, loc+"  "+strings.TrimSpace(line))
		}
	}
	if len(bad) > 0 {
		t.Errorf("strings.Replace/ReplaceAll on runtime source outside source.go:\n%s", strings.Join(bad, "\n"))
	}
}

// negativeControlFailClosed is half (c): an anchor broken in a copy of a source
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

// surfacesExercises is a table of units that together touch each seam, and the
// patch markers that must survive into the generated output so a silent drop
// is caught rather than reported green.
var surfaceExercises = []struct {
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
		// tableMessageCoreTypes (message reader Nodes seam is gated on
		// variable tables, which this unit has none of, so the message form
		// itself and the batch surface are what this unit exercises).
		want: []string{
			"TableMessageReader", // message form runtime
			"tableMessageBatchOpen",
		},
	},
}

// generateSeamSurfaceUnits is half (b): every unit generates, twice, with
// byte-identical output, and every seam marker survives.
func generateSeamSurfaceUnits(t *testing.T) {
	t.Helper()
	for _, ex := range surfaceExercises {
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
		})
	}
}
