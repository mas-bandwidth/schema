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

// TextTotalFits adds one decoded element to a text list's running byte sum
// and reports whether the sum is still within the list's `| total`
// (docs/SPEC-TABLES.md §2.13). A list past its total refuses the read
// `length_over_bound`; nothing is dropped.
func TextTotalFits(fv *Field, index int, spent *int64) bool {
	f := fv.Def
	if !f.IsTextList() || f.TextTotal <= 0 {
		return true
	}
	*spent += textElementBytes(fv.Elems[index])
	return *spent <= f.TextTotal
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
