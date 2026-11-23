package ccommon

import "testing"

// TestHeaderGuardSpelling pins the guard of each part and the escape of every
// byte a file base may carry outside [A-Za-z0-9].
func TestHeaderGuardSpelling(t *testing.T) {
	for _, tc := range []struct{ pkg, base, part, want string }{
		{"probe", "Other", "", "SCHEMA_PROBE_Other_h"},
		{"probe", "other", "", "SCHEMA_PROBE_other_h"},
		{"probe", "my-file", "", "SCHEMA_PROBE_my_2Dfile_h"},
		{"probe", "my_file", "wire", "SCHEMA_PROBE_my_5Ffile_wire_h"},
		{"probe", "MyFile", "table", "SCHEMA_PROBE_MyFile_table_h"},
		{"probe", "a.b", "block", "SCHEMA_PROBE_a_2Eb_block_h"},
	} {
		if got := HeaderGuard(tc.pkg, tc.base, tc.part); got != tc.want {
			t.Errorf("HeaderGuard(%q, %q, %q) = %q, want %q", tc.pkg, tc.base, tc.part, got, tc.want)
		}
	}
}

// TestHeaderGuardDistinct holds every guard a unit could spell distinct: the
// bases that once shared a guard (other/Other upper-cased alike, `my_file`
// beside `my_2Dfile`, a base ending in a part name beside the part), and every
// part of each, are all different tokens, and each is a valid C identifier.
func TestHeaderGuardDistinct(t *testing.T) {
	bases := []string{
		"other", "Other", "OTHER", "my-file", "my_file", "my_2Dfile", "MyFile",
		"my_5Ffile", "Awire", "A", "A_wire", "Ablock", "Atable", "a.b", "a_2Eb",
		"_x", "x_", "9",
	}
	seen := map[string]string{}
	for _, base := range bases {
		for _, part := range []string{"", "wire", "table", "block"} {
			g := HeaderGuard("probe", base, part)
			if prev, ok := seen[g]; ok {
				t.Errorf("guard %q is spelled by both %s and %s/%s", g, prev, base, part)
			}
			seen[g] = base + "/" + part
			for i := 0; i < len(g); i++ {
				c := g[i]
				ident := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
				if !ident {
					t.Errorf("guard %q holds %q, not an identifier byte", g, c)
				}
			}
		}
	}
	// the package-wide guards are all upper case, so no file guard is one
	for _, pkgWide := range []string{"SCHEMA_PROBE_VIEW_H", "SCHEMA_PROBE_TABLE_ARENA"} {
		if who, ok := seen[pkgWide]; ok {
			t.Errorf("file guard of %s spells the package-wide %s", who, pkgWide)
		}
	}
}
