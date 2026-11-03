package cstable

import "github.com/mas-bandwidth/schema/v2/ir"

func anyTableForm(u *ir.Unit, form string) bool {
	for _, st := range u.Tables {
		if !st.IsMapEntry() && st.CarriesForm(form) {
			return true
		}
	}
	return false
}

func fileTableForm(f *ir.File, form string) bool {
	for _, st := range f.Tables {
		if st.CarriesForm(form) {
			return true
		}
	}
	return false
}

func restrictedCook(u *ir.Unit) bool {
	for _, st := range u.Tables {
		if !st.CarriesForm("cook") {
			return true
		}
	}
	return false
}
