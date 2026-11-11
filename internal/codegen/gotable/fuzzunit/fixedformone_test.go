package tabledemo_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestGeneratedFixedLoadRefusesForm1 holds docs/SPEC-TABLES.md §3.4 ("a
// reader that expects a fixed table refuses every other form") on both fixed
// readers: for every fixed root of the examples unit, the generated
// <T>FixedLoad and the engine's Decode both refuse a form-1 file, the
// variable form's file and the fixed image with its form byte set to 1. The
// generated read names it previous_form (a diagnostic, pinned here so the
// form gate itself is held).
func TestGeneratedFixedLoadRefusesForm1(t *testing.T) {
	t.Parallel()
	paths, _ := compiler.GatherPaths([]string{"../../../../tables/examples"})
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	var ranged tabledemo.RangedSigned
	tabledemo.RangedSignedReset(&ranged)
	ranged.I8Inside = 9
	// The plain <T>Save of a fixed root writes form 3; the variable form's
	// file (form byte 1, the body, the id trailer) is built from its body
	// writer, as a form-1 producer would.
	saved := make([]byte, tabledemo.RangedSignedMeasure(&ranged))
	if n := tabledemo.RangedSignedSave(&ranged, saved); n != int64(len(saved)) || saved[0] != tabledemo.TableFixedForm {
		t.Fatalf("the plain save wrote %d bytes, form %d", n, saved[0])
	}
	var ids tabledemo.TableIds
	sizer := tabledemo.TableWriter{Measuring: true, Ids: &ids}
	sizer.Put8(1)
	if !tabledemo.RangedSignedSaveBody(&sizer, &ranged) {
		t.Fatal("the variable body refused the value")
	}
	sizer.Trailer()
	ids = tabledemo.TableIds{}
	plain := make([]byte, sizer.Offset)
	w := tabledemo.TableWriter{Buffer: plain, Ids: &ids}
	w.Put8(1)
	if !tabledemo.RangedSignedSaveBody(&w, &ranged) {
		t.Fatal("the variable body refused the value")
	}
	w.Trailer()
	if w.Overflow || ids.Overflow || w.Offset != int64(len(plain)) || plain[0] != 1 {
		t.Fatalf("the form-1 file wrote %d of %d bytes", w.Offset, len(plain))
	}
	for _, target := range fixedTargets {
		root := m.Lookup(target.name)
		if root == nil || !tablewire.FixedRoot(m, root) {
			continue
		}
		image := make([]byte, target.measure(1))
		target.save(target.slice(target.pair(target.zero(), target.zero()), 1), image)
		forged := append([]byte(nil), image...)
		forged[0] = 1
		inputs := map[string][]byte{"form byte 1": forged}
		if target.name == "RangedSigned" {
			inputs["the variable form's file"] = plain
		}
		for what, in := range inputs {
			var rep tabledemo.TableReport
			if n := target.load(target.blank(1), append([]byte(nil), in...), target.plan(), &rep); n >= 0 || rep.Reason != "previous_form" {
				t.Errorf("%s: %s: FixedLoad answered n=%d reason=%q, want a previous_form refusal", target.name, what, n, rep.Reason)
			}
			var erep tabletext.Report
			if ok, _ := tablewire.Decode(m, m.New(root), in, &erep); ok {
				t.Errorf("%s: %s: the engine read accepted a form-1 file at a fixed root", target.name, what)
			}
		}
	}
}
