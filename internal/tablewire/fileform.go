// THE CONFORMANCE CORPUS'S FILE FORM.
//
// The wire goldens every language leg pins under testdata/wire/tables are form
// 1 for every instance, including instances of a table declared `fixed`. A
// runtime at a declared fixed root reads form 3 and refuses form 1 (Decode);
// the corpus reader keeps the declaration's value-only walk and reads the form
// 1 bytes it is given, and the fixed form has its own corpus and surfaces. A
// form-3 file is still read by the fixed reader, so an offline tool may hand
// either form here.
package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// DecodeFileForm reads one corpus file: form 1 through the variable form's
// reader with the declaration's own walk, and form 3 through the fixed
// reader. It is the conformance corpus and cook reader, not a runtime read.
func DecodeFileForm(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	if len(data) > 0 && data[0] == tableFixedForm {
		return Decode(m, inst, data, report)
	}
	return decodeWith(m, inst, data, report, nil, false, true)
}

// DecodeRegionFileForm is [DecodeRegion] over the corpus file form.
func DecodeRegionFileForm(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	if len(data) > 0 && data[0] == tableFixedForm {
		return DecodeRegion(m, inst, data, report)
	}
	return decodeWith(m, inst, data, report, nil, true, true)
}

// EncodeFileForm writes one corpus file: form 1, which is the form every
// pinned corpus wire carries, whatever the table's declaration. A fixed
// root's form 3 is [EncodeFixed]'s.
func EncodeFileForm(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	out, _, err := encodeWith(m, inst, nil)
	return out, err
}
