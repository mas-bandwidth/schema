// `schema unpack` — the inverse (SPEC-TABLES.md §17.2): a root table's wire
// bytes become the tree again, through §16's text form, which is the tool round
// trip §1 promises. `unpack` -> `pack` is byte-stable.
package tablepack

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Unpack decodes a root table's wire bytes and writes the tree under dir. The
// report is the wire's own (§4): silence means the bytes matched this schema
// exactly.
//
// §17.1 permits a field's value to live in a `<field>.json` or in a directory,
// and this writer takes the EXPANDED form at the root: one `<field>.json` per
// field, and one `<field>/<Variant>.json` per slot of an enum-keyed array —
// the shape a person edits, and the one that exercises the directory rule
// rather than the one-file shortcut. An absent `?T` and a guarded-out field
// write no file at all, because absence is what the tree says by omission.
func Unpack(m *tabletext.Model, root string, wire []byte, dir string) (tabletext.Report, error) {
	st := m.Unit.Tables[root]
	if st == nil {
		return tabletext.Report{}, fmt.Errorf("--root %s names no table in this unit", root)
	}
	inst := m.New(st)
	var report tabletext.Report
	tablewire.Decode(m, inst, wire, &report)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return report, err
	}
	guards := tabletext.Guards(st)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		f := fv.Def
		if terms, guarded := guards[f.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if f.Type.Optional && !fv.Present {
			continue
		}
		key := ir.TableFieldJsonKey(f)
		if f.KeyEnum != "" {
			if err := unpackKeyed(m, fv, filepath.Join(dir, key)); err != nil {
				return report, err
			}
			continue
		}
		text, err := m.WriteValue(fv)
		if err != nil {
			return report, err
		}
		if err := os.WriteFile(filepath.Join(dir, key+".json"), append(text, '\n'), 0o644); err != nil {
			return report, err
		}
	}
	return report, nil
}

func unpackKeyed(m *tabletext.Model, fv *tabletext.Field, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := fv.Def
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		name := tabletext.EnumName(f.KeyEnumRef, tabletext.KeyedSlotValue(f, slot))
		if name == "" {
			return fmt.Errorf("enum-keyed array %s: slot %d belongs to no variant of %s", f.Name, slot, f.KeyEnum)
		}
		text, err := m.WriteElement(fv, slot)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), append(text, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}
