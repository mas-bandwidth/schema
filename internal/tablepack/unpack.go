// `schema unpack` — the inverse (SPEC-TABLES.md §17.3): a root table's wire
// bytes become the tree again, through §16's text form, which is the tool round
// trip §1 promises. `unpack` -> `pack` is byte-stable.
package tablepack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Unpack decodes a root table's wire bytes and writes the tree under dir. The
// report is the wire's own (§4): silence means the bytes matched this schema
// exactly.
//
// §17.2 permits a field's value to live in a `<field>.json` or in a directory,
// and this writer takes the EXPANDED form: one `<field>.json` per field, and
// one `<field>/<Variant>.json` per slot of an enum-keyed array — the shape a
// person edits, and the one that exercises the directory rule rather than the
// one-file shortcut. An absent `?T` and a guarded-out field write no file at
// all, because omission is how a tree says absence.
//
// It also PRUNES, and it prunes for THE ROOT'S WHOLE SHAPE rather than for the
// shape it happens to be writing: every `<field>.json`, every `<field>/`
// directory AND the `<Root>.json` that names the root itself is removed unless
// this run wrote it. Both halves matter. Without the first, unpacking a newer
// `.bin` over yesterday's tree leaves the file an absent optional used to have
// standing beside the new one. Without the second, switching between the
// expanded shape and `--one-file` in one directory leaves BOTH — and `pack`
// refuses that tree by name ("a root is one file or one tree of fields, never
// both"), so the tool would be writing a tree its own sibling verb rejects.
//
// An entry that names NO part of the root is left exactly where it is: it is
// not this tool's, and `pack` names it if it does not belong.
func Unpack(m *tabletext.Model, root string, wire []byte, dir string) (tabletext.Report, error) {
	return unpack(m, root, wire, dir, false)
}

// UnpackOneFile writes the root as ONE `<Root>.json` instead of a tree of
// fields — §17.2's last rule as an output shape. It is the same instance and
// the same writer, so it packs to the same bytes the expanded tree does; it
// exists because one text of the whole root is what a backend's `ToJson`
// produces, and comparing the two is §17.1's third golden.
func UnpackOneFile(m *tabletext.Model, root string, wire []byte, dir string) (tabletext.Report, error) {
	return unpack(m, root, wire, dir, true)
}

func unpack(m *tabletext.Model, root string, wire []byte, dir string, oneFile bool) (tabletext.Report, error) {
	st := m.Unit.Tables[root]
	if st == nil {
		return tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit; the roots it declares are %s", root, strings.Join(m.Roots(), ", "))
	}
	inst := m.New(st)
	var report tabletext.Report
	// the refusal comes back BEFORE anything is written: a root this engine
	// does not decode is not a tree half-written to disk
	ok, err := tablewire.Decode(m, inst, wire, &report)
	if err != nil {
		return report, err
	}
	if !ok {
		return report, fmt.Errorf("the bytes are not a %s body — the framing is damaged past the point the walk could continue (SPEC-TABLES.md §4)", root)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return report, err
	}
	// what the ROOT's shape owns at this level, whichever shape is written:
	// every field key, and the root's own name
	owned := map[string]bool{root: true}
	for i := range st.Fields {
		owned[ir.TableFieldJsonKey(st.Fields[i])] = true
	}
	if oneFile {
		text, err := m.Write(inst)
		if err != nil {
			return report, err
		}
		name := root + ".json"
		if err := os.WriteFile(filepath.Join(dir, name), append(text, '\n'), 0o644); err != nil {
			return report, err
		}
		return report, prune(dir, owned, map[string]bool{name: true})
	}
	guards := tabletext.Guards(st)
	written := map[string]bool{}
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		key := ir.TableFieldJsonKey(f)
		if terms, guarded := guards[f.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if f.Type.Optional && !fv.Present {
			continue
		}
		if f.KeyEnum != "" {
			if err := unpackKeyed(m, fv, filepath.Join(dir, key)); err != nil {
				return report, err
			}
			written[key] = true
			continue
		}
		text, err := m.WriteValue(fv)
		if err != nil {
			return report, err
		}
		if err := os.WriteFile(filepath.Join(dir, key+".json"), append(text, '\n'), 0o644); err != nil {
			return report, err
		}
		written[key+".json"] = true
	}
	return report, prune(dir, owned, written)
}

func unpackKeyed(m *tabletext.Model, fv *tabletext.Field, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := fv.Def
	written := map[string]bool{}
	owned := map[string]bool{}
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		name := tabletext.EnumName(f.KeyEnumRef, tabletext.KeyedSlotValue(f, slot))
		if name == "" {
			return fmt.Errorf("enum-keyed array %s: slot %d belongs to no variant of %s", f.Name, slot, f.KeyEnum)
		}
		owned[name] = true
		text, err := m.WriteElement(fv, slot)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(text, '\n'), 0o644); err != nil {
			return err
		}
		written[name+".json"] = true
	}
	return prune(dir, owned, written)
}

// prune removes the entries at one level that NAME something this level owns —
// a field key, or a variant of the keying enum — and that this unpack did not
// write. Both spellings of a value are pruned, so a stale `<field>/` directory
// left beside a fresh `<field>.json` cannot become "two entries claim one
// value" on the next pack. Anything the level does not own is left alone.
func prune(dir string, owned, written map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if written[e.Name()] {
			continue
		}
		key, ok := entryKey(e)
		if !ok || !owned[key] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
