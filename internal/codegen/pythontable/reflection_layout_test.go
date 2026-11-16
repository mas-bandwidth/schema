package pythontable

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const reflectionLayoutProbe = pyrun.ImportPrelude + `import importlib, struct
pkg = importlib.import_module(PACKAGE)
v = pkg.UnitView()
assert pkg.UnitView() is v
for row in v.types + v.tables:
    assert row.doc == row.type.doc and row.tags == row.type.tags, 'type annotation drift: ' + row.name
for union in v.unions:
    for arm in union.variants:
        if arm.field is not None:
            assert arm.doc == arm.field.doc and arm.tags == arm.field.tags, 'arm annotation drift: ' + union.name + '.' + arm.name
rows = [(d.name, d.type) for d in v.types + v.tables]
entries = {}
declared = {name for name, _ in rows}
pending = [x for _, x in rows]
while pending:
    current = pending.pop()
    for f in current.fields:
        target = f.target_desc
        if target is not None and target.name not in declared and target.name not in entries:
            entries[target.name] = target
            pending.append(target)
rows.extend(sorted(entries.items()))
for name, x in rows:
    padding = ','.join('%d:%d' % p for p in x.padding)
    print('record %s %d %d %s %d %d %d %d %d %d %s' % (name, x.size, x.align, str(x.variable).lower(), x.cook_align, x.extent_start, x.region_align, x.build_version, x.bytes_blob_type_id, x.string_blob_type_id, padding))
    for f in x.fields:
        pieces = ','.join('%d:%d' % p for p in f.pieces)
        print('field %s.%s %d %d %d %d %d %d %d %s %d %d %s %s %d %d %s %s' % (name, f.name, f.kind, f.elem_kind, f.at, f.elem_size, f.elem_align, f.array_kind, f.size, str(f.counted).lower(), f.count_offset, f.present_offset, str(f.unbounded_container).lower(), str(f.unbounded).lower(), f.array_bound, f.frac_bits, str(f.target_desc is not None).lower(), pieces))
        wide = '-' if f.wide is None else '%d,%d' % f.wide
        ids = ','.join('%016x' % x for x in f.variant_ids)
        keys = ','.join('%016x' % x for x in f.keys)
        retired = ','.join(str(x) for x in sorted(f.deprecated))
        fmin = struct.unpack('<Q', struct.pack('<d', f.fmin))[0]
        fmax = struct.unpack('<Q', struct.pack('<d', f.fmax))[0]
        print('meta %s.%s guard=%s required=%s retired=%s wide=%s ids=%s keys=%s variants_deprecated=%s f32=%s frange=%s min=%016x max=%016x vocab=%s vmax=%d names=%s key_names=%s' % (name, f.name, f.guard.encode('utf-8').hex(), str(f.required).lower(), str(f.field_deprecated).lower(), wide, ids, keys, retired, str(f.f32).lower(), str(f.float_range).lower(), fmin, fmax, f.vocabulary_kind, f.enum_max, ','.join(f.variant_names), ','.join(f.key_names)))
        if f.vocabulary_kind == 'union':
            un = f.union_desc
            assert un is not None and un.name == f.target, 'union descriptor missing: ' + name + '.' + f.name
            assert tuple(a.name for a in un.arms) == f.variant_names
            assert tuple(a.arm_id for a in un.arms) == f.variant_ids
            print('union %s.%s %d %d %d %d' % (name, f.name, un.size, un.tag_size, un.arm_offset, len(un.arms)))
            for a in un.arms:
                print('arm %s.%s %s %d %s' % (name, f.name, a.name, a.size, str(a.target_desc is not None).lower()))
`

// reflectionLayoutWant independently derives the view's storage metadata from
// the checked IR, rather than reading the generated descriptor expressions.
func reflectionLayoutWant(u *ir.Unit) string {
	var types, tables []*ir.Struct
	for _, file := range u.Files {
		for _, d := range file.Decls {
			if s, ok := d.(*ir.Struct); ok && !s.IsTable {
				types = append(types, s)
			}
		}
		for _, s := range file.Tables {
			if !s.IsMapEntry() {
				tables = append(tables, s)
			}
		}
	}
	cmp := func(a, b *ir.Struct) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(types, cmp)
	slices.SortFunc(tables, cmp)
	records := append(types, tables...)
	var entries []*ir.Struct
	for _, st := range u.Tables {
		if st.IsMapEntry() {
			entries = append(entries, st)
		}
	}
	slices.SortFunc(entries, cmp)
	records = append(records, entries...)
	var b strings.Builder
	for _, st := range records {
		layout := ir.RecordLayout(u, st)
		var padding []string
		end := int64(0)
		for _, field := range layout.Fields {
			if field.Offset > end {
				padding = append(padding, fmt.Sprintf("%d:%d", end, field.Offset))
			}
			end = field.Offset + field.Size
		}
		if end < layout.Size {
			padding = append(padding, fmt.Sprintf("%d:%d", end, layout.Size))
		}
		fmt.Fprintf(&b, "record %s %d %d %v %d %d %d %d %d %d %s\n", st.Name, layout.Size, layout.Align, ir.VariableTables(u)[st.Name], ir.CookNodeAlign(u, st), ir.CookExtentStart(u, st), ir.TableRegionAlign(u), ir.BuildVersion(u), ir.BytesTypeId, ir.StringTypeId, strings.Join(padding, ","))
		for _, f := range st.Fields {
			fl := layout.FieldByName(f.Name)
			if fl == nil {
				panic("missing field layout")
			}
			elem, _ := ir.ListElementLayout(u, f)
			switch {
			case f.IsMap():
				elem = ir.RecordLayout(u, f.MapEntry).Size
			case f.Array != ir.ArrayNone:
			case f.Type.Pointer:
				elem = 8
			case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
				elem = 1
			}

			count := f.IsList() || f.IsMap() || f.Array == ir.ArrayCounted || (!f.Type.Pointer && !f.Type.TextUnbounded() && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes))
			bound := int64(0)
			switch {
			case f.IsMap() || f.IsList():
			case f.KeyEnumRef != nil:
				bound = f.KeyEnumRef.Max
			case f.Array != ir.ArrayNone:
				bound = f.ArrayBound
			case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
				bound = f.Type.Size
			}
			countAt, presentAt := int64(-1), int64(-1)
			pieces := ir.FieldPieces(u, f, fl.Offset)
			if count {
				if f.IsMap() || f.IsList() {
					countAt = fl.Offset + 8
				} else if len(pieces) > 1 {
					countAt = pieces[1].Offset
				}
			}
			if f.Type.Optional && len(pieces) > 1 {
				presentAt = pieces[len(pieces)-1].Offset
			}
			size := int64(0)
			if f.IsTextList() {
				size = ir.TextListValue(f).Type.Size
			} else if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
				size = f.Type.Size
			}
			_, isStruct := f.Type.Ref.(*ir.Struct)
			hasTarget := isStruct || f.IsMap()
			_, elemAlign := ir.ListElementLayout(u, f)
			if f.IsMap() {
				elemAlign = ir.RecordLayout(u, f.MapEntry).Align
			} else if f.Type.Pointer && f.Array == ir.ArrayNone {
				elemAlign = 8
			} else if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
				elemAlign = 1
			}
			var partText []string
			for _, p := range pieces {
				partText = append(partText, fmt.Sprintf("%d:%d", p.Offset, p.Size))
			}
			fmt.Fprintf(&b, "field %s.%s %d %d %d %d %d %d %d %v %d %d %v %v %d %d %v %s\n", st.Name, f.Name, ir.TableWireFieldKind(f), ir.TableWireElemKind(f), fl.Offset, elem, elemAlign, f.Array, size, count, countAt, presentAt, f.IsList() || f.IsMap(), f.Type.TextUnbounded(), bound, f.Type.FracBits, hasTarget, strings.Join(partText, ","))
			wide := "-"
			if ir.TableKindWide(ir.TableWireScalarKind(f)) {
				if lo, hi, ok := ir.TableRawRange(f); ok {
					wide = lo.String() + "," + hi.String()
				}
			}
			var ids, keys, retired []string
			reached := ir.TableClosureVocabulary(u)
			if enum, ok := f.Type.Ref.(*ir.Enum); ok {
				for i := range enum.Variants {
					id := uint64(0)
					if reached[enum.Name] {
						id = ir.TableWireId(enum.VariantWireName(i))
					}
					ids = append(ids, fmt.Sprintf("%016x", id))
					if i < len(enum.VariantTags) && slices.Contains(enum.VariantTags[i], "deprecated") {
						retired = append(retired, strconv.Itoa(i+1))
					}
				}
			}
			vocabKind := ""
			vocabMax := int64(0)
			var vocabNames []string
			switch v := f.Type.Ref.(type) {
			case *ir.Enum:
				vocabKind = "enum"
				vocabMax = v.Max
				vocabNames = v.Variants
			case *ir.Flags:
				vocabKind = "flags"
				vocabMax = int64(len(v.Variants) - 1)
				vocabNames = v.Variants
			case *ir.Union:
				vocabKind = "union"
				vocabMax = v.Max
				for _, arm := range v.Variants {
					vocabNames = append(vocabNames, arm.Name)
					id := uint64(0)
					if reached[v.Name] {
						id = ir.TableWireId(arm.WireName())
					}
					ids = append(ids, fmt.Sprintf("%016x", id))
				}
			}
			var keyNames []string
			if f.KeyEnumRef != nil {
				for i := int64(0); i < f.ArrayBound; i++ {
					id := uint64(0)
					if reached[f.KeyEnum] && int(i) < len(f.KeyEnumRef.Variants) {
						id = ir.TableWireId(f.KeyEnumRef.VariantWireName(int(i)))
					}
					keys = append(keys, fmt.Sprintf("%016x", id))
					name := ""
					if int(i) < len(f.KeyEnumRef.Variants) {
						name = f.KeyEnumRef.Variants[i]
					}
					keyNames = append(keyNames, name)
				}
			}
			min, max := 0.0, 0.0
			isCompressed := f.Type.Kind == ir.TFloat32 && f.HasFloatRange
			if isCompressed {
				min, max = f.FMin, f.FMax
			}
			fmt.Fprintf(&b, "meta %s.%s guard=%x required=%v retired=%v wide=%s ids=%s keys=%s variants_deprecated=%s f32=%v frange=%v min=%016x max=%016x vocab=%s vmax=%d names=%s key_names=%s\n", st.Name, f.Name, f.Guard, f.Required, f.Deprecated, wide, strings.Join(ids, ","), strings.Join(keys, ","), strings.Join(retired, ","), f.Type.Kind == ir.TFloat32, isCompressed, math.Float64bits(min), math.Float64bits(max), vocabKind, vocabMax, strings.Join(vocabNames, ","), strings.Join(keyNames, ","))
			if un, ok := f.Type.Ref.(*ir.Union); ok {
				unionSize, _, tagSize, armOffset := ir.UnionLayout(u, un)
				fmt.Fprintf(&b, "union %s.%s %d %d %d %d\n", st.Name, f.Name, unionSize, tagSize, armOffset, len(un.Variants))
				for _, arm := range un.Variants {
					armSize, _ := ir.ArmLayout(u, arm)
					isTarget := false
					if arm.F != nil {
						_, isTarget = arm.F.Type.Ref.(*ir.Struct)
					}
					fmt.Fprintf(&b, "arm %s.%s %s %d %v\n", st.Name, f.Name, arm.Name, armSize, isTarget)
				}
			}
		}
	}
	return b.String()
}

func reflectionLayoutRun(t *testing.T, u *ir.Unit, files map[string][]byte, floor bool) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	source := strings.Replace(reflectionLayoutProbe, "PACKAGE", fmt.Sprintf("%q", u.Package), 1)
	bin := pyrun.Interpreter(t)
	if floor {
		bin = pyrun.Floor(t)
	}
	out, stderr, err := pyrun.Try(t, bin, dir, "main.py", map[string][]byte{"main.py": []byte(source)})
	if err != nil {
		t.Fatalf("layout probe: %v\n%s", err, stderr)
	}
	return out
}

func TestPythonReflectionLayout(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, row := range reflectionUnits(t) {
		t.Run(row.name, func(t *testing.T) {
			u := loadUnitPaths(t, row.paths...)
			files, err := reflectionGenerate(u)
			if err != nil {
				t.Fatalf("kind hole: %v", err)
			}
			want := reflectionLayoutWant(u)
			for _, floor := range []bool{false, true} {
				if got := reflectionLayoutRun(t, u, files, floor); got != want {
					t.Errorf("floor=%v layout differs:\n%s", floor, reflectionFirstDiff(want, got))
				}
			}
		})
	}
}

func TestPythonReflectionLayoutNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreter")
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.ToSlash(filepath.Join(u.Package, "Core.py"))
	src := string(files[key])
	anchor := "at=0, pieces=((0, 4), ), elem_size=4"
	if !strings.Contains(src, anchor) {
		t.Fatal("control anchor absent")
	}
	files[key] = []byte(strings.Replace(src, anchor, "at=4, pieces=((0, 4), ), elem_size=4", 1))
	for _, floor := range []bool{false, true} {
		got := reflectionLayoutRun(t, u, files, floor)
		want := reflectionLayoutWant(u)
		if got == want || !strings.Contains(reflectionFirstDiff(want, got), "field Record.value") {
			t.Fatalf("offset drift was not detected on Record.value: %s", reflectionFirstDiff(want, got))
		}
	}
}

func TestPythonReflectionLiveIdentityAndReset(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, coreSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import core
root = core.Root()
field_total = root.descriptor.field("total")
assert field_total is not None
assert field_total.get is field_total.get and field_total.set is field_total.set
assert root.descriptor.reset is root.descriptor.reset
field_total.set(root, 42)
assert field_total.get(root) == 42
field = root.descriptor.field("head")
assert field is not None and field.target_desc is core.Node.descriptor
root.descriptor.reset(root)
assert root.total == 0
view = core.UnitView()
assert next(row.type for row in view.tables if row.name == "Root") is core.Root.descriptor
assert next(row.type for row in view.tables if row.name == "Node") is core.Node.descriptor
`
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
}

func TestPythonReflectionAccessorNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, coreSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(filepath.Join(u.Package, "_schema/descriptor.py"))
	src := string(files[path])
	anchor := "        setattr(value, self.name, field_value)"
	if !strings.Contains(src, anchor) {
		t.Fatal("setter control anchor absent")
	}
	files[path] = []byte(strings.Replace(src, anchor, "        pass", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import core
r = core.Root()
f = r.descriptor.field('total')
assert f is not None
f.set(r, 42)
assert r.total == 42, 'field write drift'
`
	for _, bin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, bin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "field write drift") {
			t.Fatalf("setter sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}

func TestPythonReflectionAccessors(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, `package access
fixed table Data {
    title string(8)
    raw bytes(4)
    items [2]int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import access
x=access.Data()
for name, original, index, replacement, expected in (
    ('title', 'cat', 1, 'u', 'cut'),
    ('raw', b'cat', 1, ord('u'), b'cut'),
    ('raw', memoryview(b'cat'), 1, ord('u'), b'cut'),
    ('items', [1, 2], 1, 3, [1, 3]),
):
    f=x.descriptor.field(name)
    assert f is not None
    f.set(x, original)
    assert f.get(x) == original
    f.set_element(x, index, replacement)
    assert f.get_element(x, index) == replacement
    assert f.get(x) == expected
`
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
}

func TestPythonReflectionResetLargeArrays(t *testing.T) {
	t.Parallel()
	schema := `package bounded
fixed table Leaf { number int32 }
fixed table Root {
    items [%[1]d]Leaf
    numbers [%[1]d]int32
}
`
	small := loadUnit(t, fmt.Sprintf(schema, 2))
	large := loadUnit(t, fmt.Sprintf(schema, 2048))
	smallFiles, err := Generate(small)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(large)
	if err != nil {
		t.Fatal(err)
	}
	smallSize, largeSize := len(smallFiles["bounded/Core.py"]), len(files["bounded/Core.py"])
	if !reflectionArraySourceBounded(smallSize, largeSize) {
		t.Fatalf("fixed-array source grew with the bound: small=%d large=%d", smallSize, largeSize)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import bounded
root = bounded.Root()
assert len(root.items) == 2048 and len(root.numbers) == 2048
assert root.items[0] is not root.items[1]
root.items[0].number = 7
root.descriptor.reset(root)
assert len(root.items) == 2048 and root.items[0].number == 0
assert root.items[0] is not root.items[1]
`
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
}

func reflectionArraySourceBounded(small, large int) bool { return large-small <= 512 }

func TestPythonReflectionResetLargeArraysNegativeControl(t *testing.T) {
	t.Parallel()
	schema := `package bounded
fixed table Leaf { number int32 }
fixed table Root {
    items [%[1]d]Leaf
    numbers [%[1]d]int32
}
`
	small := loadUnit(t, fmt.Sprintf(schema, 2))
	large := loadUnit(t, fmt.Sprintf(schema, 2048))
	smallFiles, err := Generate(small)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(large)
	if err != nil {
		t.Fatal(err)
	}
	path := "bounded/Core.py"
	src := string(files[path])
	for _, tc := range []struct{ name, anchor, element string }{
		{"nested", `[new_by_name("Leaf") for _s_i in range(2048)]`, `new_by_name("Leaf")`},
		{"scalar", `[0] * 2048`, `0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(src, tc.anchor) {
				t.Fatal("unrolling control anchor absent")
			}
			expanded := "[" + strings.TrimSuffix(strings.Repeat(tc.element+", ", 2048), ", ") + "]"
			broken := strings.Replace(src, tc.anchor, expanded, 1)
			if reflectionArraySourceBounded(len(smallFiles[path]), len(broken)) {
				t.Fatal("unrolled initializer escaped the source-size gate")
			}
		})
	}
}
