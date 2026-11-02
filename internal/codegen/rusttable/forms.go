package rusttable

import "github.com/mas-bandwidth/schema/v2/ir"

// Disabled roots remain usable as storage inside an enabled parent. Only
// public root entry points are filtered; closure body codecs remain available.
func anyCookForm(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if st.CarriesForm("cook") {
			return true
		}
	}
	return false
}
