package cstable

import "github.com/mas-bandwidth/schema/v2/ir"

func integrityTrailerBytes(st *ir.Struct) int {
	if st.Integrity == "crc32c" {
		return 4
	}
	return 0
}
