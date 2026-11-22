package ccommon

import (
	"fmt"
	"strings"
)

// HeaderGuard is the include guard of one generated C header: the header of
// schema file `base` in package `pkg`, and part names which of the file's
// headers it guards — "" for <Base>.h, then "wire", "table" and "block".
//
//	SCHEMA_<PKG>_<base>_h          SCHEMA_EXAMPLE_ArmDefaults_h
//	SCHEMA_<PKG>_<base>_<part>_h   SCHEMA_EXAMPLE_ArmDefaults_wire_h
//
// A file base has no grammar (`my-file.schema` and `other.schema` beside
// `Other.schema` both check), and a guard must be one valid token, distinct
// for every file. So the base keeps its case and every byte outside
// [A-Za-z0-9] — the underscore included — is spelled `_` and two uppercase hex
// digits: `my-file` is `my_2Dfile`, `my_file` is `my_5Ffile`, and `MyFile`,
// `my_file`, `other` and `Other` all differ. The part and the `_h` are
// lowercase, which neither an escape (uppercase hex) nor a package-wide guard
// (SCHEMA_<PKG>_VIEW_H, SCHEMA_<PKG>_TABLE_ARENA, all uppercase) ever spells,
// so no file's guard is another file's, or a package's, for any base.
func HeaderGuard(pkg, base, part string) string {
	var b strings.Builder
	b.WriteString("SCHEMA_")
	b.WriteString(strings.ToUpper(pkg))
	b.WriteString("_")
	for i := 0; i < len(base); i++ {
		c := base[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "_%02X", c)
	}
	if part != "" {
		b.WriteString("_")
		b.WriteString(part)
	}
	b.WriteString("_h")
	return b.String()
}
