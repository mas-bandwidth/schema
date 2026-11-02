package tabletext

import (
	"fmt"
)

func textElementBytes(cell Cell) int64 {
	if cell.Tab == nil {
		return 0
	}
	value := &cell.Tab.Fields[0]
	if value.Def.Type.Optional && !value.Present {
		return 0
	}
	return int64(len(value.Cell.Str))
}

// KeepTextTotal adds one decoded element and keeps only the prefix within
// the total. After false, callers skip the remaining tail without decoding it.
func (m *Model) KeepTextTotal(fv *Field, index int, spent *int64, report *Report) bool {
	f := fv.Def
	if !f.IsTextList() || f.TextTotal <= 0 {
		return true
	}
	n := textElementBytes(fv.Elems[index])
	if n <= f.TextTotal-*spent {
		*spent += n
		return true
	}
	report.Clamped++
	if report.Reason == "" {
		report.Reason = "clamped: " + f.TextListElem().MapEntryOf
	}
	fv.Elems[index] = m.elementZero(f)
	return false
}

// ValidateTextTotal checks the stored list before either writer emits bytes.
func ValidateTextTotal(fv *Field) error {
	f := fv.Def
	if !f.IsTextList() || f.TextTotal <= 0 {
		return nil
	}
	if fv.Count < 0 || fv.Count > len(fv.Elems) {
		return fmt.Errorf("invalid_value: %s count is outside storage", f.Name)
	}
	remaining := f.TextTotal
	for i := 0; i < fv.Count; i++ {
		n := textElementBytes(fv.Elems[i])
		if n > remaining {
			return fmt.Errorf("invalid_value: %s exceeds its text total", f.TextListElem().MapEntryOf)
		}
		remaining -= n
	}
	return nil
}
