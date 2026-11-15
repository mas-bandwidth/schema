package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// framingNonCanonicalTables are the vectors whose PURPOSE is a non-canonical id
// table, so the one defect they carry is the one this check looks for.
var framingNonCanonicalTables = map[string]string{
	// F7: the vector IS an entry no body references.
	"frame_unreferenced_entry": "F7: an entry no body references is the vector",
}

// TestFramingVectorIdTablesAreCanonical holds every pinned framing vector's id
// table, the no-writer vectors' included, to FIRST-USE ORDER
// (docs/SPEC-TABLES.md §3): the table carries the ids its body references, each
// in the slot of its first use, root body first and then the node table's
// records in index order, and nothing the body never names. A reader refuses
// any other table (§4, docs/rulings.json reads-refuse-non-canonical), so a
// vector built to exercise some other rule and carrying a stray or misplaced
// entry would be refused for the wrong reason. The references are located by
// the wire fuzzer's framing locator; a reference above the entry count names no
// entry, and a vector whose entry count is itself the defect has no table to
// judge.
func TestFramingVectorIdTablesAreCanonical(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	vectors := append(append([]framingVector{}, framingVectors...), noWriterVectors...)
	for _, v := range vectors {
		if _, exempt := framingNonCanonicalTables[v.name]; exempt {
			continue
		}
		if v.count != nil {
			continue // the entry count is the defect: the table does not frame
		}
		data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
		if err != nil {
			t.Errorf("%s: %v", v.name, err)
			continue
		}
		if fault := framingTableFault(v, data); fault != "" {
			t.Errorf("%s: %s", v.name, fault)
		}
	}
	for name := range framingNonCanonicalTables {
		found := false
		for _, v := range vectors {
			found = found || v.name == name
		}
		if !found {
			t.Errorf("%s: framingNonCanonicalTables names no vector", name)
		}
	}
	logFramingSpellings(t, root, vectors)
}

// framingTableFault is "" when the file's id table is the body's references in
// first-use order, and otherwise says which slots break it.
func framingTableFault(v framingVector, data []byte) string {
	f := frameWire(data)
	n := uint64(len(f.entries))
	if n == 0 {
		return ""
	}
	slot := func(ref uint64) string {
		if v.entries == nil && int(ref) <= len(v.ids) {
			return fmt.Sprintf("%d (%s)", ref, v.ids[ref-1])
		}
		return fmt.Sprintf("%d", ref)
	}
	used := map[uint64]bool{}
	next := uint64(1)
	var faults []string
	for _, sp := range f.spots {
		switch sp.kind {
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
		default:
			continue
		}
		if sp.value == 0 || sp.value > n || used[sp.value] {
			continue // names no entry, or is not a first use
		}
		used[sp.value] = true
		if sp.value != next && len(faults) == 0 {
			faults = append(faults, fmt.Sprintf("byte %d first uses slot %s where first-use order expects slot %s", sp.off, slot(sp.value), slot(next)))
		}
		next++
	}
	var unused []string
	for ref := uint64(1); ref <= n; ref++ {
		if !used[ref] {
			unused = append(unused, slot(ref))
		}
	}
	if len(unused) > 0 {
		faults = append(faults, fmt.Sprintf("unused slots %v", unused))
	}
	if len(faults) == 0 {
		return ""
	}
	return fmt.Sprint(faults)
}

// THE OTHER NON-CANONICAL SPELLINGS (docs/SPEC-TABLES.md §4: a value a writer
// would leave out, or fields in another order than the writer's) are REPORTED
// here, never failed: a strict reader refuses each of them, so a vector meant to
// test some other rule and carrying one is refused for the wrong reason, and
// the list is what a vector's repair is cut from. The id table is judged
// above; this pass names the rest it can find cheaply:
//
//   - on the wire alone, a declared field stored as union arm 0, as a null
//     pointer, or as an empty array, map or keyed body;
//   - against the engine's own write-back of the value it reads (the writer
//     leaves out what is at its default and writes declaration order), a
//     declared field the write-back leaves out ("stored at its default"), and
//     the declared fields of one body in another order than the write-back's.
//
// A vector the engine cannot read at all is judged on the wire alone.
// framingSpellingsMaxBytes keeps the pass cheap: the one file past it is the
// 200000-record chain, a depth vector whose records are all one spelling.
const framingSpellingsMaxBytes = 1 << 18

func logFramingSpellings(t *testing.T, root string, vectors []framingVector) {
	t.Helper()
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	verdicts := map[string]string{}
	for _, fc := range m.Framings {
		verdicts[fc.Name] = fc.Verdict()
	}
	u := newUnits(m)
	found := 0
	for _, v := range vectors {
		if v.unit == "" || v.root == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, framingDir, v.name+".bin"))
		if err != nil {
			continue // the id-table pass above already says so
		}
		unit, err := u.get(v.unit)
		if err != nil {
			continue
		}
		if st := unit.Tables[v.root]; st != nil && st.FixedDeclared {
			if unit, err = framingVariableUnit(m, v.unit, v.root); err != nil {
				continue
			}
		}
		if len(data) > framingSpellingsMaxBytes {
			t.Logf("SPELLING %s: not judged, %d bytes is past this cheap pass's %d", v.name, len(data), framingSpellingsMaxBytes)
			continue
		}
		verdict, ok := verdicts[v.name]
		if !ok {
			verdict = "no manifest row (held)"
		}
		for _, s := range framingSpellings(unit, v.root, data, framingNonCanonicalTables[v.name] != "") {
			found++
			t.Logf("SPELLING %s: %s (manifest verdict today: %s)", v.name, s, verdict)
		}
	}
	t.Logf("SPELLING: %d non-canonical spellings beyond the id table", found)
}

// framingSpellings names each non-canonical spelling of one file it can find.
func framingSpellings(unit *ir.Unit, root string, data []byte, exempt bool) []string {
	declared := map[uint64]bool{}
	for _, st := range unit.Tables {
		for _, fd := range st.Fields {
			declared[ir.TableFieldWireId(fd)] = true
		}
	}
	for _, unions := range []map[string]*ir.Union{unit.Unions, unit.TableUnions} {
		for _, un := range unions {
			for _, arm := range un.Variants {
				if arm.F != nil {
					declared[ir.TableFieldWireId(arm.F)] = true
				}
			}
		}
	}
	f := frameWire(data)
	var out []string
	for i, sp := range f.spots {
		if sp.kind != spotRef || sp.value == 0 || i+2 >= len(f.spots) {
			continue
		}
		id, named := f.entryOf(sp.value)
		if !named || !declared[id] {
			continue
		}
		k := f.spots[i+1]
		if k.kind != spotKind || k.role != roleField {
			continue
		}
		next := f.spots[i+2]
		switch {
		case k.value == ir.TableKindUnion && next.kind == spotArm && next.value == 0:
			out = append(out, fmt.Sprintf("byte %d: union arm 0 stored in a field (slot %d)", sp.off, sp.value))
		case k.value == ir.TableKindPointer && next.kind == spotIndex && next.value == 0:
			out = append(out, fmt.Sprintf("byte %d: an explicit null pointer stored (slot %d)", sp.off, sp.value))
		case (k.value == ir.TableKindArray || k.value == ir.TableKindKeyed) && i+4 < len(f.spots) &&
			f.spots[i+4].kind == spotCount && f.spots[i+4].value == 0:
			out = append(out, fmt.Sprintf("byte %d: an empty array stored (slot %d, kind %d)", sp.off, sp.value, k.value))
		}
	}

	model := tabletext.NewModel(unit)
	def := model.Lookup(root)
	if def == nil {
		return out
	}
	// THE ROOT BODY'S ORDER is judged against the declaration itself, so a
	// field the write-back leaves out (an unknown arm, say) still counts
	place := map[uint64]int{}
	for i, fd := range def.Fields {
		if _, dup := place[ir.TableFieldWireId(fd)]; !dup {
			place[ir.TableFieldWireId(fd)] = i
		}
	}
	rootIds, _, _ := bodyFieldIds(f, declared)
	last := -1
	for _, id := range rootIds[""] {
		at, mine := place[id]
		if !mine {
			continue
		}
		if at < last {
			out = append(out, "fields out of declaration order (body root)")
			break
		}
		last = at
	}
	inst := model.New(def)
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, inst, data, &report); err != nil || !ok || report.Refused {
		return out
	}
	back, err := tablewire.Encode(model, inst)
	if err != nil {
		return out
	}
	g := frameWire(back)
	have, haveAt, _ := bodyFieldIds(f, declared)
	want, _, bodies := bodyFieldIds(g, declared)
	// NODE ORDER IS NOT ONE OF §4's SHAPES, and the write-back may number the
	// records another way: the records' bodies are compared index by index
	// only where the write-back keeps the file's order, and otherwise only as
	// a whole (a record losing a field is still found, but not located).
	inRecord := func(path string) bool { return strings.Contains("/"+path, "/r.") }
	judgeRecords := true
	if fs, gs := recordSigs(have, bodiesOf(f, declared)), recordSigs(want, bodies); !slices.Equal(fs, gs) {
		judgeRecords = false
		slices.Sort(fs)
		slices.Sort(gs)
		// a record of a type this reader cannot name reads null and leaves
		// the write-back with its fields, which is no spelling
		if report.Unknown == 0 && !slices.Equal(fs, gs) {
			out = append(out, "a node record stores a field the write-back leaves out (the records are renumbered, so it is not located)")
		}
	}
	var paths []string
	for path := range have {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !judgeRecords && inRecord(path) {
			continue
		}
		w := want[path]
		if path != "" && !bodies[path] {
			continue // the whole body is left out: its own field is named above it
		}
		in := map[uint64]bool{}
		for _, id := range w {
			in[id] = true
		}
		seen := map[uint64]bool{}
		var kept []uint64
		for j, id := range have[path] {
			if seen[id] {
				continue // a repeated id is its own (framing) defect
			}
			seen[id] = true
			if !in[id] && !carriesUnknown(f, haveAt[path][j], declared) {
				out = append(out, fmt.Sprintf("byte %d: a field stored at its default (body %s)", haveAt[path][j], bodyName(path)))
				continue
			}
			if in[id] {
				kept = append(kept, id)
			}
		}
		var order []uint64
		for _, id := range w {
			if seen[id] {
				order = append(order, id)
			}
		}
		if path != "" && !slices.Equal(kept, order) {
			out = append(out, fmt.Sprintf("fields out of declaration order (body %s)", bodyName(path)))
		}
	}
	if !exempt && judgeRecords && len(out) == 0 && report.Unknown == 0 && report.Retained == 0 && !bytes.Equal(back, data) {
		out = append(out, "the engine's write-back differs from the file, by no spelling named here")
	}
	return out
}

func bodyName(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

// bodyFieldIds is, per body, the declared field ids its references name in
// wire order, and the byte each reference sits at. A body is named by the path
// of lengths enclosing it: the field, arm, keyed slot or node record each
// length frames, and its place among siblings of the same name.
func bodyFieldIds(f *wireFrame, declared map[uint64]bool) (map[string][]uint64, map[string][]int, map[string]bool) {
	bodies := map[string]bool{}
	labels := map[int]string{}
	seen := map[string]int{}
	ids := map[string][]uint64{}
	at := map[string][]int{}
	pathOf := func(chain []int) string {
		var parts []string
		for _, j := range chain {
			parts = append(parts, labels[j])
		}
		return strings.Join(parts, "/")
	}
	for j, sp := range f.spots {
		switch sp.kind {
		case spotLength:
			label := "e"
			if j >= 2 && f.spots[j-1].kind == spotKind && f.spots[j-1].role == roleField && f.spots[j-2].kind == spotRef {
				id, _ := f.entryOf(f.spots[j-2].value)
				label = fmt.Sprintf("f%x", id)
			} else if j >= 1 && f.spots[j-1].kind == spotRecordType {
				label = "r"
			} else if j >= 1 && f.spots[j-1].kind == spotKey {
				id, _ := f.entryOf(f.spots[j-1].value)
				label = fmt.Sprintf("k%x", id)
			} else if j >= 2 && f.spots[j-1].kind == spotKind && f.spots[j-2].kind == spotArm {
				id, _ := f.entryOf(f.spots[j-2].value)
				label = fmt.Sprintf("a%x", id)
			}
			key := pathOf(sp.enclosing) + "/" + label
			labels[j] = fmt.Sprintf("%s.%d", label, seen[key])
			seen[key]++
			bodies[pathOf(append(append([]int(nil), sp.enclosing...), j))] = true
		case spotRef:
			id, named := f.entryOf(sp.value)
			if !named || !declared[id] {
				continue
			}
			path := pathOf(sp.enclosing)
			ids[path] = append(ids[path], id)
			at[path] = append(at[path], sp.off)
		}
	}
	return ids, at, bodies
}

// carriesUnknown is true when the field whose reference sits at off names an
// id or arm the unit does not declare anywhere inside it: such a field reads
// as unknown (or its arm as the declared default), which is its purpose, not a
// value stored at its default.
func carriesUnknown(f *wireFrame, off int, declared map[uint64]bool) bool {
	end := -1
	for _, fd := range f.fields {
		if fd.start == off {
			end = fd.end
			break
		}
	}
	for _, sp := range f.spots {
		if sp.off < off || sp.off >= end || (sp.kind != spotRef && sp.kind != spotArm) || sp.value == 0 {
			continue
		}
		if id, named := f.entryOf(sp.value); !named || !declared[id] {
			return true
		}
	}
	return false
}

func bodiesOf(f *wireFrame, declared map[uint64]bool) map[string]bool {
	_, _, bodies := bodyFieldIds(f, declared)
	return bodies
}

// recordSigs is, for each node record body in index order, the declared field
// ids it stores directly.
func recordSigs(ids map[string][]uint64, bodies map[string]bool) []string {
	var paths []string
	for path := range bodies {
		last := path[strings.LastIndex(path, "/")+1:]
		if strings.HasPrefix(last, "r.") {
			paths = append(paths, path)
		}
	}
	slices.SortFunc(paths, func(a, b string) int {
		var x, y int
		fmt.Sscanf(a[strings.LastIndex(a, "r.")+2:], "%d", &x)
		fmt.Sscanf(b[strings.LastIndex(b, "r.")+2:], "%d", &y)
		if c := strings.Compare(a[:strings.LastIndex(a, "r.")], b[:strings.LastIndex(b, "r.")]); c != 0 {
			return c
		}
		return x - y
	})
	var out []string
	for _, path := range paths {
		got := slices.Clone(ids[path])
		slices.Sort(got)
		out = append(out, fmt.Sprint(got))
	}
	return out
}
