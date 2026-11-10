package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// These registered-construct prerequisites are independent of what a port
// elects to claim. They are not exhaustive specification or native runtime
// acceptance: removing cases cannot make these prerequisites pass.
func originalImplementationProblems(m *Manifest) []string {
	return implementationProblems(m, originalNine, compiler.CarryConstructs(), compiler.Carried, newUnits(m), compiler.New())
}

type implementationGenerator interface {
	Generate(*ir.Unit, string, compiler.Options) (map[string][]byte, error)
}

func implementationProblems(m *Manifest, langs, required []string, carried func(string) ([]string, bool), units unitSource, generator implementationGenerator) []string {
	problems, _ := implementationAudit(m, langs, required, carried, units, generator)
	return problems
}

func implementationAudit(m *Manifest, langs, required []string, carried func(string) ([]string, bool), units unitSource, generator implementationGenerator) ([]string, int) {
	var problems []string
	if len(required) == 0 {
		return []string{"implementation: the required construct registry is empty"}, 0
	}
	for _, lang := range langs {
		have, ok := carried(lang)
		if !ok {
			problems = append(problems, lang+": implementation registration is missing")
		}
		for _, feature := range required {
			if !slices.Contains(have, feature) {
				problems = append(problems, fmt.Sprintf("%s: required Schema construct %s is not implemented", lang, feature))
			}
		}
	}
	// Only positive wire instances count. A feature in an unrelated table, a
	// refusal fixture, or an unused unit declaration is not a runtime witness.
	witnesses := map[string][]string{}
	qualified := map[string]bool{}
	generated := map[string]bool{}
	for _, inst := range m.Instances {
		if inst.WireRefused {
			continue
		}
		u, err := units.get(inst.Unit)
		if err != nil {
			problems = append(problems, fmt.Sprintf("implementation witness %s: %v", inst.Name, err))
			continue
		}
		rooted, err := implementationRoot(u, inst.Root)
		if err != nil {
			problems = append(problems, fmt.Sprintf("implementation witness %s: %v", inst.Name, err))
			continue
		}
		uses := compiler.UnitConstructs(rooted)
		// Every positive unit is required, including ordinary schema shapes
		// outside the special construct registry. Generate each unit once per
		// language; duplicate instances do not add compiler work.
		model, value, jsonVerified, err := decodeImplementationWitness(u, inst)
		if err != nil {
			problems = append(problems, fmt.Sprintf("implementation witness %s: %v", inst.Name, err))
			continue
		}
		active := activeImplementationConstructs(model, value, jsonVerified)
		for _, feature := range uses {
			if active[feature] {
				witnesses[feature] = append(witnesses[feature], inst.Name)
				qualified[inst.Name] = true
			}
		}
		for _, lang := range langs {
			key := lang + "/" + inst.Unit
			if generated[key] {
				continue
			}
			generated[key] = true
			files, err := generator.Generate(u, lang, compiler.Options{"runtime": "standalone"})
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: required feature witness unit %s cannot generate: %v", lang, inst.Unit, err))
			} else if len(files) == 0 {
				problems = append(problems, fmt.Sprintf("%s: required feature witness unit %s generated no files", lang, inst.Unit))
			}
		}
	}
	for _, feature := range required {
		if len(witnesses[feature]) == 0 {
			problems = append(problems, "implementation corpus: required Schema construct "+feature+" has no oracle-qualified active instance (not merely a reachable declaration)")
		}
	}
	sort.Strings(problems)
	return problems, len(qualified)
}

// Decode each declared positive input once, independently of the number of
// ports. This is not a second reference read of every writer's output.
// Missing/damaged/noncanonical files and unrelated JSON cannot certify a feature.
func decodeImplementationWitness(u *ir.Unit, inst Instance) (*tabletext.Model, *tabletext.Instance, bool, error) {
	model := tabletext.NewModel(u)
	root := u.Tables[inst.Root]
	if root == nil {
		return nil, nil, false, fmt.Errorf("no table root %q", inst.Root)
	}
	wire, err := os.ReadFile(inst.Wire)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read positive wire: %w", err)
	}
	if !root.CarriesForm("wire") {
		return nil, nil, false, fmt.Errorf("positive wire uses a form excluded by its root")
	}
	if root.Integrity != "" && root.Integrity != "crc32c" {
		return nil, nil, false, fmt.Errorf("integrity %q has no qualified witness oracle", root.Integrity)
	}
	value := model.New(root)
	var report tabletext.Report
	ok, err := strictDecode(model, value, wire, &report)
	_ = err
	if !ok {
		return nil, nil, false, fmt.Errorf("positive wire does not decode cleanly: report=%+v", report)
	}
	again, err := encodeImplementationWitness(model, value)
	if err != nil {
		return nil, nil, false, fmt.Errorf("positive wire is not canonical for its declared root: %w", err)
	}
	if !bytes.Equal(again, wire) {
		return nil, nil, false, fmt.Errorf("positive wire is not canonical for its declared root")
	}
	if inst.NoText {
		return model, value, false, nil
	}
	if !root.CarriesForm("json") {
		return nil, nil, false, fmt.Errorf("positive JSON uses a form excluded by its root")
	}
	text, err := os.ReadFile(inst.JSON)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read positive JSON: %w", err)
	}
	fromText := model.New(root)
	report = tabletext.Report{}
	if !strictReadJSON(model, fromText, text, &report) {
		return nil, nil, false, fmt.Errorf("positive JSON does not decode cleanly: %+v", report)
	}
	again, err = encodeImplementationWitness(model, fromText)
	if err != nil {
		return nil, nil, false, fmt.Errorf("positive JSON does not reproduce its declared wire: %w", err)
	}
	if !bytes.Equal(again, wire) {
		return nil, nil, false, fmt.Errorf("positive JSON does not reproduce its declared wire")
	}
	return model, value, true, nil
}

// Integrity is handled by the same neutral file codec used by pack/unpack.
// There is deliberately no second wrapper which could append or strip twice.
func encodeImplementationWitness(model *tabletext.Model, value *tabletext.Instance) ([]byte, error) {
	return tablewire.Encode(model, value)
}

// Walk actual stored values, not every possible declaration edge. Nil pointers,
// absent optionals, unselected arms, default scalars, empty collections and
// unused counted-array capacity do not manufacture coverage.
func activeImplementationConstructs(model *tabletext.Model, root *tabletext.Instance, jsonVerified bool) map[string]bool {
	active := map[string]bool{}
	populated := false
	seen := map[*tabletext.Instance]bool{}
	hasNode := map[*tabletext.Instance]bool{}
	var walkInstance func(*tabletext.Instance) bool
	var walkField func(*tabletext.Field) bool
	walkCell := func(cell *tabletext.Cell, f *ir.Field) bool {
		def := tabletext.Cell{}
		if f.Array == ir.ArrayNone {
			def = model.FieldDefaultCell(f)
		}
		if cell.I != def.I || cell.U != def.U || cell.B != def.B || cell.F != def.F || !bytes.Equal(cell.Str, def.Str) {
			populated = true
		}
		if cell.Wide != nil && (def.Wide == nil || cell.Wide.Cmp(def.Wide) != 0) || cell.Wide == nil && def.Wide != nil && def.Wide.Sign() != 0 {
			populated = true
		}

		// Array elements have zero defaults; a scalar uses its declaration's
		// default. Nondefault content must survive the actual wire decode.
		if f.Type.Kind == ir.TInt && !f.Type.Pointer {
			def := tabletext.Cell{}
			if f.Array == ir.ArrayNone {
				def = model.FieldDefaultCell(f)
			}
			different := cell.I != def.I || cell.U != def.U
			if cell.Wide != nil {
				different = def.Wide == nil || cell.Wide.Cmp(def.Wide) != 0
			} else if def.Wide != nil {
				different = def.Wide.Sign() != 0
			}
			if different {
				if f.Type.Timestamp {
					active["timestamp"] = true
				}
				if f.RangeOneSided {
					active["one-sided-range"] = true
				}
			}
		}
		if f.Type.Pointer {
			if cell.Node == nil {
				return false
			}
			populated = true
			walkInstance(cell.Node)
			return true
		}
		if un := tabletext.UnionOf(f); un != nil {
			if cell.U == 0 {
				return false
			}
			if cell.Arm != nil {
				return walkField(cell.Arm)
			}
			return walkInstance(cell.Tab)
		}
		return walkInstance(cell.Tab)
	}
	walkField = func(fv *tabletext.Field) bool {
		f := fv.Def
		if f.Deprecated || (f.Type.Optional && !fv.Present) {
			return false
		}
		node := false
		switch {
		case f.IsMap():
			if len(fv.Entries) > 0 {
				populated = true
			}
			if f.Type.Optional && len(fv.Entries) > 0 {
				active["optional-map"] = true
			}
			for i := range fv.Entries {
				entry := fv.Entries[i].Tab
				if ir.MapKeyUnbounded(f) && entry != nil && len(entry.Fields) > 0 && len(entry.Fields[0].Cell.Str) > 0 {
					active["unbounded-text"] = true
				}
				node = walkInstance(entry) || node
			}
		case f.Array != ir.ArrayNone:
			count := len(fv.Elems)
			if f.CountedOnWire() {
				count = fv.Count
			}
			start := 0
			if f.KeyEnum != "" {
				start = tabletext.KeyedFirstSlot()
				count = tabletext.KeyedSlotCount(f)
			}
			if count > start {
				populated = true
			}
			for i := start; i < count && i < len(fv.Elems); i++ {
				cell := &fv.Elems[i]
				if f.IsTextList() && cell.Tab != nil && len(cell.Tab.Fields) > 0 {
					text := &cell.Tab.Fields[0]
					if (!text.Def.Type.Optional || text.Present) && len(text.Cell.Str) > 0 {
						active["text-list"] = true
						if f.TextTotal > 0 {
							active["text-list-total"] = true
						}
						if ir.TextListUnbounded(f) {
							active["unbounded-text"] = true
						}
					}
				}
				node = walkCell(cell, f) || node
			}
		default:
			// A required positive witness carries actual nondefault scalar
			// content. Missing-required refusal remains a native obligation.
			if f.Required {
				def := model.FieldDefaultCell(f)
				if fv.Cell.I != def.I || fv.Cell.U != def.U || fv.Cell.B != def.B || fv.Cell.F != def.F || !bytes.Equal(fv.Cell.Str, def.Str) {
					active["required"] = true
				}
			}
			node = walkCell(&fv.Cell, f)
		}
		if f.Type.Optional && (f.Array == ir.ArrayNone || f.Array == ir.ArrayFixed) {
			if _, ok := f.Type.Ref.(*ir.Struct); ok && node {
				active["optional-variable-closure"] = true
			}
		}
		return node
	}
	walkInstance = func(value *tabletext.Instance) bool {
		if value == nil {
			return false
		}
		if seen[value] {
			return hasNode[value]
		}
		seen[value] = true
		guards := tabletext.Guards(value.Def)
		node := false
		for i := range value.Fields {
			fv := &value.Fields[i]
			if !value.GuardHolds(guards[fv.Def.Name]) {
				continue
			}
			node = walkField(fv) || node
		}
		hasNode[value] = node
		return node
	}
	walkInstance(root)
	// Forms and the JSON root mode are root-entry properties. A nested
	// declaration cannot exercise a root's alternate entry shape.
	if populated && root.Def.DeclaredForms() && root.Def.CarriesForm("wire") {
		active["forms"] = true
	}
	if root.Def.JsonArray && jsonVerified {
		for _, fv := range root.Fields {
			if fv.Def.Array != ir.ArrayNone && fv.Count > 0 {
				active["array-root"] = true
			}
		}
	}
	if !jsonVerified {
		for _, feature := range []string{"timestamp", "text-list", "text-list-total", "unbounded-text", "array-root"} {
			delete(active, feature)
		}
	}
	// This is reached only after the root's checksum was checked and the
	// canonical body was resealed byte-identically. Nested annotations alone
	// cannot exercise a file-level integrity declaration.
	if populated && root.Def.Integrity == "crc32c" {
		active["integrity"] = true
	}
	return active
}

// Restrict the feature query to declarations actually reachable from this
// case's root. References are walked once, including cycles, map entries,
// pointer/array elements and union payloads. The compiler IR is never mutated.
func implementationRoot(u *ir.Unit, name string) (*ir.Unit, error) {
	root := u.Tables[name]
	if root == nil {
		return nil, fmt.Errorf("no table root %q", name)
	}
	reached := map[*ir.Struct]bool{}
	unions := map[*ir.Union]bool{}
	var walkStruct func(*ir.Struct)
	var walkUnion func(*ir.Union)
	walkField := func(f *ir.Field) {
		if f == nil {
			return
		}
		if f.IsMap() {
			walkStruct(f.MapEntry)
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			walkStruct(ref)
		case *ir.Union:
			walkUnion(ref)
		}
	}
	walkUnion = func(un *ir.Union) {
		if un == nil || unions[un] {
			return
		}
		unions[un] = true
		for _, arm := range un.Variants {
			walkField(arm.F)
		}
	}
	walkStruct = func(st *ir.Struct) {
		if st == nil || reached[st] {
			return
		}
		reached[st] = true
		for _, f := range st.Fields {
			walkField(f)
		}
	}
	walkStruct(root)
	out := *u
	out.Tables = map[string]*ir.Struct{}
	for tableName, st := range u.Tables {
		if reached[st] {
			out.Tables[tableName] = st
		}
	}
	// Some IR predicates walk ordered declarations rather than the lookup
	// maps. Filter those too, or a timestamp in an unrelated table would
	// falsely count as exercised by this root.
	out.Files = nil
	for _, file := range u.Files {
		copyFile := *file
		copyFile.Tables = nil
		for _, st := range file.Tables {
			if reached[st] {
				copyFile.Tables = append(copyFile.Tables, st)
			}
		}
		out.Files = append(out.Files, &copyFile)
	}
	return &out, nil
}

func implementationReport(w io.Writer, m *Manifest, lang string) error {
	langs := originalNine
	if lang != "" {
		if !admittedImplementationLanguage(lang) {
			return fmt.Errorf("implementation: %q is not an original-nine language", lang)
		}
		langs = []string{lang}
	}
	problems, witnesses := implementationAudit(m, langs, compiler.CarryConstructs(), compiler.Carried, newUnits(m), compiler.New())
	if _, err := fmt.Fprintln(w, "Required registered constructs and oracle-qualified active corpus inputs"); err != nil {
		return err
	}
	for _, problem := range problems {
		if _, err := fmt.Fprintln(w, "FAIL:", problem); err != nil {
			return err
		}
	}
	if len(problems) != 0 {
		return fmt.Errorf("full implementation prerequisites are incomplete (%d findings)", len(problems))
	}
	for _, checked := range langs {
		if _, err := fmt.Fprintf(w, "IMPLEMENTATION_PREREQUISITES_PASS language=%s constructs=%d witnesses=%d\n", checked, len(compiler.CarryConstructs()), witnesses); err != nil {
			return err
		}
	}
	return nil
}
