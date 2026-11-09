package cpptable

// THE C++ VARIABLE-CLASS STRICT-100 CELLS (docs/roadmap-evidence/
// zz-strict-100-c-cpp.sexp). Each cell is the ONE READ (docs/SPEC-TABLES.md
// §4, §6.5): a read returns the whole value or refuses the whole load. The
// cells this card closes are held two ways, both against the real artifacts:
//
//   - message-form (vt/message-form/cpp): the runtime probe
//     cppMessageStrictRun drives the generated strict read on a value past its
//     declared range and requires the whole load to refuse (count 0, the
//     value reset to its default, out_of_range, no damage). This is the same
//     probe TestCppMessageFormStrict runs, called here because the evidence
//     overlay cites this test.
//
//   - text-form, wstring, widening, fixed-class, maps, unbounded-arrays,
//     id-table-wire, retain-unknown: the emitter, the runtime and the
//     conformance driver carry the fix that makes the strict read hold, and
//     the gate reads the REAL source for it. A test that checks the emitted
//     source holds a cell (docs/roadmap-evidence/zz-strict-100-c-cpp.sexp,
//     calibration): these are the concrete spellings of the refusals the
//     audit cited as missing.
//
// The negative control plants the pre-fix spelling in a COPY of each real
// artifact and requires the same gate to go red on exactly the cell it
// sabotaged: a gate that cannot see a plant in the real artifact proves
// nothing about the unplanted one.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppVarCellAnchor names one real artifact and the strict-refusal spelling it
// must carry. Each is the concrete fix the audit cited as missing.
var cppVarCellAnchors = map[string]struct {
	src  string
	want []string
}{
	// vt/text-form: the JSON write refuses storage that is not text (§16.14
	// W0) by marking the sink invalid instead of writing U+FFFD.
	"vt/text-form/cpp": {
		src: "internal/codegen/cpptable/json.go",
		want: []string{
			"out.invalid = true; // not text: the write refuses (§16.14 W0)",
		},
	},
	// vt/wstring: an unpaired surrogate is not text; the write refuses
	// (§16.14 W0) rather than writing U+FFFD.
	"vt/wstring/cpp": {
		src: "internal/codegen/cpptable/json.go",
		want: []string{
			"out.invalid = true; // an unpaired surrogate is not text: the write refuses (§16.14 W0)",
		},
	},
	// vt/widening: a widened value that lands outside the declared range
	// refuses out_of_range on the raw scale, never reading it.
	"vt/widening/cpp": {
		src: "internal/codegen/cpptable/widen.go",
		want: []string{
			"r.report->refuse_reason = out_of_range;",
		},
	},
	// vt/fixed-class: the conformance driver's form-1 read gates on the
	// strict framing walk (framing_strict_ok); damage anywhere refuses the
	// load before any value is decoded (D1).
	"vt/fixed-class/cpp": {
		src: "test/conformance/cpp/main.cpp",
		want: []string{
			"const bool framing_ok = !form1 || framing_strict_ok(",
			"static bool framing_strict_ok(",
		},
	},
	// vt/doc-tags: the table's doc/tags negative controls plant the retired
	// per-row spellings in a copy of the generated descriptor and require the
	// tree to fail, holding the shared TableDocNone string claim.
	"vt/doc-tags/cpp": {
		src: "Makefile",
		want: []string{
			"tables-doctags-negative-controls:",
			`s|", TableDocNone, 0, NULL, false, false, 0, 0, false }|", NULL, 0, NULL, false, false, 0, 0, false }|`,
			"NEGATIVE CONTROL FAILED: a NULL doc column did not take the gate down",
		},
	},
	// vt/maps: a repeated key is a malformed map and the strict read refuses
	// the whole load (§2.8, §6.5) instead of picking a winner. The duplicate
	// rule's refusal is the concrete spelling the audit cited as missing.
	"vt/maps/cpp": {
		src: "internal/codegen/cpptable/maps.go",
		want: []string{
			`if ( order == 0 )\n%[1]s        {\n%[1]s            r.report->refused = true;\n%[1]s            r.report->refuse_reason = malformed;\n%[1]s        }`,
		},
	},
	// vt/unbounded-arrays: a list read under a changed element kind refuses the
	// whole load as kind_mismatch (§4, §6.5) instead of decoding the elements
	// under defaulted kinds.
	"vt/unbounded-arrays/cpp": {
		src: "internal/codegen/cpptable/lists.go",
		want: []string{
			`else if ( elem_kind != %[2]d )\n%[1]s    {\n%[1]s        r.report->refused = true;\n%[1]s        r.report->refuse_reason = kind_mismatch;\n%[1]s        r.offset = body_end;\n%[1]s        break;\n%[1]s    }`,
		},
	},
	// vt/id-table-wire: the conformance driver's form-1 framing walk refuses a
	// repeated field id (R5, which is how a node table riding twice is caught)
	// and a node record no pointer reaches (T1, node_index_null), so those
	// id-table wiring damages refuse the whole load before any value decodes.
	"vt/id-table-wire/cpp": {
		src: "test/conformance/cpp/main.cpp",
		want: []string{
			"for ( size_t k = 0; k < seen.size(); k++ ) if ( seen[k] == id ) return false; // R5",
			"for ( size_t k = 1; k < n; k++ ) if ( mark[k] == 0 ) return false; // unreachable is damage",
		},
	},
	// vt/retain-unknown: a retaining read that cannot capture a field discards
	// the whole load (damage + refusal) rather than continuing with a partial
	// value (§6.6); the generated reader spells that guard here.
	"vt/retain-unknown/cpp": {
		src: "internal/codegen/cpptable/codecs.go",
		want: []string{
			"if ( !TableRetainCapture( retain, r, path, field_id, kind ) )",
		},
	},
}

// cppVarCellSources loads the real artifacts the anchors read.
func cppVarCellSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	out := map[string]string{}
	for _, a := range cppVarCellAnchors {
		if _, ok := out[a.src]; ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, a.src))
		if err != nil {
			t.Fatalf("read %s: %v", a.src, err)
		}
		out[a.src] = string(b)
	}
	return out
}

// cppVarCellsFindings reports the cells whose real artifact no longer carries
// the strict-refusal spelling. Empty means every anchored cell holds.
func cppVarCellsFindings(sources map[string]string) []string {
	var out []string
	for cell, a := range cppVarCellAnchors {
		src, ok := sources[a.src]
		if !ok {
			out = append(out, fmt.Sprintf("%s: source %s missing", cell, a.src))
			continue
		}
		for _, w := range a.want {
			if !strings.Contains(src, w) {
				out = append(out, fmt.Sprintf("%s: %s no longer carries %q", cell, a.src, w))
			}
		}
	}
	return out
}

// TestCppVariableCellsStrict holds the C++ variable-class strict-100 cells:
// the message-form strict read at runtime, and the text-form, wstring,
// widening, fixed-class, maps, unbounded-arrays, id-table-wire and
// retain-unknown strict-refusal spellings in the real artifacts.
func TestCppVariableCellsStrict(t *testing.T) {
	// message-form: the runtime strict read refuses the whole load on an
	// out_of_range value. This is the same probe TestCppMessageFormStrict
	// runs; it is a functional test (it compiles and runs the C++ probe).
	t.Run("vt/message-form/cpp", func(t *testing.T) {
		slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
		out, err := cppMessageStrictRun(t, nil)
		if err != nil {
			t.Fatalf("the message-form strict read: %v\n%s", err, out)
		}
	})

	sources := cppVarCellSources(t)
	for _, finding := range cppVarCellsFindings(sources) {
		t.Error(finding)
	}
}

// TestCppVariableCellsStrictNegativeControl plants the pre-fix spelling in a
// COPY of each real artifact and requires the same gate to go red on exactly
// the cell it sabotaged. It also removes the message-form strict read's
// whole-load refusal guard and requires the probe to go red.
func TestCppVariableCellsStrictNegativeControl(t *testing.T) {
	base := cppVarCellSources(t)
	if got := cppVarCellsFindings(base); len(got) != 0 {
		t.Fatalf("the unplanted real artifacts already fail the gate: %v", got)
	}

	for cell, a := range cppVarCellAnchors {
		planted := map[string]string{}
		maps.Copy(planted, base)
		for _, w := range a.want {
			planted[a.src] = strings.ReplaceAll(planted[a.src], w, "/* SABOTAGED: the refusal spelling was removed */")
		}
		got := cppVarCellsFindings(planted)
		found := false
		for _, f := range got {
			if strings.HasPrefix(f, cell+":") {
				found = true
			}
		}
		if !found {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s did not go red when its refusal spelling was planted out", cell)
		}
	}

	// message-form: remove the generated strict read's whole-load refusal
	// guard, so the body ends as a value even when it refused.
	t.Run("vt/message-form/cpp", func(t *testing.T) {
		slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
		const guard = "if ( report->refused ) { return false; } // a refusal refuses the whole batch (§3.3, §4)"
		undermine := func(files map[string][]byte) {
			for name, data := range files {
				if strings.HasSuffix(name, "Table.h") {
					text := string(data)
					if strings.Count(text, guard) != 1 {
						t.Fatalf("%s: the refusal guard is not there exactly once", name)
					}
					files[name] = []byte(strings.ReplaceAll(text, guard, ""))
				}
			}
		}
		out, err := cppMessageStrictRun(t, undermine)
		if err == nil {
			t.Fatalf("with the refusal guard removed, the strict probe still passed:\n%s", out)
		}
	})
}
