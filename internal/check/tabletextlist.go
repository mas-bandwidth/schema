// Text lists and the array root in the checker (docs/SPEC-TABLES.md §2.13,
// §16.10): the four text-list spellings, the generated element record and the
// names it claims, and the `json = "array"` table line with its refusals, each
// by name.
//
// A text list is §2.8's generated-record pattern with one field: the element
// resolves through the ordinary `string(M)` path, so its bound and its range
// are the scalar's own, and what this file adds is the placements the
// construct is refused in and the spellings that name something else.
package check

import (
	"math"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// resolveTextListField resolves `rules [..16]string(64)`, `[..16]?string(64)`,
// `[]string(64)` and `[]?string(64)` (docs/SPEC-TABLES.md §2.13), and refuses
// `[..N]?T` for every element but `string(M)` by name.
func (c *checker) resolveTextListField(owner string, f *ast.Field, inTable bool) *ir.Field {
	if f.Type.Kind != ast.ScalarString || f.Type.Pointer {
		c.errf(f.Type.Pos, "field %s: [..N]?T and []?T are a named follow-on for every T but string(M) — an element's presence bit beside the array's own count is §15's open question, answered for text only; declare [..N]*T or []*T with a null slot, or ?[..N]T for a presence bit on the array (docs/SPEC-TABLES.md §2.9, §2.13, §15)", f.Name)
		return nil
	}
	spelling := textListSpelling(f)
	route := "declare the field in a plain `table` body"
	st := c.tables[owner]
	switch {
	case !inTable:
		c.errf(f.Pos, "field %s: %s is a text list, and a text list is a VARIABLE-table construct — a `type`'s wire is positional and carries no array of text (SPEC.md §4.6); %s (docs/SPEC-TABLES.md §2.13, §11)", f.Name, spelling, route)
		return nil
	case c.arm != "":
		c.errf(f.Pos, "%s: a text list is not an arm — wrap the list in a plain table and make THAT the arm (docs/SPEC-TABLES.md §2.13, §2.6, §11)", c.arm)
		return nil
	case st == nil:
		c.errf(f.Pos, "field %s: %s is a text list, and a text list is not a map value — wrap it in a plain table and make that the value (docs/SPEC-TABLES.md §2.13, §2.8, §11)", f.Name, spelling)
		return nil
	case st.FixedDeclared:
		c.errf(f.Pos, "fixed table %s: field %s is a text list (%s), and a text list is a VARIABLE-table construct — its count and its elements vary, which a fixed record cannot hold; %s, or drop `fixed` from %s (docs/SPEC-TABLES.md §2.2, §2.13, §11)", owner, f.Name, spelling, route, owner)
		return nil
	}
	if f.Type.Optional {
		c.errf(f.Type.Pos, "field %s: %s puts a presence bit on a list of text, and an empty list is elided and its count's zero IS its absence, so the bit would be two answers to one question; drop the leading ?, or write it after the bound for elements that may be absent (docs/SPEC-TABLES.md §2.13, §11)", f.Name, "?"+spelling)
		return nil
	}
	if f.Array.Kind == ast.ArrayFixed || f.Array.Kind == ast.ArrayRange {
		c.errf(f.Pos, "field %s: %s is refused — a text list is COUNTED, so a fixed extent, an enum key or a lower bound names a count that cannot vary the way a list's does; spell it [..N]%s (docs/SPEC-TABLES.md §2.13, §2.13.1, §11)", f.Name, spelling, textElemSpelling(f))
		return nil
	}
	if f.Default != nil {
		c.errf(f.Pos, "field %s: a text list takes no specified default — a fresh list is empty, and empty is the only value a default could name (docs/SPEC-TABLES.md §2.13)", f.Name)
		return nil
	}
	total := int64(0)
	for i := range f.Attrs {
		a := &f.Attrs[i]
		if a.Value != nil && a.Key == "total" {
			if total != 0 {
				continue // qualification refuses the repeat by name
			}
			if total = c.textListTotal(f, a); total == 0 {
				return nil
			}
			continue
		}
		if a.Value != nil && a.Key != "was" && a.Key != "json" {
			c.errf(a.Pos, "field %s: %s does not apply to a text list — the element's bound is its string(M) and the count's is the array's; drop the attribute (docs/SPEC-TABLES.md §2.13, §11)", f.Name, a.Key)
			return nil
		}
	}

	elemName := ir.TextListElemName(owner, f.Name)
	if d, declared := c.astDecls[elemName]; declared {
		c.errf(f.Pos, "field %s: the text list generates a record named %s, and this unit already declares a %s under that name — the name is CLAIMED on %s by the list; rename the declaration, or rename the field (docs/SPEC-TABLES.md §2.13, §11)", f.Name, elemName, declKindName(d), owner)
		return nil
	}
	if prior := c.tables[elemName]; prior != nil && prior.MapEntryOf != "" {
		c.errf(f.Pos, "field %s: the text list generates a record named %s, and %s already generated one under that name; rename one of the two fields (docs/SPEC-TABLES.md §2.13)", f.Name, elemName, prior.MapEntryOf)
		return nil
	}

	// THE ELEMENT resolves through the ordinary string(M) path: its bound,
	// its floor and its ceiling are the scalar's own
	vf := &ast.Field{Name: ir.TextListValueFieldName, Pos: f.Pos, Type: f.Type}
	vf.Type.Optional = false
	vf.Type.ElemOptional = false
	// an UNBOUNDED element, `[]string` (§2.13.1), resolves through the same
	// path, which takes the bare word here and nowhere else
	saved := c.unboundedText
	c.unboundedText = true
	value := c.resolveField(elemName, vf, true)
	c.unboundedText = saved
	if value == nil {
		return nil
	}
	// a TOTAL on an UNBOUNDED element (docs/SPEC-TABLES.md §2.13, §2.13.1): no
	// Go writer or reader path holds it there yet, so it is refused by name
	if total > 0 && value.Type.TextUnbounded() {
		c.errf(f.Pos, "field %s: total on a list of unbounded elements (%s) is a named follow-on — the elements live in the builder's arena and no writer or reader holds a total there yet; bound the element as string(M) to declare a total (docs/SPEC-TABLES.md §2.13, §2.13.1, §11)", f.Name, spelling)
		return nil
	}
	// the one place a `?` on a string is legal, and only the generator
	// writes it (§2.13): the element's presence
	value.Type.Optional = f.Type.ElemOptional

	entry := &ir.Struct{
		Name:       elemName,
		IsTable:    true,
		MapEntryOf: owner + "." + f.Name,
		TextElem:   true,
		Fields:     []*ir.Field{value},
		Items:      []ir.Item{&ir.FieldItem{F: value}},
	}
	c.tables[elemName] = entry
	c.declFile[elemName] = c.declFile[owner]
	c.mapEntries = append(c.mapEntries, entry)

	out := &ir.Field{Name: f.Name, Doc: f.Doc, Type: ir.FieldType{Kind: ir.TNamed, Name: elemName, Ref: entry}}
	if f.Array.Kind == ast.ArrayList {
		out.Array = ir.ArrayList
	} else {
		hi, ok := c.evalInt(f.Array.Hi)
		if !ok {
			return nil
		}
		if !hi.IsInt64() || hi.Int64() < 1 {
			c.errf(f.Pos, "array bound %s below 1 (SPEC §4.6)", hi)
			return nil
		}
		if hi.Int64() > math.MaxInt32 {
			c.errf(f.Pos, "array bound %s above %d — counts live in int32 storage (SPEC §4.3, §6.1)", hi, math.MaxInt32)
			return nil
		}
		out.Array = ir.ArrayCounted
		out.ArrayBound = hi.Int64()
		out.ArrayExpr = f.Array.Hi
	}
	out.TextTotal = total
	c.resolveAttrs(f, out)
	return out
}

// textListTotal reads a text list's `| total = N` (docs/SPEC-TABLES.md §2.13):
// an integer constant expression of at least 1, the sum of the elements' byte
// lengths. Zero is the refusal, already reported.
func (c *checker) textListTotal(f *ast.Field, a *ast.Attr) int64 {
	if _, ok := a.Value.(*ast.StringLit); ok {
		c.errf(a.Pos, "field %s: total takes an integer — the most bytes the list's elements hold in all, e.g. | total = 256 (docs/SPEC-TABLES.md §2.13)", f.Name)
		return 0
	}
	n, ok := c.evalInt(a.Value)
	if !ok {
		return 0
	}
	if n.Sign() < 1 {
		c.errf(a.Pos, "field %s: total = %s is below 1 — a total bounds the sum of the elements' bytes, and a list that may hold no byte is spelled without one (docs/SPEC-TABLES.md §2.13, §11)", f.Name, n)
		return 0
	}
	if !n.IsInt64() {
		c.errf(a.Pos, "field %s: total = %s is above %d — the sum is counted in int64 (docs/SPEC-TABLES.md §2.13)", f.Name, n, int64(math.MaxInt64))
		return 0
	}
	return n.Int64()
}

// textListSpelling is the spelling a diagnostic quotes back.
func textListSpelling(f *ast.Field) string {
	bound := "[]"
	switch f.Array.Kind {
	case ast.ArrayUpTo:
		bound = "[..N]"
	case ast.ArrayFixed:
		bound = "[N]"
		if id, ok := f.Array.Hi.(*ast.IdentExpr); ok {
			bound = "[" + id.Name + "]"
		}
	case ast.ArrayRange:
		bound = "[A..N]"
	}
	if f.Type.ElemOptional {
		bound += "?"
	}
	return bound + textElemSpelling(f)
}

// textElemSpelling is the element as written: `string(M)`, or the bare
// `string` of an unbounded element (§2.13.1).
func textElemSpelling(f *ast.Field) string {
	if f.Type.Unbounded {
		return "string"
	}
	return "string(M)"
}

// refuseBareString refuses a bare `string` wherever it is not a text-list
// element or a map key (docs/SPEC-TABLES.md §2.13.1, §11), naming the route.
func (c *checker) refuseBareString(owner string, f *ast.Field, inTable bool) {
	st := c.tables[owner]
	switch {
	case c.arm != "":
		c.errf(f.Type.Pos, "%s: a bare `string` is not an arm — unbounded text is a plain table's text-list element ([]string) or map key (map[string]V) and nothing else; declare string(N) for inline text or *string for a buffer at its used size (docs/SPEC-TABLES.md §2.13.1, §2.6, §11)", c.arm)
	case c.mapValue:
		c.errf(f.Type.Pos, "field %s: a map VALUE of bare `string` is refused — unbounded text is a text-list element or a map KEY; declare the value string(N) or *string, or wrap the text in a table (docs/SPEC-TABLES.md §2.13.1, §2.8, §11)", f.Name)
	case !inTable:
		c.errf(f.Type.Pos, "field %s: a bare `string` in a `type` body or a packet is refused — the type wire carries no unbounded text (SPEC.md §4.6); declare string(N), or move the field to a plain `table` (docs/SPEC-TABLES.md §2.13.1, §11)", f.Name)
	case st != nil && st.FixedDeclared:
		c.errf(f.Type.Pos, "fixed table %s: field %s is a bare `string`, and unbounded text is a VARIABLE-table construct — declare string(N), or drop `fixed` from %s and hold the text as []string or a map[string]V key in a plain `table` (docs/SPEC-TABLES.md §2.13.1, §2.2, §11)", owner, f.Name, owner)
	default:
		c.errf(f.Type.Pos, "field %s: a bare `string` has no bound, and only a text-list element ([]string, [..N]string) and a map key (map[string]V) may be unbounded text; declare string(N) for inline text, or *string for a buffer at its used size (docs/SPEC-TABLES.md §2.13.1, §2.5, §11)", f.Name)
	}
}

// tableJsonRoot reads a table line's `json = "array"` or `json = "value"`
// (docs/SPEC-TABLES.md §16.10, §16.13). The value is closed: "array" and
// "value", and nothing else.
func (c *checker) tableJsonRoot(d *ast.TableDecl, st *ir.Struct, a *ast.Attr) {
	lit, ok := a.Value.(*ast.StringLit)
	switch {
	case !ok:
		c.errf(a.Pos, `table %s: json on a table line takes a quoted string, and the values it takes are "array" and "value" — json = "array" declares a table whose text is a top-level array, json = "value" one whose text is its raw JSON value (docs/SPEC-TABLES.md §16.10, §16.13)`, d.Name)
		return
	case lit.Value != "array" && lit.Value != "value":
		c.errf(a.Pos, `table %s: json = %q names no text shape — on a table line json takes the values "array" and "value" (docs/SPEC-TABLES.md §16.10, §16.13)`, d.Name, lit.Value)
		return
	case d.Fixed:
		c.errf(a.Pos, `fixed table %s: json = %q is a VARIABLE-table declaration — drop it, or drop fixed (docs/SPEC-TABLES.md §16.10, §16.13, §11)`, d.Name, lit.Value)
		return
	}
	if lit.Value == "array" {
		st.JsonArray = true
		return
	}
	st.MarkJsonValue()
}

// checkJsonValue holds the raw JSON value's SHAPE (docs/SPEC-TABLES.md
// §16.13): a table with exactly `nodes []JsonNode` then `text *bytes`, an
// element table with exactly `kind JsonKind`, `count uint32`, `key_len uint32`
// and `text_len uint32`, an enum with exactly the seven variants Null, False,
// True, Number, String, Array and Object, and `integrity = "crc32c"` on the
// same line. Type names are free. Every mismatch is refused by name with
// `json_value_shape: <Table>: <the first difference>`.
func (c *checker) checkJsonValue(d *ast.TableDecl, st *ir.Struct) {
	pos := d.DeclPos()
	fail := func(format string, args ...any) {
		c.errf(pos, "json_value_shape: %s: "+format, append([]any{d.Name}, args...)...)
	}
	plainField := func(f *ir.Field) string {
		switch {
		case f.Guard != "":
			return "a guard"
		case f.JsonKey != "":
			return "a json key"
		case f.Required:
			return "required"
		case f.Deprecated:
			return "deprecated"
		case f.Type.Optional:
			return "?"
		case f.WasName != "":
			return "was"
		case f.HasDefault:
			return "a default"
		case len(f.Tags) != 0:
			return "a tag"
		case f.TextTotal != 0:
			return "total"
		case f.ArrayMin != 0:
			return "a range"
		}
		return ""
	}

	if len(st.Fields) != 2 {
		fail("exactly the two fields nodes then text, in that order, and it has %d", len(st.Fields))
		return
	}
	nodes, text := st.Fields[0], st.Fields[1]
	if nodes.Name != "nodes" {
		fail("field 1 is named %q, not nodes", nodes.Name)
		return
	}
	if nodes.Array != ir.ArrayList || nodes.Type.Kind != ir.TNamed || nodes.Type.Pointer {
		fail("field nodes is not []JsonNode")
		return
	}
	if a := plainField(nodes); a != "" {
		fail("field nodes carries %s", a)
		return
	}
	elem, ok := nodes.Type.Ref.(*ir.Struct)
	if !ok || elem == nil || !elem.IsTable || elem.FixedDeclared || elem.MapEntryOf != "" || elem.TextElem {
		fail("field nodes is not an array of an element table")
		return
	}
	if text.Name != "text" {
		fail("field 2 is named %q, not text", text.Name)
		return
	}
	if text.Array != ir.ArrayNone || text.Type.Kind != ir.TBytes || !text.Type.Pointer || text.Type.Size != 0 {
		fail("field text is not *bytes")
		return
	}
	if a := plainField(text); a != "" {
		fail("field text carries %s", a)
		return
	}
	if c.checkJsonValueElement(elem, fail, plainField) {
		return
	}
	if st.Integrity != "crc32c" {
		fail(`integrity = "crc32c" on the same table line`)
	}
}

// checkJsonValueElement holds the element table and its enum to §16.13:
// exactly kind, count, key_len and text_len, in that order, with an enum of
// exactly Null, False, True, Number, String, Array and Object. It answers true
// when it has already refused a difference.
func (c *checker) checkJsonValueElement(elem *ir.Struct, fail func(string, ...any), plainField func(*ir.Field) string) bool {
	if len(elem.Fields) != 4 {
		fail("the element table %s has %d fields, not exactly kind, count, key_len and text_len", elem.Name, len(elem.Fields))
		return true
	}
	wantNames := [4]string{"kind", "count", "key_len", "text_len"}
	for i, want := range wantNames {
		if got := elem.Fields[i].Name; got != want {
			fail("element field %d is named %q, not %s", i+1, got, want)
			return true
		}
	}
	kind := elem.Fields[0]
	if kind.Array != ir.ArrayNone || kind.Type.Kind != ir.TNamed || kind.Type.Pointer {
		fail("element field kind is not an enum")
		return true
	}
	if a := plainField(kind); a != "" {
		fail("element field kind carries %s", a)
		return true
	}
	enum, ok := kind.Type.Ref.(*ir.Enum)
	if !ok || enum == nil {
		fail("element field kind is not an enum")
		return true
	}
	wantVariants := []string{"Null", "False", "True", "Number", "String", "Array", "Object"}
	if len(enum.Variants) != len(wantVariants) {
		fail("the enum %s has %d variants, not exactly Null, False, True, Number, String, Array and Object", enum.Name, len(enum.Variants))
		return true
	}
	for i, want := range wantVariants {
		if got := enum.Variants[i]; got != want {
			fail("variant %d of enum %s is %q, not %s", i+1, enum.Name, got, want)
			return true
		}
	}
	for _, f := range elem.Fields[1:] {
		if f.Array != ir.ArrayNone || f.Type.Kind != ir.TInt || f.Type.Signed || f.Type.Width != 32 {
			fail("element field %s is not uint32", f.Name)
			return true
		}
		if a := plainField(f); a != "" {
			fail("element field %s carries %s", f.Name, a)
			return true
		}
	}
	return false
}

// checkArrayRoot holds the array root's shape (docs/SPEC-TABLES.md §16.10): ONE
// field, a `[..N]T` or a `[]T`, unguarded and without a key of its own.
func (c *checker) checkArrayRoot(d *ast.TableDecl, st *ir.Struct) {
	pos := d.DeclPos()
	for _, f := range st.Fields {
		if f.Guard != "" {
			c.errf(pos, `table %s: json = "array" refuses a guarded (if) field, and %s is one — the text has no key to say whether a branch was taken (docs/SPEC-TABLES.md §16.10)`, d.Name, f.Name)
			return
		}
	}
	if len(st.Fields) != 1 {
		c.errf(pos, `table %s: json = "array" needs exactly ONE field, a [..N]T or a []T, and %s has %d — the text is that field's array and has no room for another (docs/SPEC-TABLES.md §16.10)`, d.Name, d.Name, len(st.Fields))
		return
	}
	f := st.Fields[0]
	switch {
	case f.JsonKey != "":
		c.errf(pos, `table %s: json = "array" refuses a json key on its one field %s — the field's array IS the text, so there is no key to carry (docs/SPEC-TABLES.md §16.10)`, d.Name, f.Name)
	case f.Type.Optional && f.Array != ir.ArrayNone:
		c.errf(pos, `table %s: json = "array" refuses ?[..N]T on its one field %s — an absent array has no array to be the text; drop the ? (docs/SPEC-TABLES.md §16.10)`, d.Name, f.Name)
	case f.Array == ir.ArrayFixed:
		c.errf(pos, `table %s: json = "array" refuses [N]T and [E]T on its one field %s — the field must be [..N]T or []T (docs/SPEC-TABLES.md §16.10)`, d.Name, f.Name)
	case f.Array == ir.ArrayNone || f.IsMap():
		c.errf(pos, `table %s: json = "array" needs its one field %s to be a [..N]T or a []T, and it is not an array (docs/SPEC-TABLES.md §16.10)`, d.Name, f.Name)
	}
}

// checkArrayRootTargets refuses an array-root table any pointer targets
// (docs/SPEC-TABLES.md §16.10): `&node` (§16.7) has no object to live in.
func (c *checker) checkArrayRootTargets() {
	names := make([]string, 0, len(c.tables))
	for name := range c.tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := c.tables[name]
		if st == nil {
			continue
		}
		pos := ast.Pos{}
		holder := name
		if st.MapEntryOf != "" {
			// a map's generated entry names the map field that holds it, so
			// `map[K]*G` is refused naming `H.m.value` (§2.8)
			holder = st.MapEntryOf
			owner, _, _ := strings.Cut(st.MapEntryOf, ".")
			if d, ok := c.astDecls[owner]; ok {
				pos = d.DeclPos()
			}
		}
		if d, ok := c.astDecls[name]; ok {
			pos = d.DeclPos()
		}
		check := func(f *ir.Field, where string) {
			if f == nil || !f.Type.Pointer {
				return
			}
			if target, ok := f.Type.Ref.(*ir.Struct); ok && target.JsonArray {
				c.errf(pos, `table %s: json = "array" refuses a table any pointer targets, and %s.%s points at it — a shared node is defined under &node (§16.7) and an array has no object for that key; hold %s by value, or drop the key (docs/SPEC-TABLES.md §16.10)`, target.Name, holder, where, target.Name)
			}
			if target, ok := f.Type.Ref.(*ir.Struct); ok && target.IsJsonValue() {
				c.errf(pos, `table %s: json = "value" refuses a table any pointer targets, and %s.%s points at it — a shared node is defined under &node (§16.7) and a raw value has no object for that key; hold %s by value, or drop the key (docs/SPEC-TABLES.md §16.13)`, target.Name, holder, where, target.Name)
			}
		}
		for _, f := range st.Fields {
			check(f, f.Name)
			if f.Type.Kind == ir.TNamed {
				if un, ok := f.Type.Ref.(*ir.Union); ok {
					for _, v := range un.Variants {
						check(v.F, f.Name+"."+v.Name)
					}
				}
			}
		}
	}
}
