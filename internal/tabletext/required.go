package tabletext

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math/big"
	"strings"
)

// MissingRequired is a named whole-record refusal, not framing damage.
type MissingRequired struct{ Reason string }

func (e *MissingRequired) Error() string { return e.Reason }

// RequireSeen closes one body. Presence is supplied by the form's own rule:
// a named JSON key or a compatible binary field. Guards use the final value.
func (r *Report) RequireSeen(inst *Instance, seen func(int) bool) bool {
	guards := Guards(inst.Def)
	var missing []string
	for i := range inst.Fields {
		f := inst.Fields[i].Def
		if !f.Required || seen(i) {
			continue
		}
		if terms, guarded := guards[f.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		missing = append(missing, f.Name)
	}
	if len(missing) == 0 {
		return true
	}
	r.MissingRequired++
	r.Reason = "missing_required: " + strings.Join(missing, ", ")
	return false
}

// ValidateRequiredValue refuses an unset or out-of-range required value.
// Compound fields validate their element or child declarations in their walks.
func ValidateRequiredValue(fv *Field) error {
	f := fv.Def
	if !f.Required || f.Type.Pointer || f.IsMap() {
		return nil
	}
	cells := []Cell{fv.Cell}
	if f.Array != ir.ArrayNone {
		cells = fv.Elems
		if f.CountedOnWire() {
			if fv.Count < 0 || fv.Count > len(cells) {
				return fmt.Errorf("invalid_value: %s count is outside storage", f.Name)
			}
			cells = cells[:fv.Count]
		}
	}
	for _, c := range cells {
		if lo, hi, ok := ir.TableRawRange(f); ok {
			var raw big.Int
			if ir.TableKindWide(ir.TableScalarKind(f)) {
				if c.Wide != nil {
					raw.Set(c.Wide)
				}
			} else if f.Type.Signed {
				raw.SetInt64(int64(c.U))
			} else {
				raw.SetUint64(c.U)
			}
			if raw.Cmp(lo) < 0 || raw.Cmp(hi) > 0 {
				return fmt.Errorf("invalid_value: %s is outside its required range", f.Name)
			}
		}
		if f.HasFloatRange && (c.F < f.FMin || c.F > f.FMax) {
			return fmt.Errorf("invalid_value: %s is outside its required range", f.Name)
		}
	}
	return nil
}
