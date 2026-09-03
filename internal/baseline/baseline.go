// Package baseline is the TABLES BASELINE (docs/SPEC-TABLES.md §18): an optional
// committed projection of a unit's table closure, and the check that refuses
// the edits the table wire cannot report.
//
// # WHY IT EXISTS
//
// The table wire is evolution-tolerant by construction (docs/SPEC-TABLES.md §4):
// fields may be added, removed and reordered, variants inserted anywhere,
// bounds grown, names changed under `was`. Every one of those edits is
// invisible to a reader in the good sense — nothing is lost and nothing is
// misread. Two edits are invisible in the bad sense, and they are the reason
// this file exists:
//
//   - A SPECIFIED DEFAULT is part of the wire contract. A field holding its
//     default is elided, so the reader's declared default is what the absence
//     MEANS. Change it and every file already written changes meaning, with no
//     report event, because nothing was lost or skipped.
//   - A FLAGS variant is positional. A mask carries bits, not names, so
//     inserting or reordering a variant silently remaps every stored file.
//
// The compiler retains no history and cannot see either edit on its own. The
// baseline is that history, kept where a human can read it: a text file in the
// unit directory, diffed on every check.
//
// # THE SHAPE OF THE ANSWER
//
// One representation serves rendering, parsing and diffing: a member is a list
// of fields, and a field is its name, its wire id and an ORDERED LIST OF
// TOKENS — `kind=4`, `default=21`, `bound=8`. The whole compatibility policy
// is then one table, [DefaultTokenPolicy], mapping a token key to what a
// change of it means. Adding a fact to the projection is a line in the
// renderer plus a row in that table, which is what keeps the enum-keyed array
// hook (`key=`) a one-line addition the day the construct lands.
package baseline

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Version is the baseline RENDERING's own version, on the file's first line —
// the same discipline ir.ProjectionVersion keeps for the protocol id. Bumping
// it makes every committed baseline stale at once, deliberately and visibly:
// an older file gets the "regenerate it" refusal, and `--update` regenerates
// it with its history intact, instead of a flood of diffs against facts this
// rendering spells differently.
//
// THE RULE FOR BUMPING IT: any NEW JUDGED TOKEN bumps the version. A token
// this rendering emits and an older one did not reads as an ADDITION to every
// older file, and the added-token branch of the diff refuses an addition on
// every judged row — so an unbumped rendering greets an untouched schema with
// a refusal per field. Recording a fact nothing judges (an `optional`) does
// not need a bump; adding a rule does.
const Version = 3

// FileName is the baseline's name in the unit directory. Its presence is what
// turns the check on: no file, no check.
const FileName = "tables.baseline"

// HistoryHeading opens the intentional-break log inside the file. Everything
// from this line to the end of the file is history, preserved verbatim by
// every update.
const HistoryHeading = "## history"

const magic = "schema-tables-baseline"

// A Unit is one baseline: the projection of a unit's table closure, plus the
// history the file carries.
type Unit struct {
	Version int
	Package string
	Tables  []Table
	Enums   []Enum
	Flags   []Flags
	Unions  []Union

	// History is the `## history` section's lines, verbatim and in file
	// order. Rendering writes them back untouched; an update appends.
	History []string
}

// A Table is one member of the table closure: a `table` declaration, or a
// `type` reached from one. The two are one thing on the table wire — the
// keyword that declared it changes no byte — so the baseline spells both
// `table`.
type Table struct {
	Name   string
	Fields []Field
}

// A Field is one field of a closure member: its declared name, its EFFECTIVE
// table-wire id (the `was` alias applied), and the wire facts as ordered
// tokens.
type Field struct {
	Name   string
	Id     uint16
	Tokens []Token
}

// A Token is one `key=value` wire fact on a field's line. The key is what
// [DefaultTokenPolicy] judges.
type Token struct {
	Key   string
	Value string
}

// Get returns the value of one token and whether the field carries it.
func (f Field) Get(key string) (string, bool) {
	for _, t := range f.Tokens {
		if t.Key == key {
			return t.Value, true
		}
	}
	return "", false
}

// An Enum is one enum in the closure: variants in declaration order, each with
// the wire id it rides under.
type Enum struct {
	Name     string
	Variants []Variant
}

// A Variant is one enum variant or union arm, with its name-hash id.
type Variant struct {
	Name    string
	Id      uint16
	Payload string // union arms only
}

// A Flags is one flags declaration in the closure. The ORDER IS THE FACT:
// bit i is variant i, and no name rides on the wire at all.
type Flags struct {
	Name     string
	Variants []string
}

// A Union is one union in the closure: arms in declaration order, each with
// its name-hash id and its payload type.
type Union struct {
	Name string
	Arms []Variant
}

// Render projects a checked unit's table closure. The result is exactly what
// [Unit.Text] writes and what the check diffs against; it carries no history
// (the file's own history is merged in by the update path).
func Render(u *ir.Unit) *Unit {
	out := &Unit{Version: Version, Package: u.Package}

	closure := ir.TableClosure(u)
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)

	seenEnum := map[string]bool{}
	seenFlags := map[string]bool{}
	seenUnion := map[string]bool{}

	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		t := Table{Name: name}
		// DECLARATION ORDER: st.Fields is the flattened body, branch fields
		// included. A guard removes a field from the bytes exactly as a
		// default does, and both are absorbed by the reader's default — so
		// the branch structure is deliberately not a fact here.
		for _, f := range st.Fields {
			t.Fields = append(t.Fields, renderField(f))
			// an ENUM-KEYED array's key enum is a vocabulary on the wire, not
			// just a spelling: its slots ride under their variants' NAME ids
			// (docs/SPEC-TABLES.md §3.2), so its variants are covered exactly as an
			// enum-typed field's are. It reaches the closure through KeyEnum,
			// never through the field's own type.
			if f.KeyEnumRef != nil && !seenEnum[f.KeyEnumRef.Name] {
				seenEnum[f.KeyEnumRef.Name] = true
				out.Enums = append(out.Enums, renderEnum(f.KeyEnumRef))
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				if !seenEnum[ref.Name] {
					seenEnum[ref.Name] = true
					out.Enums = append(out.Enums, renderEnum(ref))
				}
			case *ir.Flags:
				if !seenFlags[ref.Name] {
					seenFlags[ref.Name] = true
					out.Flags = append(out.Flags, Flags{Name: ref.Name, Variants: append([]string(nil), ref.Variants...)})
				}
			case *ir.Union:
				if !seenUnion[ref.Name] {
					seenUnion[ref.Name] = true
					out.Unions = append(out.Unions, renderUnion(ref))
				}
			}
		}
		out.Tables = append(out.Tables, t)
	}

	sort.Slice(out.Enums, func(i, j int) bool { return out.Enums[i].Name < out.Enums[j].Name })
	sort.Slice(out.Flags, func(i, j int) bool { return out.Flags[i].Name < out.Flags[j].Name })
	sort.Slice(out.Unions, func(i, j int) bool { return out.Unions[i].Name < out.Unions[j].Name })
	return out
}

func renderEnum(e *ir.Enum) Enum {
	out := Enum{Name: e.Name}
	for _, v := range e.Variants {
		out.Variants = append(out.Variants, Variant{Name: v, Id: ir.VariantId(v)})
	}
	return out
}

func renderUnion(un *ir.Union) Union {
	out := Union{Name: un.Name}
	for _, v := range un.Variants {
		out.Arms = append(out.Arms, Variant{Name: v.Name, Id: ir.VariantId(v.Name), Payload: v.Type})
	}
	return out
}

// renderField lists a field's wire facts. The token ORDER here is the file's
// column order; every key it can emit has a row in [DefaultTokenPolicy].
func renderField(f *ir.Field) Field {
	out := Field{Name: f.Name, Id: ir.TableFieldId(f)}
	add := func(k, v string) { out.Tokens = append(out.Tokens, Token{Key: k, Value: v}) }

	add("kind", strconv.Itoa(ir.TableFieldKind(f)))
	if elem := ir.TableElemKind(f); elem != 0 {
		add("elem", strconv.Itoa(elem))
	}
	// the referent rides under a key that says WHAT it names, because the two
	// are judged differently: a table's fields ride by id inside their own
	// body, and a vocabulary's do not ride at all (docs/SPEC-TABLES.md §4 — an
	// enum field and a plain uint16 field are both kind 7, so the runtime
	// cannot report an edit between them). See DefaultTokenPolicy.
	if f.Type.Blob() {
		// a BYTE BUFFER's referent is the blob node's shape (§2.5): a slot
		// moved between `*bytes` and `*string`, or to a `*T`, meets a record
		// of another type id and reads null (§3.1)
		if f.Type.Kind == ir.TString {
			add("type", "string")
		} else {
			add("type", "bytes")
		}
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			add("enum", f.Type.Name)
		case *ir.Flags:
			add("flags", f.Type.Name)
		case *ir.Union:
			add("union", f.Type.Name)
		default:
			add("type", f.Type.Name)
		}
	}
	// PRESENCE IS RECORDED AND JUDGED ON NOTHING (docs/SPEC-TABLES.md §18.1): a
	// field moving between T, ?T and *T moves no byte (§3.1), so the fact is
	// here to be read in a diff and nowhere in the policy.
	if f.Type.Optional {
		add("optional", "true")
	}
	switch {
	case f.KeyEnum != "":
		// an enum-keyed array is its own wire kind (§3.2) and its slots ride
		// under their variants' NAME ids, so the key enum is a referent like
		// any other — and the keyed and positional bodies can never be
		// decoded as one another, which the kind fact already says.
		add("array", "keyed")
		add("bound", strconv.FormatInt(f.ArrayBound, 10))
		add("key", f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		add("array", "fixed")
		add("bound", strconv.FormatInt(f.ArrayBound, 10))
	case f.Array == ir.ArrayCounted:
		add("array", "bounded")
		add("bound", strconv.FormatInt(f.ArrayBound, 10))
	}
	if f.Type.Kind == ir.TFixed {
		// a fixed field's SCALE. The kind fixes the width and the signedness;
		// F is the one wire-invisible fact left, and a stored raw value under
		// a moved F reads as a different number with no counter to fire —
		// the same class a changed default is in (docs/SPEC-TABLES.md §4.1, §18.1)
		add("frac", strconv.Itoa(f.Type.FracBits))
	}
	if f.Type.Size != 0 {
		add("size", strconv.FormatInt(f.Type.Size, 10))
	}
	if f.HasDefault {
		add("default", DefaultText(f))
	}
	if f.WasName != "" {
		add("was", f.WasName)
	}
	return out
}

// DefaultText renders a specified default as exact canonical text — the
// EVALUATED value, never the author's spelling, so a constant that moved
// through an expression into a default shows up as the value it now produces.
func DefaultText(f *ir.Field) string {
	switch {
	case f.DefVariant != "":
		return f.DefVariant
	case f.DefInt != nil:
		return f.DefInt.String()
	case f.Type.Kind == ir.TBool:
		return strconv.FormatBool(f.DefBool)
	default:
		// shortest round-tripping form: exact, and stable across builds. The
		// trailing ".0" is put back on a whole number so a float default
		// never reads as an integer one.
		s := strconv.FormatFloat(f.DefFloat, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEnN") {
			s += ".0"
		}
		return s
	}
}

// Text renders the baseline as the committed file: the projection, then the
// history section verbatim.
func (u *Unit) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d\n", magic, u.Version)
	fmt.Fprintf(&b, "package %s\n", u.Package)

	for _, t := range u.Tables {
		fmt.Fprintf(&b, "\ntable %s\n", t.Name)
		for _, f := range t.Fields {
			fmt.Fprintf(&b, "    field %s id=0x%04x", f.Name, f.Id)
			for _, tok := range f.Tokens {
				fmt.Fprintf(&b, " %s=%s", tok.Key, tok.Value)
			}
			b.WriteString("\n")
		}
	}
	for _, e := range u.Enums {
		fmt.Fprintf(&b, "\nenum %s\n", e.Name)
		for _, v := range e.Variants {
			fmt.Fprintf(&b, "    variant %s id=0x%04x\n", v.Name, v.Id)
		}
	}
	for _, f := range u.Flags {
		fmt.Fprintf(&b, "\nflags %s\n", f.Name)
		for i, v := range f.Variants {
			fmt.Fprintf(&b, "    variant %s bit=%d\n", v, i)
		}
	}
	for _, un := range u.Unions {
		fmt.Fprintf(&b, "\nunion %s\n", un.Name)
		for _, a := range un.Arms {
			fmt.Fprintf(&b, "    arm %s id=0x%04x payload=%s\n", a.Name, a.Id, a.Payload)
		}
	}
	if len(u.History) > 0 {
		fmt.Fprintf(&b, "\n%s\n", HistoryHeading)
		for _, line := range u.History {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Parse reads a committed baseline file. It is deliberately strict: a file
// this parser cannot read is reported rather than guessed at, because a
// misread baseline is a check that lies.
func Parse(path string, data []byte) (*Unit, error) {
	u := &Unit{}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s: empty", path)
	}

	head := strings.Fields(lines[0])
	if len(head) != 2 || head[0] != magic {
		return nil, fmt.Errorf("%s: not a schema tables baseline (its first line must read %q)", path, magic+" "+strconv.Itoa(Version))
	}
	v, err := strconv.Atoi(head[1])
	if err != nil {
		return nil, fmt.Errorf("%s: unreadable baseline version %q", path, head[1])
	}
	if v != Version {
		return nil, fmt.Errorf("%s: baseline version %d, this compiler writes %d", path, v, Version)
	}
	u.Version = v

	var cur string // the open section's kind
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == HistoryHeading {
			u.History = append(u.History, lines[i+1:]...)
			for len(u.History) > 0 && strings.TrimSpace(u.History[0]) == "" {
				u.History = u.History[1:]
			}
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if !strings.HasPrefix(line, " ") {
			// a section opener
			if len(fields) != 2 {
				return nil, fmt.Errorf("%s:%d: unreadable section line %q", path, i+1, line)
			}
			switch fields[0] {
			case "package":
				u.Package = fields[1]
			case "table":
				u.Tables = append(u.Tables, Table{Name: fields[1]})
			case "enum":
				u.Enums = append(u.Enums, Enum{Name: fields[1]})
			case "flags":
				u.Flags = append(u.Flags, Flags{Name: fields[1]})
			case "union":
				u.Unions = append(u.Unions, Union{Name: fields[1]})
			default:
				return nil, fmt.Errorf("%s:%d: unknown section %q", path, i+1, fields[0])
			}
			cur = fields[0]
			continue
		}
		if err := u.parseMemberLine(path, i+1, cur, fields); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func (u *Unit) parseMemberLine(path string, lineno int, section string, fields []string) error {
	bad := func() error { return fmt.Errorf("%s:%d: unreadable %s line", path, lineno, section) }
	if len(fields) < 2 {
		return bad()
	}
	switch section {
	case "table":
		if fields[0] != "field" || len(u.Tables) == 0 {
			return bad()
		}
		f := Field{Name: fields[1]}
		var haveId bool
		for _, tok := range fields[2:] {
			k, val, ok := strings.Cut(tok, "=")
			if !ok {
				return bad()
			}
			if k == "id" {
				id, err := strconv.ParseUint(strings.TrimPrefix(val, "0x"), 16, 16)
				if err != nil {
					return bad()
				}
				f.Id, haveId = uint16(id), true
				continue
			}
			f.Tokens = append(f.Tokens, Token{Key: k, Value: val})
		}
		// the wire id is the field's IDENTITY here — a line without one names
		// nothing the diff can match, and a file that carries such a line is
		// reported rather than half-read
		if !haveId {
			return fmt.Errorf("%s:%d: field %s carries no id= — the wire id is a field's identity in this file", path, lineno, fields[1])
		}
		t := &u.Tables[len(u.Tables)-1]
		t.Fields = append(t.Fields, f)
	case "enum", "union":
		id, err := parseIdToken(fields)
		if err != nil {
			return bad()
		}
		v := Variant{Name: fields[1], Id: id}
		if section == "enum" {
			if fields[0] != "variant" || len(u.Enums) == 0 {
				return bad()
			}
			e := &u.Enums[len(u.Enums)-1]
			e.Variants = append(e.Variants, v)
			return nil
		}
		if fields[0] != "arm" || len(u.Unions) == 0 {
			return bad()
		}
		for _, tok := range fields[2:] {
			if k, val, ok := strings.Cut(tok, "="); ok && k == "payload" {
				v.Payload = val
			}
		}
		un := &u.Unions[len(u.Unions)-1]
		un.Arms = append(un.Arms, v)
	case "flags":
		if fields[0] != "variant" || len(u.Flags) == 0 || len(fields) != 3 {
			return bad()
		}
		fl := &u.Flags[len(u.Flags)-1]
		// LINE ORDER IS THE FACT, and `bit=` states it in the file so a human
		// reading a diff does not have to count. The parser holds the two to
		// each other: a hand-edit that moves a line without moving its bit is
		// a file this parser cannot read, and it says so rather than guessing
		// which half the author meant.
		k, val, ok := strings.Cut(fields[2], "=")
		if !ok || k != "bit" {
			return bad()
		}
		bit, err := strconv.Atoi(val)
		if err != nil || bit != len(fl.Variants) {
			return fmt.Errorf("%s:%d: flags variant %s is written at bit=%s but stands at position %d — bit position is line order here, and the two disagree",
				path, lineno, fields[1], val, len(fl.Variants))
		}
		fl.Variants = append(fl.Variants, fields[1])
	default:
		return bad()
	}
	return nil
}

func parseIdToken(fields []string) (uint16, error) {
	for _, tok := range fields[2:] {
		if k, val, ok := strings.Cut(tok, "="); ok && k == "id" {
			id, err := strconv.ParseUint(strings.TrimPrefix(val, "0x"), 16, 16)
			return uint16(id), err
		}
	}
	return 0, fmt.Errorf("no id")
}
