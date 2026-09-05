// Package tablepack is `schema pack` and `schema unpack` (docs/SPEC-TABLES.md
// §17): a directory tree that MIRRORS a root table becomes one table instance,
// and the root's wire bytes come out — no magic, no content hash, no protocol
// id, no length prefix around the whole.
//
// It adds no format. The tree rule is STRUCTURAL only — it says where a value
// lives, never what a value means — and every rule about kinds, presence,
// clamping and the report belongs to §16 and lives in tabletext.
package tablepack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Refusals is the list of tree-shape refusals a pack collected — a directory
// or file naming no field, two entries claiming one value, a variant name the
// enum does not have (docs/SPEC-TABLES.md §17.3). They are collected rather than
// thrown one at a time, so a pack of a hundred files reports once.
type Refusals []string

func (r Refusals) Error() string { return strings.Join(r, "\n") }

type packer struct {
	m        *tabletext.Model
	report   *tabletext.Report
	refusals Refusals
	skipped  []string // hidden non-JSON files the tree walk passed over
}

func (p *packer) list(dir string) ([]os.DirEntry, error) {
	entries, skipped, err := readDir(dir)
	p.skipped = append(p.skipped, skipped...)
	return entries, err
}

func (p *packer) refusef(format string, args ...any) {
	p.refusals = append(p.refusals, fmt.Sprintf(format, args...))
}

// Pack assembles ONE instance of the named root table from the tree under dir
// and returns the root's wire bytes (docs/SPEC-TABLES.md §17), the hidden non-JSON
// files the walk passed over, and the report — which aggregates everything §16
// counts across the whole tree.
func Pack(m *tabletext.Model, root, dir string) ([]byte, []string, tabletext.Report, error) {
	st := m.Unit.Tables[root]
	if st == nil {
		return nil, nil, tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", root, strings.Join(m.Roots(), ", "))
	}
	inst := m.New(st)
	p := &packer{m: m, report: &tabletext.Report{}}
	if err := p.rootTree(inst, root, dir); err != nil {
		return nil, p.skipped, *p.report, err
	}
	if len(p.refusals) > 0 {
		return nil, p.skipped, *p.report, p.refusals
	}
	bytes, err := tablewire.Encode(m, inst)
	if err != nil {
		return nil, p.skipped, *p.report, err
	}
	return bytes, p.skipped, *p.report, nil
}

// PackMessage is Pack over the MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the
// same tree, the same instance and the same walk, and a BATCH OF ONE comes out
// — the form byte, the count, and one bitpacked body, whose ids live in the
// announced table rather than in a trailer of this wire's.
//
// The tree is unchanged, which is the text form's own claim: a message's text
// is its file form's text, byte for byte.
func PackMessage(m *tabletext.Model, root, dir string) ([]byte, []string, tabletext.Report, error) {
	return PackMessages(m, []MessageTree{{Root: root, Dir: dir}})
}

// MessageTree is one message of a batch: the root it is and the tree that
// holds it.
type MessageTree struct {
	Root string
	Dir  string
}

// PackMessages is the BATCH, which is the message form's primitive
// (docs/SPEC-TABLES.md §3.3): every tree is read into its own instance and the
// whole batch is written as ONE buffer, one count and one continuous bit
// stream. Which root each message is, is the caller's, exactly as it is the
// application's on the wire.
func PackMessages(m *tabletext.Model, trees []MessageTree) ([]byte, []string, tabletext.Report, error) {
	p := &packer{m: m, report: &tabletext.Report{}}
	insts := make([]*tabletext.Instance, 0, len(trees))
	for _, tree := range trees {
		st := m.Unit.Tables[tree.Root]
		if st == nil {
			return nil, nil, tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", tree.Root, strings.Join(m.Roots(), ", "))
		}
		inst := m.New(st)
		if err := p.rootTree(inst, tree.Root, tree.Dir); err != nil {
			return nil, p.skipped, *p.report, err
		}
		insts = append(insts, inst)
	}
	if len(p.refusals) > 0 {
		return nil, p.skipped, *p.report, p.refusals
	}
	bytes, err := tablewire.EncodeMessages(m, insts)
	if err != nil {
		return nil, p.skipped, *p.report, err
	}
	return bytes, p.skipped, *p.report, nil
}

// rootTree reads the tree at dir into the root instance. The root may simply
// be one `<Root>.json` (§17.2's last rule); anything beside that file is a
// second claim on the root and is refused rather than merged.
//
// A VARIABLE root is that one file and nothing else (§17.2): a shared node is
// named by an id a TEXT owns (§16.7), and a tree of fields would split the
// root across texts that cannot name each other's nodes. A tree of fields
// under a variable root is refused by name before a file is read.
func (p *packer) rootTree(inst *tabletext.Instance, root, dir string) error {
	entries, err := p.list(dir)
	if err != nil {
		return err
	}
	whole := root + ".json"
	for _, e := range entries {
		if e.Name() != whole {
			continue
		}
		if len(entries) > 1 {
			p.refusef("%s: %s is the whole root, and %s beside it claims it too — a root is one file or one tree of fields, never both (docs/SPEC-TABLES.md §17.2)",
				dir, whole, plural(len(entries)-1, "other entry", "other entries"))
			return nil
		}
		text, err := os.ReadFile(filepath.Join(dir, whole))
		if err != nil {
			return err
		}
		p.readTableText(inst, filepath.Join(dir, whole), text)
		return nil
	}
	if p.m.IsVariable(root) {
		p.refusef("%s: %s is VARIABLE-LENGTH and packs from one %s — its shared nodes are named by labels a text owns, so a tree of fields cannot carry it (docs/SPEC-TABLES.md §16.7, §17.2)",
			dir, root, whole)
		return nil
	}
	p.tableDir(inst, dir, entries)
	return nil
}

// tableDir reads a directory whose entries are a table's fields: a
// `<field>.json` is that field's value verbatim, a `<field>/` directory holds
// it structurally, and anything else names no field.
func (p *packer) tableDir(inst *tabletext.Instance, dir string, entries []os.DirEntry) {
	claimed := map[string]string{}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		key, ok := entryKey(e)
		if !ok {
			p.refusef("%s: names no field — a tree mirrors the table and holds `<field>.json` files and `<field>` directories only (docs/SPEC-TABLES.md §17.1)", path)
			continue
		}
		fv, known := inst.FieldByKey(key)
		if !known {
			p.refusef("%s: %q names no field of table %s (docs/SPEC-TABLES.md §17.3)", path, key, inst.Def.Name)
			continue
		}
		if prev, dup := claimed[key]; dup {
			p.refusef("%s and %s both claim field %q of table %s — one value, one place (docs/SPEC-TABLES.md §17.3)", prev, path, key, inst.Def.Name)
			continue
		}
		claimed[key] = path
		if e.IsDir() {
			p.fieldDir(fv, path)
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			p.refusef("%s: %v", path, err)
			continue
		}
		p.readFieldText(fv, path, text)
	}
}

// fieldDir reads a `<field>/` directory, whose meaning is the field's own
// shape: an enum-keyed array holds one `<Variant>.json` per slot, a plain
// array holds files in NAME ORDER as its elements, and a nested table holds a
// directory of its own fields (docs/SPEC-TABLES.md §17.1).
func (p *packer) fieldDir(fv *tabletext.Field, dir string) {
	entries, err := p.list(dir)
	if err != nil {
		p.refusef("%s: %v", dir, err)
		return
	}
	f := fv.Def
	switch {
	case f.KeyEnum != "":
		p.keyedDir(fv, dir, entries)
	case f.Array != ir.ArrayNone:
		p.arrayDir(fv, dir, entries)
	case tabletext.StructOf(f) != nil:
		if fv.Cell.Tab == nil {
			fv.Cell.Tab = p.m.New(tabletext.StructOf(f))
		}
		p.tableDir(fv.Cell.Tab, dir, entries)
		if f.Type.Optional {
			// a directory is the field being there, exactly as a key is
			// (docs/SPEC-TABLES.md §16.2)
			fv.Present = true
		}
	default:
		p.refusef("%s: field %q is a %s, and only a nested table, an array or an enum-keyed array has a directory form (docs/SPEC-TABLES.md §17.1)",
			dir, ir.TableFieldJsonKey(f), fieldShapeName(f))
	}
}

// keyedDir reads one `<Variant>.json` per slot. A variant name the enum does
// not have is refused naming the file, and None keys no record, so there is no
// `None.json` (§3.2).
func (p *packer) keyedDir(fv *tabletext.Field, dir string, entries []os.DirEntry) {
	f := fv.Def
	claimed := map[int]string{}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		name, ok := entryKey(e)
		if !ok || e.IsDir() {
			p.refusef("%s: an enum-keyed array's directory holds one `<Variant>.json` per slot and nothing else (docs/SPEC-TABLES.md §17.1)", path)
			continue
		}
		value := tabletext.EnumValue(f.KeyEnumRef, name)
		slot := tabletext.KeyedValueSlot(f, value)
		if slot < 0 {
			if value == 0 {
				p.refusef("%s: None keys no record, so an enum-keyed array has no None slot and no `None.json` (docs/SPEC-TABLES.md §3.2)", path)
			} else {
				p.refusef("%s: %q is not a variant of enum %s (docs/SPEC-TABLES.md §17.3)", path, name, f.KeyEnum)
			}
			continue
		}
		if prev, dup := claimed[slot]; dup {
			p.refusef("%s and %s both claim the %s slot of %q (docs/SPEC-TABLES.md §17.3)", prev, path, name, ir.TableFieldJsonKey(f))
			continue
		}
		claimed[slot] = path
		text, err := os.ReadFile(path)
		if err != nil {
			p.refusef("%s: %v", path, err)
			continue
		}
		var r tabletext.Report
		p.m.ReadElement(fv, slot, text, &r)
		p.report.Add(r)
		if r.Malformed {
			p.refusef("%s: not one JSON value for the %s slot of %q (docs/SPEC-TABLES.md §17.3)", path, name, ir.TableFieldJsonKey(f))
		}
	}
}

// arrayDir reads files in NAME ORDER as the array's elements (§17.1).
func (p *packer) arrayDir(fv *tabletext.Field, dir string, entries []os.DirEntry) {
	f := fv.Def
	placed := 0
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if _, ok := entryKey(e); !ok || e.IsDir() {
			p.refusef("%s: an array's directory holds `.json` element files and nothing else (docs/SPEC-TABLES.md §17.1)", path)
			continue
		}
		if placed >= int(f.ArrayBound) {
			// more elements than the reader's bound: the bounded prefix is
			// kept and the excess counts, the wire's rule (§4)
			p.report.Clamped++
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			p.refusef("%s: %v", path, err)
			continue
		}
		var r tabletext.Report
		p.m.ReadElement(fv, placed, text, &r)
		p.report.Add(r)
		if r.Malformed {
			p.refusef("%s: not one JSON value for an element of %q (docs/SPEC-TABLES.md §17.3)", path, ir.TableFieldJsonKey(f))
		}
		placed++
	}
	if f.CountedOnWire() {
		fv.Count = placed
	}
}

func (p *packer) readTableText(inst *tabletext.Instance, path string, text []byte) {
	var r tabletext.Report
	p.m.Read(inst, text, &r)
	p.report.Add(r)
	if r.Malformed {
		p.refusef("%s: not one JSON text for table %s (docs/SPEC-TABLES.md §17.3)", path, inst.Def.Name)
	}
}

func (p *packer) readFieldText(fv *tabletext.Field, path string, text []byte) {
	var r tabletext.Report
	p.m.ReadValue(fv, text, &r)
	p.report.Add(r)
	if r.Malformed {
		p.refusef("%s: not one JSON value for field %q (docs/SPEC-TABLES.md §17.3)", path, ir.TableFieldJsonKey(fv.Def))
	}
}

// entryKey is the field key or variant name an entry names: a directory's own
// name, or a file's name without its `.json` suffix. The bool is false for
// anything else, which names no field.
func entryKey(e os.DirEntry) (string, bool) {
	name := e.Name()
	if e.IsDir() {
		return name, true
	}
	if key, ok := strings.CutSuffix(name, ".json"); ok {
		return key, true
	}
	return "", false
}

// readDir lists a directory in name order, and reports the entries it did not
// return. The ONE thing a tree walk passes over is a hidden file that is not a
// `.json`: a tool that refused `.DS_Store` would be a tool nobody could run on
// a checkout. A hidden `.json` file and a hidden directory still name
// something, so they go through the ordinary rules and are refused if they name
// no field — the skip is narrow enough that it cannot swallow a value, and it
// is counted so that `--verbose` can say what was passed over.
func readDir(dir string) ([]os.DirEntry, []string, error) {
	all, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	out := make([]os.DirEntry, 0, len(all))
	var skipped []string
	for _, e := range all {
		if strings.HasPrefix(e.Name(), ".") && !e.IsDir() && !strings.HasSuffix(e.Name(), ".json") {
			skipped = append(skipped, filepath.Join(dir, e.Name()))
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	sort.Strings(skipped)
	return out, skipped, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func fieldShapeName(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TString:
		return "string"
	case f.Type.Kind == ir.TBytes:
		return "bytes"
	case tabletext.UnionOf(f) != nil:
		return "union"
	case tabletext.EnumOf(f) != nil:
		return "enum"
	case tabletext.FlagsOf(f) != nil:
		return "flags"
	}
	return "scalar"
}
