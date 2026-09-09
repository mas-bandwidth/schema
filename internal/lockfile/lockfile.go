// Package lockfile is the SCHEMA LOCK (docs/SPEC-TABLES.md §2.10): the
// committed record of every FIXED table's field sequence, and the check that
// refuses every edit to it but an append.
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
// One entry per field, in DECLARED ORDER, carrying the four facts a fixed
// record's layout is made of: the field's wire id, its kind, its WIDTH in the
// record, and whether it is deprecated. Per table, the hash of that sequence.
// The check is then one sentence: THE LOCKED SEQUENCE IS A PREFIX OF THE
// LIVE ONE, entry for entry, with `deprecated` allowed only to turn on.
package lockfile

import (
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
// THE RULE FOR BUMPING IT: any new fact on an entry line, or any change to
// what the layout hash digests. Both would make every committed hash
// disagree with its own entries, which is the refusal that catches a
// hand-edit — so the version is what tells the two apart.
const Version = 1

// FileName is the lock's name beside the unit's schema files. Unlike the
// tables baseline, its presence is not what turns the check on for a unit
// that HAS one: a unit with fixed tables and no lock is unlocked, and `schema
// lock` is what locks it.
const FileName = "schema.lock"

const magic = "schema-lock"

// A Unit is one lock file: every fixed table of a unit, in name order.
type Unit struct {
	Version int
	Package string
	Tables  []Table
}

// A Table is one FIXED table's locked field sequence.
type Table struct {
	// Name is the table's WIRE name (§5): the `was` alias of a renamed table,
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

// An Entry is one field of a fixed table, and it is the unit the check
// compares. Everything a fixed record's layout is made of is here and nothing
// else: the record has no ids in it, so a field's POSITION is its identity,
// and the id, kind and width are what a reader at that position assumes.
type Entry struct {
	// Name is the field's WIRE name (§5) — its `was` alias where one is
	// declared — for the same reason the table's is: a `was =` rename is not
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

	// Deprecated is the field's `| deprecated` marker (§2.10). It may turn
	// on and it may never turn off: the slot stays where it is, nothing new
	// may name the field, and a reader ignores what it finds there.
	Deprecated bool
}

// FixedTables names the unit's FIXED tables — the ones this file locks — in
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

// Render projects a checked unit's fixed tables. The result is exactly what
// [Unit.Text] writes and what the check compares against.
func Render(u *ir.Unit) *Unit {
	out := &Unit{Version: Version, Package: u.Package}
	for _, name := range FixedTables(u) {
		st := u.Tables[name]
		out.Tables = append(out.Tables, renderTable(u, st))
	}
	return out
}

func renderTable(u *ir.Unit, st *ir.Struct) Table {
	t := Table{Name: st.WireName()}
	layout := ir.RecordLayout(u, st)
	for _, f := range st.Fields {
		e := Entry{
			Name:       fieldWireName(f),
			Id:         ir.TableFieldWireId(f),
			Kind:       ir.TableFieldKind(f),
			Deprecated: f.Deprecated,
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

// line is one entry's text, and the unit the layout hash digests.
func (e Entry) line() string {
	s := fmt.Sprintf("field %s id=0x%016x kind=%d width=%d", e.Name, e.Id, e.Kind, e.Width)
	if e.Deprecated {
		s += " deprecated"
	}
	return s
}

// Text renders the lock as the committed file.
func (u *Unit) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d\n", magic, u.Version)
	fmt.Fprintf(&b, "package %s\n", u.Package)
	for _, t := range u.Tables {
		fmt.Fprintf(&b, "\nfixed table %s layout=0x%016x\n", t.Name, t.Layout)
		for _, e := range t.Entries {
			fmt.Fprintf(&b, "    %s\n", e.line())
		}
	}
	return b.String()
}

// Table returns one locked table by wire name.
func (u *Unit) Table(name string) *Table {
	for i := range u.Tables {
		if u.Tables[i].Name == name {
			return &u.Tables[i]
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
				return nil, fmt.Errorf("%s: this lock is rendering version %d and this compiler writes version %d", where, v, Version)
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
			u.Tables = append(u.Tables, Table{Name: fields[2], Layout: h})
			cur = &u.Tables[len(u.Tables)-1]
		case fields[0] == "field":
			if cur == nil {
				return nil, fmt.Errorf("%s: a field line before any `fixed table` line", where)
			}
			e, err := parseEntry(fields)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			cur.Entries = append(cur.Entries, e)
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
	if len(fields) < 5 {
		return Entry{}, fmt.Errorf("a field line is `field <name> id=0x... kind=N width=N [deprecated]`")
	}
	e := Entry{Name: fields[1]}
	for _, tok := range fields[2:] {
		key, val, valued := strings.Cut(tok, "=")
		switch {
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
