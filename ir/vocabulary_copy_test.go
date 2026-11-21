package ir

import (
	"maps"
	"slices"
	"testing"
)

// THE EXPORTED VOCABULARIES ARE COPIES (F-190). An importer that filters its
// own view by editing ir.TableForms, ir.TableIntegrityChecks, ir.WireForms or
// a verb list must not change what the checker, the registry or another
// compiler in the same process answers. The test empties every exported copy,
// asks the authorities, and puts the copies back. It is not parallel: it
// writes package variables, and every parallel test waits for it.
func TestExportedVocabulariesAreCopies(t *testing.T) {
	forms, checks := maps.Clone(TableForms), maps.Clone(TableIntegrityChecks)
	registry := slices.Clone(WireForms)
	mapVerbs, listVerbs, textVerbs := slices.Clone(MapFieldVerbs), slices.Clone(ListFieldVerbs), slices.Clone(TextListVerbs)
	t.Cleanup(func() {
		TableForms, TableIntegrityChecks, WireForms = forms, checks, registry
		MapFieldVerbs, ListFieldVerbs, TextListVerbs = mapVerbs, listVerbs, textVerbs
	})

	// Each copy starts equal to its authority.
	if !maps.Equal(forms, tableForms) || !maps.Equal(checks, tableIntegrityChecks) || !slices.Equal(registry, wireForms) ||
		!slices.Equal(mapVerbs, mapFieldVerbs) || !slices.Equal(listVerbs, listFieldVerbs) || !slices.Equal(textVerbs, textListVerbs) {
		t.Fatal("an exported vocabulary differs from the one the compiler reads")
	}

	// An importer's edits: delete, overwrite in place, and replace.
	clear(TableForms)
	clear(TableIntegrityChecks)
	WireForms[3] = WireForm{Byte: 3, Name: "edited by an importer", Planned: true}
	for i := range MapFieldVerbs {
		MapFieldVerbs[i] = ""
	}
	for i := range ListFieldVerbs {
		ListFieldVerbs[i] = ""
	}
	for i := range TextListVerbs {
		TextListVerbs[i] = ""
	}

	for name := range forms {
		if !IsTableForm(name) {
			t.Errorf("IsTableForm(%q) followed an edit of ir.TableForms", name)
		}
	}
	st := &Struct{Forms: []string{"wire", "json"}}
	if !st.CarriesForm("json") {
		t.Error("CarriesForm(json) followed an edit of ir.TableForms")
	}
	for name := range checks {
		if !IsTableIntegrityCheck(name) {
			t.Errorf("IsTableIntegrityCheck(%q) followed an edit of ir.TableIntegrityChecks", name)
		}
	}
	if f, ok := WireFormOf(3); !ok || f != registry[3] {
		t.Errorf("WireFormOf(3) = %+v, %t after an edit of ir.WireForms; want %+v", f, ok, registry[3])
	}
	if got, want := WireFormLine([]byte{3}), "FORM 3 the fixed form (docs/SPEC-TABLES.md §3.4)"; got != want {
		t.Errorf("WireFormLine followed an edit of ir.WireForms: %q, want %q", got, want)
	}
	for _, c := range []struct {
		name string
		all  []string
		want []string
	}{
		{"AllMapFieldVerbs", slices.Collect(AllMapFieldVerbs()), mapVerbs},
		{"AllListFieldVerbs", slices.Collect(AllListFieldVerbs()), listVerbs},
		{"AllTextListVerbs", slices.Collect(AllTextListVerbs()), textVerbs},
	} {
		if !slices.Equal(c.all, c.want) {
			t.Errorf("%s followed an edit of its exported copy: %q, want %q", c.name, c.all, c.want)
		}
	}
}
