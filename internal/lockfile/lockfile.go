// Package lockfile is the SCHEMA LOCK (docs/SPEC-TABLES.md §2.10): the
// committed record of everything a FIXED table's reader stands on, and the
// check that refuses every edit to it but an append.
//
// # WHY IT EXISTS
//
// A fixed-size table is a plain C record (§6.1). Its bytes carry no ids, no
// terminators and no names: a reader finds a field by walking to its offset,
// so the field ORDER and the field WIDTHS are the whole contract. Everything
// the id-table wire absorbs — a reordered field, a removed one, a widened one
// — a fixed table reads as garbage, silently, with no counter to fire. The
// tables baseline (§18) guards what the table wire cannot report; this guards
// what the fixed record cannot report, which is more.
//
// The owner ruled the discipline rather than the mechanism, and the mechanism
// follows from it: fixed tables evolve APPEND-ONLY — "append only, and
// deprecation possibly. might keep it light weight?" Network Next ran the
// same rule by hand for years, a version byte at the front and new fields at
// the bottom: "manual stuff, and it is a footgun, but it worked." The lock is
// that footgun taken away — the compiler owns the file, and the check is the
// hand nobody has to remember to use.
//
// # THE SHAPE OF THE ANSWER
//
// ORDER AND WIDTH ARE NOT THE WHOLE OF WHAT A READER STANDS ON, so they are
// not the whole of the file. The owner's question — "is there anything schema
// check cannot catch in a fixed table? can we fix that so it does?" — is
// answered by recording every fact a reader assumes and the declaration can
// move underneath it:
//
//   - one entry per field, in DECLARED ORDER, carrying the field's wire id,
//     its kind, its WIDTH in the record, its DEFAULT declared or implicit, its
//     declared RANGE and resolution, its `?` and its `deprecated` marker;
//   - one block per TYPE the fixed tables reach — a nested `type` record, an
//     `enum`, a `flags` mask, a `union` — because a fixed record is made of
//     those and a change inside one moves the root's bytes without moving one
//     line of the root's own entries;
//   - per block, the hash of its lines.
//
// The check is then one sentence: THE LOCK IS THE LIVE SEQUENCE, entry for
// entry and value for value. The APPEND-ONLY rule — the locked sequence is a
// PREFIX of the live one, with `deprecated` allowed only to turn on — is what
// `schema lock` will write, and what it refuses to write is what every compile
// refuses too. The two readings are [Current] and [Appendable].
//
// ONE CLASS IS BEYOND THE FILE, and §2.10 says so: a field that keeps its
// name, its id, its kind, its width, its default, its range and its `?` and
// changes only what it MEANS. Nothing recorded here moves, so nothing here can
// refuse it; the rule for that change is deprecate and add.
package lockfile

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Version is the lock RENDERING's own version, on the file's first line — the
// discipline ir.ProjectionVersion keeps for the protocol id and
// baseline.Version keeps for the baseline. Bumping it makes every committed
// lock stale at once, deliberately and visibly.
//
// THE RULE FOR BUMPING IT: any new fact on an entry line, any new block, or
// any change to what a hash digests. All three would make every committed hash
// disagree with its own lines, which is the refusal that catches a hand-edit —
// so the version is what tells the two apart.
//
// 2 is the owner's widening: the default, the range, the `?`, and a block per
// enum, flags mask, union and nested record the fixed tables reach.
const Version = 2

// FileName is the lock's name beside the unit's schema files. Unlike the
// tables baseline, its presence is not what turns the check on for a unit
// that HAS one: a unit with fixed tables and no lock is unlocked, and `schema
// lock` is what locks it.
const FileName = "schema.lock"

const magic = "schema-lock"

// The four block keywords. A block's keyword is the DECLARATION'S OWN WORD, so
// a refusal reads as the schema reads: `fixed table Config`, `type Vec3`,
// `enum Hull`, `flags Perm`, `union Payload`.
const (
	DeclFixedTable = "fixed table" // a locked table: the root of a record
	DeclType       = "type"        // a `type` a fixed record nests by value
	DeclEnum       = "enum"
	DeclFlags      = "flags"
	DeclUnion      = "union"
)

// A Unit is one lock file: every fixed table of a unit and every nested record
// its fields reach, then every enum, flags mask and union they reach — each
// group in name order.
type Unit struct {
	Version int
	Package string
	Tables  []Table
	Values  []ValueList
}

// A Table is one RECORD's locked field sequence: a FIXED table, or a `type`
// one of them nests by value.
type Table struct {
	// Decl is [DeclFixedTable] or [DeclType] — the word the declaration uses,
	// and the word the refusal uses.
	Decl string

	// Name is the record's WIRE name (§5): the `was` alias of a renamed table,
	// else its declared name. A rename keeps the identity, so it moves no
	// line in this file.
	Name string

	// Layout is the hash of Entries as [LayoutHash] takes it. It is derived,
	// and it is written down anyway: a lock whose entries have been edited by
	// hand no longer hashes to the number beside them, and that is the only
	// way this file can be caught lying.
	Layout uint64

	Entries []Entry
}

// An Entry is one field of a locked record, and it is the unit the check
// compares. EVERY FACT A READER OF THIS RECORD ASSUMES IS HERE: the record has
// no ids in it, so a field's POSITION is its identity, and everything below is
// what a reader standing at that position takes the bytes to be.
type Entry struct {
	// Name is the field's WIRE name (§5) — its `was` alias where one is
	// declared — for the same reason the record's is: a `was =` rename is not
	// a change, so it must move no byte of this file.
	Name string

	// Id is the field's table-wire id (§5). It is recorded and compared: two
	// fields of the same kind and width at one position are still two
	// different fields, and the id is what says so.
	Id uint64

	// Kind is the field's table-wire kind (§3).
	Kind int

	// Width is the field's WHOLE storage in the record, in bytes — an array's
	// elements together, a string's buffer AND its length companion, an
	// optional's value AND its presence bool, exactly as ir.FieldLayout
	// measures it (§19.3). It is the fact a fixed reader is standing on.
	Width int64

	// Default is the field's FRESH VALUE — its specified default where one is
	// declared, and the implicit zero otherwise (SPEC §5), rendered as exact
	// canonical text. It is a WIRE FACT, not a convenience: a deprecated slot
	// holds it, an absent field on the id-table wire reads back as it, and a
	// reader given an older writer's record fills the missing field from it.
	// Move it and every such record means something else.
	Default string

	// Min and Max are the field's DECLARED range, "" when it declares none,
	// and Res is the compressed-float resolution beside them, "" on an integer
	// range. A stored value is read back against these bounds, so moving them
	// reads every record already written at a different scale.
	Min, Max, Res string

	// Frac is a fixed-point field's RESOLUTION, spelled as the fractional bits
	// its raw integer is scaled by, and -1 on every field that is not
	// fixed-point. The kind fixes the width and the signedness and says
	// nothing about F, so a moved F reads a stored raw value as a different
	// number with no counter to fire (§4.3).
	Frac int

	// Optional is the field's `?` (§2.3): a presence bool beside the value,
	// inside the record. A reader that expects one where none is written
	// stands on the next field's first byte.
	Optional bool

	// Deprecated is the field's `| deprecated` marker (§2.10). It may turn
	// on and it may never turn off: the slot stays where it is, nothing new
	// may name the field, and a reader ignores what it finds there.
	Deprecated bool
}

// A ValueList is one enum, flags mask or union a fixed table reaches: its
// values in DECLARED ORDER, which is the order the storage is numbered by. In
// a fixed record an enum rides as its dense ordinal, a flags bit is its
// position, and a union's tag is its arm's position — so the list IS the
// mapping from a stored number to a meaning, and it evolves append-only for
// exactly the reason a field sequence does.
type ValueList struct {
	// Decl is [DeclEnum], [DeclFlags] or [DeclUnion].
	Decl string
	// Name is the declaration's name.
	Name string
	// Hash is the hash of Values as [ValuesHash] takes it — the same statement
	// twice that a record's layout hash is.
	Hash uint64
	// Values are the variant or arm WIRE names in declared order (§5), so a
	// `was` rename moves no line here either.
	Values []string
}

// Child is the word one value of this list is called: a union has ARMS and an
// enum or a flags mask has VARIANTS.
func (v *ValueList) Child() string {
	if v.Decl == DeclUnion {
		return "arm"
	}
	return "variant"
}

// FixedTables names the unit's FIXED tables — the roots this file locks — in
// name order.
//
// THIS IS THE ONE PLACE THE SET IS DECIDED. Today it is derived: a table is
// fixed when it is not variable-length (ir.VariableTables, §2.2), because
// that is the only answer the language can give. The sibling work adding the
// `fixed table` keyword makes the flag DECLARED, and when it lands this
// function's body is the one line that changes.
//
// TODO(fixed-table-keyword): read the declared flag — `st.IsFixedTable` on
// the `fixed table` keyword branch — instead of deriving the answer here.
func FixedTables(u *ir.Unit) []string {
	variable := ir.VariableTables(u)
	var out []string
	for name, st := range u.Tables {
		if st.IsMapEntry() {
			// a generated map entry is not a declaration anybody wrote, and
			// its holder is variable by construction (§2.8)
			continue
		}
		if variable[name] {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// closure is everything the unit's FIXED tables REACH, which is everything
// their records are made of. The root alone is not the layout: a `type` nested
// by value contributes its own fields' bytes, an enum contributes the meaning
// of a stored ordinal, a flags mask the meaning of a bit, a union the meaning
// of a tag. A change inside any of them moves what a reader of the ROOT gets
// back without moving one line of the root's entries, so each is locked in its
// own block.
type closure struct {
	structs []*ir.Struct // the `type` records — the tables are roots already
	enums   []*ir.Enum
	flags   []*ir.Flags
	unions  []*ir.Union
}

// reach walks the fixed tables' by-value edges. A pointer edge cannot appear
// here at all — a pointer is what makes its owner VARIABLE-LENGTH
// (ir.VariableTables) — so every edge this walk crosses is one whose bytes are
// inside the root's own record.
func reach(u *ir.Unit) *closure {
	c := &closure{}
	seenStruct := map[*ir.Struct]bool{}
	seenEnum := map[*ir.Enum]bool{}
	seenFlags := map[*ir.Flags]bool{}
	seenUnion := map[*ir.Union]bool{}

	var walkFields func(fields []*ir.Field)
	var walkStruct func(st *ir.Struct)
	var walkUnion func(un *ir.Union)

	walkStruct = func(st *ir.Struct) {
		if st == nil || seenStruct[st] {
			return
		}
		seenStruct[st] = true
		// a nested `table` is a fixed table of this unit and already a root
		// (FixedTables lists every one of them); only a `type` needs a block
		// of its own
		if !st.IsTable {
			c.structs = append(c.structs, st)
		}
		walkFields(st.Fields)
	}
	walkUnion = func(un *ir.Union) {
		if un == nil || seenUnion[un] {
			return
		}
		seenUnion[un] = true
		c.unions = append(c.unions, un)
		for _, v := range un.Variants {
			if v.F != nil {
				walkFields([]*ir.Field{v.F})
			}
		}
	}
	walkFields = func(fields []*ir.Field) {
		for _, f := range fields {
			// an enum-keyed array's KEY reaches the enum as surely as a field
			// of that type does (§2.4), and its Max is the array's length
			if f.KeyEnumRef != nil && !seenEnum[f.KeyEnumRef] {
				seenEnum[f.KeyEnumRef] = true
				c.enums = append(c.enums, f.KeyEnumRef)
			}
			if f.IsMap() {
				walkStruct(f.MapEntry)
				continue
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walkStruct(ref)
			case *ir.Enum:
				if !seenEnum[ref] {
					seenEnum[ref] = true
					c.enums = append(c.enums, ref)
				}
			case *ir.Flags:
				if !seenFlags[ref] {
					seenFlags[ref] = true
					c.flags = append(c.flags, ref)
				}
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}

	for _, name := range FixedTables(u) {
		walkStruct(u.Tables[name])
	}
	sort.Slice(c.structs, func(i, j int) bool { return c.structs[i].Name < c.structs[j].Name })
	sort.Slice(c.enums, func(i, j int) bool { return c.enums[i].Name < c.enums[j].Name })
	sort.Slice(c.flags, func(i, j int) bool { return c.flags[i].Name < c.flags[j].Name })
	sort.Slice(c.unions, func(i, j int) bool { return c.unions[i].Name < c.unions[j].Name })
	return c
}

// Render projects a checked unit's fixed tables and their whole closure. The
// result is exactly what [Unit.Text] writes and what the check compares
// against.
func Render(u *ir.Unit) *Unit {
	out := &Unit{Version: Version, Package: u.Package}
	for _, name := range FixedTables(u) {
		out.Tables = append(out.Tables, renderTable(u, DeclFixedTable, u.Tables[name]))
	}
	c := reach(u)
	for _, st := range c.structs {
		out.Tables = append(out.Tables, renderTable(u, DeclType, st))
	}
	for _, e := range c.enums {
		vals := make([]string, len(e.Variants))
		for i := range e.Variants {
			vals[i] = e.VariantWireName(i)
		}
		out.Values = append(out.Values, newValueList(DeclEnum, e.Name, vals))
	}
	for _, fl := range c.flags {
		out.Values = append(out.Values, newValueList(DeclFlags, fl.Name, append([]string(nil), fl.Variants...)))
	}
	for _, un := range c.unions {
		vals := make([]string, len(un.Variants))
		for i, v := range un.Variants {
			vals[i] = v.WireName()
		}
		out.Values = append(out.Values, newValueList(DeclUnion, un.Name, vals))
	}
	return out
}

func newValueList(decl, name string, values []string) ValueList {
	v := ValueList{Decl: decl, Name: name, Values: values}
	v.Hash = ValuesHash(&v)
	return v
}

func renderTable(u *ir.Unit, decl string, st *ir.Struct) Table {
	t := Table{Decl: decl, Name: st.WireName()}
	layout := ir.RecordLayout(u, st)
	for _, f := range st.Fields {
		e := Entry{
			Name:       fieldWireName(f),
			Id:         ir.TableFieldWireId(f),
			Kind:       ir.TableFieldKind(f),
			Default:    defaultText(f),
			Frac:       -1,
			Optional:   f.Type.Optional,
			Deprecated: f.Deprecated,
		}
		switch {
		case f.HasIntRange && f.IntMin != nil && f.IntMax != nil:
			e.Min, e.Max = f.IntMin.String(), f.IntMax.String()
		case f.HasFloatRange:
			e.Min, e.Max, e.Res = floatText(f.FMin), floatText(f.FMax), floatText(f.Resolution)
		}
		if f.Type.Kind == ir.TFixed {
			e.Frac = f.Type.FracBits
		}
		if fl := layout.FieldByName(f.Name); fl != nil {
			e.Width = fl.Size
		}
		t.Entries = append(t.Entries, e)
	}
	t.Layout = LayoutHash(t.Entries)
	return t
}

// fieldWireName is the name this file records: the `was` alias where one is
// declared, so a rename moves nothing (§5).
func fieldWireName(f *ir.Field) string {
	if f.WasName != "" {
		return f.WasName
	}
	return f.Name
}

// defaultText is the field's FRESH VALUE as exact canonical text — the
// specified default where one is declared, and the implicit zero otherwise
// (SPEC §5). It is the EVALUATED value and never the author's spelling, so a
// constant that moved through an expression into a default shows up as the
// value it now produces.
//
// Every field renders something: "no default declared" is not the absence of a
// fact, it is the fact that a fresh record holds zero there, and a declaration
// that gives that slot a value later has changed what an older writer's
// missing field reads back as.
func defaultText(f *ir.Field) string {
	if f.IsMap() || f.Array != ir.ArrayNone {
		// an array takes no specified default; its one birth value is the
		// COUNT a fresh value carries, the declared minimum (SPEC §4.6)
		return "born:" + strconv.FormatInt(f.BornCount(), 10)
	}
	switch f.Type.Kind {
	case ir.TBool:
		return strconv.FormatBool(f.HasDefault && f.DefBool)
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			return floatText(f.DefFloat)
		}
		return floatText(0)
	case ir.TString, ir.TWString, ir.TBytes:
		// the bytes themselves, hex-spelled so a space in a default cannot
		// split the token (SPEC §4.2)
		if f.HasDefault {
			return "bytes:" + hex.EncodeToString(f.DefBytes)
		}
		return "bytes:"
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// the implicit None at 0 is what a fresh enum field holds, and a
			// named default records the variant's WIRE name so a rename moves
			// no line (§5)
			if f.HasDefault && f.DefVariant != "" {
				return "variant:" + ref.VariantWireNameOf(f.DefVariant)
			}
			return "variant:None"
		case *ir.Flags:
			return "mask:" + intText(f)
		}
		// a nested record and a union hold their own fresh value, and their
		// own block is where it is recorded
		return "zero"
	default:
		// TInt, TBits and TFixed — a fixed field's RAW value
		return intText(f)
	}
}

func intText(f *ir.Field) string {
	if f.HasDefault && f.DefInt != nil {
		return f.DefInt.String()
	}
	return "0"
}

// floatText is the shortest round-tripping form of a float fact: exact, and
// stable across builds. The trailing ".0" is put back on a whole number so a
// float never reads as an integer one.
func floatText(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
}

// LayoutHash digests an entry sequence with the wire's own hash — one hash in
// this tree, and this file has no reason to be the second (§5).
//
// It digests the ENTRY LINES as they are written, so the number in the file
// and the entries above it are the same statement twice; that is what makes a
// hand-edit of either half visible.
func LayoutHash(entries []Entry) uint64 {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.line())
		b.WriteByte('\n')
	}
	return ir.TableWireId(b.String())
}

// ValuesHash is [LayoutHash] for a value list, over the same lines the file
// carries and for the same reason.
func ValuesHash(v *ValueList) uint64 {
	var b strings.Builder
	for _, s := range v.Values {
		b.WriteString(v.Child())
		b.WriteByte(' ')
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return ir.TableWireId(b.String())
}

// line is one entry's text, and the unit the layout hash digests.
func (e Entry) line() string {
	s := fmt.Sprintf("field %s id=0x%016x kind=%d width=%d default=%s", e.Name, e.Id, e.Kind, e.Width, e.Default)
	if e.Min != "" || e.Max != "" {
		s += " min=" + e.Min + " max=" + e.Max
	}
	if e.Res != "" {
		s += " res=" + e.Res
	}
	if e.Frac >= 0 {
		s += " frac=" + strconv.Itoa(e.Frac)
	}
	if e.Optional {
		s += " optional"
	}
	if e.Deprecated {
		s += " deprecated"
	}
	return s
}

// rangeText is an entry's range as a refusal says it, and "unranged" when the
// field declares none.
func (e Entry) rangeText() string {
	var parts []string
	if e.Min != "" || e.Max != "" {
		parts = append(parts, "["+e.Min+", "+e.Max+"]")
	}
	if e.Res != "" {
		parts = append(parts, "resolution "+e.Res)
	}
	if e.Frac >= 0 {
		parts = append(parts, strconv.Itoa(e.Frac)+" fractional bits")
	}
	if len(parts) == 0 {
		return "unranged"
	}
	return strings.Join(parts, " at ")
}

// sameRange reports whether two entries agree on every bound and every scale.
func (e Entry) sameRange(o Entry) bool {
	return e.Min == o.Min && e.Max == o.Max && e.Res == o.Res && e.Frac == o.Frac
}

// Text renders the lock as the committed file.
func (u *Unit) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d\n", magic, u.Version)
	fmt.Fprintf(&b, "package %s\n", u.Package)
	for _, t := range u.Tables {
		fmt.Fprintf(&b, "\n%s %s layout=0x%016x\n", t.Decl, t.Name, t.Layout)
		for _, e := range t.Entries {
			fmt.Fprintf(&b, "    %s\n", e.line())
		}
	}
	for _, v := range u.Values {
		fmt.Fprintf(&b, "\n%s %s values=0x%016x\n", v.Decl, v.Name, v.Hash)
		for _, s := range v.Values {
			fmt.Fprintf(&b, "    %s %s\n", v.Child(), s)
		}
	}
	return b.String()
}

// Table returns one locked record by name.
func (u *Unit) Table(name string) *Table {
	for i := range u.Tables {
		if u.Tables[i].Name == name {
			return &u.Tables[i]
		}
	}
	return nil
}

// List returns one locked enum, flags mask or union by name.
func (u *Unit) List(name string) *ValueList {
	for i := range u.Values {
		if u.Values[i].Name == name {
			return &u.Values[i]
		}
	}
	return nil
}

// Parse reads a committed lock. Every refusal names the file and the line, and
// the remedy is always the same one command.
func Parse(path string, data []byte) (*Unit, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	u := &Unit{}
	var cur *Table
	var curList *ValueList
	for n, raw := range lines {
		where := fmt.Sprintf("%s:%d", path, n+1)
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case n == 0:
			if len(fields) != 2 || fields[0] != magic {
				return nil, fmt.Errorf("%s: a %s begins with %q and its rendering version", where, FileName, magic)
			}
			v, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a rendering version", where, fields[1])
			}
			if v != Version {
				// THE ONE PARSE REFUSAL THAT IS NOT A HAND-EDIT. The
				// rendering version is the compiler's own, so there is
				// nothing here for a person to repair: the file is
				// regenerated from the declaration and nothing in it is
				// salvage. Say the remedy that works, because the wrappers
				// around this error say "write it with `schema lock`" and
				// this is the one case where that command needs the old file
				// gone first.
				return nil, fmt.Errorf("%s: this lock is rendering version %d and this compiler writes version %d — the rendering version is the compiler's own and this file holds nothing a hand can carry forward: delete it and write it again with `schema lock`", where, v, Version)
			}
			u.Version = v
		case fields[0] == "package":
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s: the package line names one package", where)
			}
			u.Package = fields[1]
		case fields[0] == "fixed" && len(fields) > 1 && fields[1] == "table":
			if len(fields) != 4 {
				return nil, fmt.Errorf("%s: a table line is `fixed table <Name> layout=0x...`", where)
			}
			h, err := parseHex(fields[3], "layout")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			u.Tables = append(u.Tables, Table{Decl: DeclFixedTable, Name: fields[2], Layout: h})
			cur, curList = &u.Tables[len(u.Tables)-1], nil
		case fields[0] == DeclType:
			if len(fields) != 3 {
				return nil, fmt.Errorf("%s: a nested record line is `type <Name> layout=0x...`", where)
			}
			h, err := parseHex(fields[2], "layout")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			u.Tables = append(u.Tables, Table{Decl: DeclType, Name: fields[1], Layout: h})
			cur, curList = &u.Tables[len(u.Tables)-1], nil
		case fields[0] == DeclEnum, fields[0] == DeclFlags, fields[0] == DeclUnion:
			if len(fields) != 3 {
				return nil, fmt.Errorf("%s: a value-list line is `%s <Name> values=0x...`", where, fields[0])
			}
			h, err := parseHex(fields[2], "values")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			u.Values = append(u.Values, ValueList{Decl: fields[0], Name: fields[1], Hash: h})
			cur, curList = nil, &u.Values[len(u.Values)-1]
		case fields[0] == "field":
			if cur == nil {
				return nil, fmt.Errorf("%s: a field line before any record line", where)
			}
			e, err := parseEntry(fields)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			cur.Entries = append(cur.Entries, e)
		case fields[0] == "variant", fields[0] == "arm":
			if curList == nil {
				return nil, fmt.Errorf("%s: a %s line before any `enum`, `flags` or `union` line", where, fields[0])
			}
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s: a %s line names one value", where, fields[0])
			}
			if fields[0] != curList.Child() {
				return nil, fmt.Errorf("%s: a %s carries %s lines, not %s lines", where, curList.Decl, curList.Child(), fields[0])
			}
			curList.Values = append(curList.Values, fields[1])
		default:
			return nil, fmt.Errorf("%s: %q is not a line a %s carries", where, fields[0], FileName)
		}
	}
	if u.Version == 0 {
		return nil, fmt.Errorf("%s: empty — a %s begins with %q and its rendering version", path, FileName, magic)
	}
	return u, nil
}

func parseEntry(fields []string) (Entry, error) {
	if len(fields) < 6 {
		return Entry{}, fmt.Errorf("a field line is `field <name> id=0x... kind=N width=N default=V [min=V max=V] [res=V] [frac=N] [optional] [deprecated]`")
	}
	e := Entry{Name: fields[1], Frac: -1}
	for _, tok := range fields[2:] {
		key, val, valued := strings.Cut(tok, "=")
		switch {
		case !valued && key == "optional":
			e.Optional = true
		case !valued && key == "deprecated":
			e.Deprecated = true
		case !valued:
			return Entry{}, fmt.Errorf("%q is not a fact a field line carries", tok)
		case key == "id":
			h, err := parseHex(tok, "id")
			if err != nil {
				return Entry{}, err
			}
			e.Id = h
		case key == "kind":
			n, err := strconv.Atoi(val)
			if err != nil {
				return Entry{}, fmt.Errorf("kind=%q is not a number", val)
			}
			e.Kind = n
		case key == "width":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return Entry{}, fmt.Errorf("width=%q is not a number", val)
			}
			e.Width = n
		case key == "default":
			e.Default = val
		case key == "min":
			e.Min = val
		case key == "max":
			e.Max = val
		case key == "res":
			e.Res = val
		case key == "frac":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return Entry{}, fmt.Errorf("frac=%q is not a count of fractional bits", val)
			}
			e.Frac = n
		default:
			return Entry{}, fmt.Errorf("%q is not a fact a field line carries", key)
		}
	}
	return e, nil
}

func parseHex(tok, key string) (uint64, error) {
	_, val, ok := strings.Cut(tok, "=")
	if !ok {
		return 0, fmt.Errorf("%s takes a hex value, written %s=0x0000000000000000", key, key)
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(val, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a 64-bit hex value", key, val)
	}
	return v, nil
}
