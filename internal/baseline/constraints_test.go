package baseline_test

// THE CLASS, NOT THE INSTANCE. A declared constraint that a reader enforces on
// a save, and that the baseline does not record, is an edit `schema check`
// passes in silence while old saves stop reading: the enum-keyed element
// range, the bits width and the counted array's minimum were each found one
// at a time. This file closes the class by program:
//
//   - every field of the IR's declaration types (ir.Field, ir.FieldType,
//     ir.Enum, ir.Flags, ir.UnionVariant) is CLASSIFIED: it is a constraint a
//     reader holds a save to, and names the rows below that tighten it, or it
//     is not, and says why. A field added to the IR fails the test until it
//     is classified.
//   - every refusal reason the table reader can give (parsed from
//     ../tablewire/refusal.go) is classified the same way: a declaration
//     moves it, and rows show that, or no declaration does.
//   - every ROW is one constraint tightened, run in every place the
//     compiler lets the declaration stand (a field, a nested record's field,
//     a union arm, a map value, an element of a bounded, an unbounded and an
//     enum-keyed array). For each it is PROVED AGAINST A REAL READ that the
//     constraint is enforced (an old save refuses with the row's reason, or
//     reads back as something else), and then that the baseline projection
//     MOVED and the check SAID SOMETHING.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// a constraintRow is one declared constraint, tightened.
type constraintRow struct {
	name string
	// pre0 and pre1 are the declarations the field's type needs, before and
	// after; decl0 and decl1 the field's own spelling, type and attributes.
	pre0, pre1   string
	decl0, decl1 string
	// value is the JSON an old save holds in the field; "" is a field the old
	// writer left out, which only a field and a nested record's field can be.
	value string
	// reason is the refusal the new schema gives the old save. "" is a
	// constraint the reader enforces WITHOUT refusing: the save reads, and
	// reads back as something other than what was written.
	reason string
	// names is true where the finding must name that refusal reason itself.
	names bool
	// only restricts the row to the holders named, with why.
	only    []string
	onlyWhy string
	// must names the holders the row has to be proved in: a holder the compiler
	// stops admitting it in fails the test rather than thinning it.
	must []string
	// owner is set where the check is still SILENT on the edit and the fix is
	// another change's: the projection must move here, and the silence is
	// reported rather than failed, so the two changes can land in either
	// order. Empty means silence fails.
	owner string
}

const sideEnum = "enum Side\n{\n    Left\n    Right\n}\n\n"

// a holder is one place a declaration can stand in a table closure.
type holder struct {
	name   string
	schema func(pre, decl string) string
	save   func(value string) string // "" where the holder cannot leave the field out
}

func holders() []holder {
	wrap := func(body string) func(pre, decl string) string {
		return func(pre, decl string) string {
			return "package fixture\n\n" + pre + strings.Replace(body, "%s", decl, 1)
		}
	}
	present := func(open, close string) func(string) string {
		return func(v string) string {
			if v == "" {
				return ""
			}
			return open + v + close
		}
	}
	return []holder{
		{"field", wrap("table Save\n{\n    x %s\n}\n"), func(v string) string {
			if v == "" {
				return `{}`
			}
			return `{"x":` + v + `}`
		}},
		{"nested record field", wrap("type Inner\n{\n    x %s\n}\n\ntable Save\n{\n    inner Inner\n    n int32\n}\n"), func(v string) string {
			if v == "" {
				return `{"n":1}`
			}
			return `{"inner":{"x":` + v + `},"n":1}`
		}},
		{"union arm", wrap("union Holder\n{\n    x %s\n    other int32\n}\n\ntable Save\n{\n    h Holder\n}\n"), present(`{"h":{"x":`, `}}`)},
		{"map value", wrap("table Save\n{\n    m map[uint16]%s\n}\n"), present(`{"m":{"7":`, `}}`)},
		{"bounded array element", wrap("table Save\n{\n    xs [..4]%s\n}\n"), present(`{"xs":[`, `]}`)},
		{"unbounded array element", wrap("table Save\n{\n    xs []%s\n}\n"), present(`{"xs":[`, `]}`)},
		{"enum-keyed array element", wrap(sideEnum + "table Save\n{\n    xs [Side]%s\n}\n"), present(`{"xs":{"Left":`, `}}`)},
	}
}

func constraintRows() []constraintRow {
	const team2 = "enum Team\n{\n    Red\n    Blue\n}\n\n"
	const team1 = "enum Team\n{\n    Red\n}\n\n"
	const teamDep = "enum Team\n{\n    Red\n    Blue | deprecated\n}\n\n"
	const pick2 = "union Pick\n{\n    a int32\n    b int32\n}\n\n"
	const pick1 = "union Pick\n{\n    a int32\n}\n\n"
	const perks = "flags Perks\n{\n    Quad\n    Haste\n}\n\n"
	const perksMoved = "flags Perks\n{\n    Haste\n    Quad\n}\n\n"
	const ab = "table A\n{\n    n int32\n}\n\ntable B\n{\n    n int32\n}\n\n"
	const pair = "table Pair\n{\n    key uint16\n    value int32\n}\n\n"
	fieldOnly := []string{"field", "nested record field"}
	return []constraintRow{
		// ---- out_of_range ----
		{name: "integer maximum lowered", decl0: "int32 | min = -100, max = 100", decl1: "int32 | min = -100, max = 50", value: "75", reason: "out_of_range", names: true},
		{name: "integer minimum raised", decl0: "int32 | min = -100, max = 100", decl1: "int32 | min = -50, max = 100", value: "-75", reason: "out_of_range", names: true},
		{name: "integer range declared", decl0: "int32", decl1: "int32 | min = 0, max = 50", value: "75", reason: "out_of_range", names: true},
		{name: "compressed float maximum lowered", decl0: "float32 | min = 0.0, max = 100.0, resolution = 0.5", decl1: "float32 | min = 0.0, max = 50.0, resolution = 0.5", value: "75.0", reason: "out_of_range", names: true},
		{name: "compressed float minimum raised", decl0: "float32 | min = -100.0, max = 100.0, resolution = 0.5", decl1: "float32 | min = -50.0, max = 100.0, resolution = 0.5", value: "-75.0", reason: "out_of_range", names: true},
		{name: "fixed maximum lowered", decl0: "fixed(8, 8) | min = -100, max = 100", decl1: "fixed(8, 8) | min = -100, max = 50", value: "75.5", reason: "out_of_range", names: true},
		{name: "bits width narrowed", decl0: "bits(7)", decl1: "bits(5)", value: "95", reason: "out_of_range", names: true},
		// ---- kind_mismatch ----
		{name: "integer width narrowed", decl0: "int32", decl1: "int16", value: "5", reason: "kind_mismatch"},
		{name: "integer made a float", decl0: "int32", decl1: "float32", value: "5", reason: "kind_mismatch"},
		{name: "timestamp made an integer", decl0: "timestamp", decl1: "int64", value: `"2026-01-01T00:00:00Z"`, reason: "kind_mismatch"},
		{name: "array element kind changed", decl0: "[..4]int32", decl1: "[..4]float32", value: "[5]", reason: "kind_mismatch"},
		{name: "map key kind changed", decl0: "map[uint16]int32", decl1: "map[string(8)]int32", value: `{"1":5}`, reason: "kind_mismatch"},
		{name: "enum made its raw integer", pre0: team2, pre1: team2, decl0: "Team", decl1: "uint16", value: `"Blue"`, reason: "kind_mismatch"},
		// ---- length_over_bound ----
		{name: "string capacity lowered", decl0: "string(8)", decl1: "string(2)", value: `"abcdef"`, reason: "length_over_bound", names: true},
		{name: "bytes capacity lowered", decl0: "bytes(8)", decl1: "bytes(2)", value: `"aGVsbG8="`, reason: "length_over_bound", names: true},
		{name: "map key capacity lowered", decl0: "map[string(8)]int32", decl1: "map[string(2)]int32", value: `{"abcdef":1}`, reason: "length_over_bound", names: true},
		{name: "map key capacity declared", decl0: "map[string]int32", decl1: "map[string(2)]int32", value: `{"abcdef":1}`, reason: "length_over_bound", names: true},
		{name: "text list total lowered", decl0: "[..4]string(8) | total = 8", decl1: "[..4]string(8) | total = 3", value: `["ab","cd"]`, reason: "length_over_bound", names: true},
		{name: "text list total declared", decl0: "[..4]string(8)", decl1: "[..4]string(8) | total = 3", value: `["ab","cd"]`, reason: "length_over_bound", names: true},
		{name: "text list element capacity lowered", decl0: "[..4]string(8)", decl1: "[..4]string(2)", value: `["abcdef"]`, reason: "length_over_bound", names: true},
		// ---- count_over_bound ----
		{name: "array bound lowered", decl0: "[..4]int32", decl1: "[..2]int32", value: "[1,2,3,4]", reason: "count_over_bound", names: true},
		{name: "array minimum raised", decl0: "[..4]int32", decl1: "[2..4]int32", value: "[7]", reason: "count_over_bound", names: true,
			must: []string{"field", "nested record field", "union arm", "map value"}},
		{name: "fixed length lowered", decl0: "[4]int32", decl1: "[2]int32", value: "[1,2,3,4]", reason: "count_over_bound", names: true},
		{name: "unbounded array given a bound", decl0: "[]int32", decl1: "[..2]int32", value: "[1,2,3]", reason: "count_over_bound", names: true},
		// ---- missing_required ----
		{name: "required added", decl0: "int32", decl1: "int32 | required", value: "", reason: "missing_required", names: true},
		// ---- wire_damaged, where a declaration is what moved ----
		{name: "pair array made a map", pre0: pair, decl0: "[..4]Pair", decl1: "map[uint16]int32", value: `[{"key":2,"value":22},{"key":1,"value":11}]`, reason: "wire_damaged", names: true,
			only: []string{"field"}, onlyWhy: "the pair's own table would be a second map entry in any other holder"},
		{name: "byte buffer made a text buffer", decl0: "*bytes", decl1: "*string", value: `"aGk="`, reason: "wire_damaged"},
		{name: "pointer retargeted", pre0: ab, pre1: ab, decl0: "*A", decl1: "*B", value: `{"n":1}`, reason: "wire_damaged",
			owner: "rowan/fix-baseline-referents: a pointer's target type is recorded (type=) and a twin table still stands in for it"},
		// ---- enforced without a refusal: the save reads as something else ----
		{name: "specified default changed", decl0: "int32 = 21", decl1: "int32 = 25", value: "", only: fieldOnly, onlyWhy: "only a field takes a default"},
		{name: "array minimum lowered", decl0: "[2..4]int32", decl1: "[..4]int32", value: "", only: fieldOnly, onlyWhy: "only a field is elided at its born value"},
		{name: "fixed fractional bits moved", decl0: "fixed(8, 8) | min = -100, max = 100", decl1: "fixed(12, 4) | min = -100, max = 100", value: "0.5"},
		{name: "enum variant removed", pre0: team2, pre1: team1, decl0: "Team", decl1: "Team", value: `"Blue"`},
		{name: "enum variant deprecated", pre0: team2, pre1: teamDep, decl0: "Team", decl1: "Team", value: `"Blue"`},
		{name: "key enum variant removed", pre0: team2, pre1: team1, decl0: "[Team]uint8", decl1: "[Team]uint8", value: `{"Red":11,"Blue":22}`},
		{name: "union arm removed", pre0: pick2, pre1: pick1, decl0: "Pick", decl1: "Pick", value: `{"b":5}`},
		{name: "flags variant moved", pre0: perks, pre1: perksMoved, decl0: "Perks", decl1: "Perks", value: `["Quad"]`},
	}
}

// classified says what one IR field or one refusal reason is to a save: the
// rows that tighten it, or why no row can.
type classified struct {
	rows []string
	not  string
}

func rows(names ...string) classified { return classified{rows: names} }
func not(why string) classified       { return classified{not: why} }

// irFacts classifies every field of the IR's declaration types.
var irFacts = map[string]classified{
	// ---- ir.Field ----
	"Field.Name":          not("the wire id is the name's hash: a rename is a removal and an addition, which the wire absorbs and the field-pair row reports"),
	"Field.Guard":         not("a guard removes the field from the bytes as a default does, and the reader's default absorbs both (baseline.Render)"),
	"Field.Doc":           not("metadata no codec reads"),
	"Field.Tags":          not("metadata no codec reads"),
	"Field.WasName":       not("the alias that KEEPS the wire id; a second one aimed at an intermediate name is the was-chain row"),
	"Field.JsonKey":       not("the text key, which moves no wire byte (§16.4)"),
	"Field.Deprecated":    not("the variable read takes a stored value under a deprecated field as it takes any other; the marker binds the writer and the fixed form, which the lock holds"),
	"Field.Required":      rows("required added"),
	"Field.Array":         rows("pair array made a map", "unbounded array given a bound", "array minimum raised"),
	"Field.ArrayBound":    rows("array bound lowered", "fixed length lowered", "unbounded array given a bound"),
	"Field.ArrayExpr":     not("the bound's source spelling; the evaluated bound is ArrayBound"),
	"Field.TextTotal":     rows("text list total lowered", "text list total declared"),
	"Field.ArrayMin":      rows("array minimum raised", "array minimum lowered"),
	"Field.KeyEnum":       rows("key enum variant removed"),
	"Field.KeyEnumRef":    rows("key enum variant removed"),
	"Field.MapEntry":      rows("map key kind changed", "map key capacity lowered", "map key capacity declared"),
	"Field.Type":          not("classified field by field below, as FieldType"),
	"Field.HasDefault":    rows("specified default changed"),
	"Field.DefBool":       rows("specified default changed"),
	"Field.DefInt":        rows("specified default changed"),
	"Field.DefFloat":      rows("specified default changed"),
	"Field.DefVariant":    rows("specified default changed"),
	"Field.DefBytes":      rows("specified default changed"),
	"Field.DefExpr":       not("the default's source spelling; the evaluated default is the Def* value"),
	"Field.HasIntRange":   rows("integer range declared"),
	"Field.RangeOneSided": not("checker bookkeeping: the filled range is the two-sided declaration (ir.Field)"),
	"Field.IntMin":        rows("integer minimum raised"),
	"Field.IntMax":        rows("integer maximum lowered", "fixed maximum lowered"),
	"Field.IntMinExpr":    not("the bound's source spelling"),
	"Field.IntMaxExpr":    not("the bound's source spelling"),
	"Field.HasFloatRange": rows("compressed float maximum lowered"),
	"Field.FMin":          rows("compressed float minimum raised"),
	"Field.FMax":          rows("compressed float maximum lowered"),
	"Field.Resolution":    not("the type wire's quantization step; a save stores the float itself and the table reader holds it to the range alone"),
	"Field.Round":         not("a frozen type-wire projection token"),
	"Field.Steps":         not("derived from the range and the resolution, for the type wire"),
	// ---- ir.FieldType ----
	"FieldType.Kind":      rows("integer made a float", "enum made its raw integer"),
	"FieldType.Signed":    not("the kind token: a signed and an unsigned integer of one width are two wire kinds, and the integer-width row holds the kind"),
	"FieldType.Width":     rows("integer width narrowed", "bits width narrowed"),
	"FieldType.IntBits":   rows("fixed fractional bits moved"),
	"FieldType.FracBits":  rows("fixed fractional bits moved"),
	"FieldType.Size":      rows("string capacity lowered", "bytes capacity lowered", "text list element capacity lowered"),
	"FieldType.SizeExpr":  not("the capacity's source spelling"),
	"FieldType.Name":      rows("pointer retargeted", "enum made its raw integer"),
	"FieldType.Ref":       rows("pointer retargeted", "enum variant removed", "union arm removed", "flags variant moved"),
	"FieldType.Pointer":   rows("pointer retargeted", "byte buffer made a text buffer"),
	"FieldType.Optional":  not("T and ?T share variable framing, so no read refuses the move; the baseline holds it one way, refusing ?T to T (R16, §4, §18.2): TestBaselineOptionalIsOneWay"),
	"FieldType.Timestamp": rows("timestamp made an integer"),
	"FieldType.Unbounded": rows("map key capacity declared"),
	// ---- ir.Enum ----
	"Enum.Name":              not("a declaration's name is not on the wire; a rename is the member row's"),
	"Enum.SchemaName":        not("the declared spelling of a name a backend respelled; a name is not on the wire"),
	"Enum.Variants":          rows("enum variant removed", "key enum variant removed"),
	"Enum.Was":               not("the alias that keeps a variant's id"),
	"Enum.Max":               not("the type wire's range; a save names a variant by its name id"),
	"Enum.StorageBits":       not("the type wire's storage; a save names a variant by its name id"),
	"Enum.Doc":               not("metadata no codec reads"),
	"Enum.Tags":              not("metadata no codec reads"),
	"Enum.VariantDocs":       not("metadata no codec reads"),
	"Enum.VariantTags":       not("metadata no codec reads"),
	"Enum.VariantDeprecated": rows("enum variant deprecated"),
	// ---- ir.Flags ----
	"Flags.Name":        not("a declaration's name is not on the wire"),
	"Flags.SchemaName":  not("the declared spelling of a name a backend respelled; a name is not on the wire"),
	"Flags.Variants":    rows("flags variant moved"),
	"Flags.WireBits":    not("the type wire's mask width; a save's mask is held by the positions the flags section records"),
	"Flags.Doc":         not("metadata no codec reads"),
	"Flags.Tags":        not("metadata no codec reads"),
	"Flags.VariantDocs": not("metadata no codec reads"),
	"Flags.VariantTags": not("metadata no codec reads"),
	// ---- ir.UnionVariant ----
	"UnionVariant.Name":    rows("union arm removed"),
	"UnionVariant.WasName": not("the alias that keeps the arm's id"),
	"UnionVariant.Type":    not("the payload's name, judged as a referent (payload=) by the stand-in rule (§18.3)"),
	"UnionVariant.Ref":     not("the payload's declaration, judged as a referent (payload=) by the stand-in rule (§18.3)"),
	"UnionVariant.F":       not("an arm is a field line: every Field row above runs in the union arm holder"),
	"UnionVariant.Doc":     not("metadata no codec reads"),
	"UnionVariant.Tags":    not("metadata no codec reads"),
}

// reasonFacts classifies every refusal reason the table reader can give.
var reasonFacts = map[string]classified{
	"wire_damaged":          rows("pair array made a map", "byte buffer made a text buffer", "pointer retargeted"),
	"kind_mismatch":         rows("integer width narrowed", "integer made a float", "array element kind changed", "map key kind changed"),
	"out_of_range":          rows("integer maximum lowered", "integer minimum raised", "compressed float maximum lowered", "fixed maximum lowered", "bits width narrowed"),
	"count_over_bound":      rows("array bound lowered", "array minimum raised", "fixed length lowered", "unbounded array given a bound"),
	"length_over_bound":     rows("string capacity lowered", "bytes capacity lowered", "map key capacity lowered", "text list total lowered", "text list element capacity lowered"),
	"missing_required":      rows("required added"),
	"count_over_extent_cap": not("the int32 extent cap every array shares (§2.2): a constant of the format, moved by no declaration"),
	"blob_over_size_cap":    not("the blob size cap (§2.5): a constant of the format, moved by no declaration"),
	"blob_shared":           not("two slots naming one blob node: a fault of the file's structure, moved by no declaration"),
	"non_finite_float":      not("a NaN or an infinity is refused on its raw bits whatever is declared (§3.3)"),
	"integrity_mismatch":    not("the file's own checksum, moved by no declaration"),
}

func TestEveryReaderConstraintMovesTheProjection(t *testing.T) {
	all := constraintRows()
	byName := map[string]*constraintRow{}
	for i := range all {
		if byName[all[i].name] != nil {
			t.Fatalf("two rows are named %q", all[i].name)
		}
		byName[all[i].name] = &all[i]
	}
	cited := map[string]bool{}
	cite := func(what string, c classified) {
		if (len(c.rows) == 0) == (c.not == "") {
			t.Errorf("%s must name its rows or say why it has none, and not both", what)
		}
		for _, r := range c.rows {
			if byName[r] == nil {
				t.Errorf("%s names the row %q, and there is no such row", what, r)
			}
			cited[r] = true
		}
	}

	// ---- the IR's declaration types, field by field ----
	seen := map[string]bool{}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[ir.Field](), reflect.TypeFor[ir.FieldType](), reflect.TypeFor[ir.Enum](),
		reflect.TypeFor[ir.Flags](), reflect.TypeFor[ir.UnionVariant](),
	} {
		for field := range typ.Fields() {
			key := typ.Name() + "." + field.Name
			seen[key] = true
			c, ok := irFacts[key]
			if !ok {
				t.Errorf("ir.%s is not classified: say which rows tighten it, or why a reader holds no save to it (irFacts)", key)
				continue
			}
			cite("ir."+key, c)
		}
	}
	for key := range irFacts {
		if !seen[key] {
			t.Errorf("irFacts classifies ir.%s, and the IR has no such field", key)
		}
	}

	// ---- the reader's refusal reasons, parsed from its own source ----
	reasons := readerReasons(t)
	for _, reason := range reasons {
		c, ok := reasonFacts[reason]
		if !ok {
			t.Errorf("the reader's refusal reason %q is not classified: say which rows a declaration moves it by, or why none does (reasonFacts)", reason)
			continue
		}
		cite("reason "+reason, c)
		for _, r := range c.rows {
			if row := byName[r]; row != nil && row.reason != reason {
				t.Errorf("reason %s names the row %q, whose read refuses %q", reason, r, row.reason)
			}
		}
	}
	for reason := range reasonFacts {
		if !slices.Contains(reasons, reason) {
			t.Errorf("reasonFacts classifies %q, and ../tablewire/refusal.go declares no such reason", reason)
		}
	}
	for _, row := range all {
		if row.reason != "" && !slices.Contains(reasons, row.reason) {
			t.Errorf("row %q expects the refusal %q, which the reader does not declare", row.name, row.reason)
		}
		if !cited[row.name] {
			t.Errorf("row %q is cited by no IR field and no refusal reason: it tightens nothing this test enumerates", row.name)
		}
	}

	// ---- every row, in every holder the compiler admits it in ----
	ran := map[string]int{} // holder -> rows proved there
	for _, row := range all {
		t.Run(row.name, func(t *testing.T) {
			proved := 0
			provedIn := map[string]bool{}
			for _, h := range holders() {
				if len(row.only) > 0 && !slices.Contains(row.only, h.name) {
					continue
				}
				save := h.save(row.value)
				if save == "" {
					continue // the holder cannot leave its value out
				}
				before, after := h.schema(row.pre0, row.decl0), h.schema(row.pre1, row.decl1)
				old, err0 := tryUnit(before)
				live, err1 := tryUnit(after)
				if err0 != nil || err1 != nil {
					if len(row.only) > 0 {
						t.Errorf("%s: the row is restricted to this holder and does not compile there: %v %v", h.name, err0, err1)
					}
					continue // the language does not let the declaration stand here
				}
				r, err := trySavedRead(old, live, "Save", save)
				if err != nil {
					t.Errorf("%s: %v", h.name, err)
					continue
				}

				// 1. THE READER ENFORCES IT
				switch {
				case row.reason != "" && (r.OK || r.Reason != row.reason):
					t.Errorf("%s: the old save %s must refuse %s under the tightened schema, got ok=%t reason=%q text=%s", h.name, save, row.reason, r.OK, r.Reason, r.Text)
					continue
				case row.reason == "" && !r.OK:
					t.Errorf("%s: the old save %s must read, as something else, and it refused %s", h.name, save, r.Reason)
					continue
				case row.reason == "" && r.Text == r.Own:
					t.Errorf("%s: the old save %s reads back unchanged (%s): the row no longer tightens anything here", h.name, save, r.Text)
					continue
				}
				proved++
				provedIn[h.name] = true
				ran[h.name]++

				// 2. THE PROJECTION MOVED
				if baseline.Render(old).Text() == baseline.Render(live).Text() {
					t.Errorf("%s: THE BASELINE DOES NOT RECORD IT: %q -> %q leaves the projection byte-identical while the old save %s %s",
						h.name, row.decl0, row.decl1, save, outcome(r))
					continue
				}

				// 3. THE CHECK SAID SOMETHING
				got := baseline.Diff(committed(t, before), baseline.Render(live), baseline.DefaultTokenPolicy)
				refusals, warns := baseline.Split(got)
				if len(refusals)+len(warns) == 0 {
					if row.owner != "" {
						t.Logf("%s: RECORDED BUT SILENT, and owned elsewhere: %s", h.name, row.owner)
						continue
					}
					t.Errorf("%s: THE CHECK IS SILENT: %q -> %q moves the projection and draws no finding while the old save %s %s",
						h.name, row.decl0, row.decl1, save, outcome(r))
					continue
				}
				if row.names {
					named := false
					for _, f := range got {
						named = named || strings.Contains(f.What, row.reason)
					}
					if !named {
						t.Errorf("%s: no finding names the refusal the read gave (%s), got:%s", h.name, row.reason, summary(got))
					}
				}
			}
			for _, h := range row.must {
				if !provedIn[h] {
					t.Errorf("the row must be proved in the %s holder, and it was not", h)
				}
			}
			if proved == 0 {
				t.Errorf("the row was proved in no holder: it compiles nowhere, or its fixture fits none")
			}
		})
	}

	// the minimum count holds wherever a counted array can stand, which is
	// the finding this file was written for; and no holder is dead weight
	for _, h := range holders() {
		if ran[h.name] == 0 {
			t.Errorf("no row was proved in the %s holder", h.name)
		}
	}
	t.Logf("rows proved per holder: %v", ran)
}

func outcome(r savedRead) string {
	if !r.OK {
		return "refuses " + r.Reason
	}
	return "reads back as " + r.Text + " (it was " + r.Own + ")"
}

// readerReasons is every refusal reason the table reader declares: the string
// constants named Reason* in ../tablewire/refusal.go, read from the source so
// a reason added there is met here.
func readerReasons(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../tablewire/refusal.go", nil, 0)
	if err != nil {
		t.Fatalf("the reader's refusal vocabulary: %v", err)
	}
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Reason") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("reason %s: %v", name.Name, err)
				}
				out = append(out, s)
			}
		}
	}
	if len(out) < 6 {
		t.Fatalf("../tablewire/refusal.go declares %d refusal reasons: the vocabulary moved and this test no longer reads it", len(out))
	}
	slices.Sort(out)
	return out
}
