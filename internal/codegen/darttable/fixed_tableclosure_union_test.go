package darttable

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestFixedFormDeclaresATableClosureUnion is the name-buildability row
// u_enum_arm_table (testdata/name-buildability, SPEC §4.6): a union whose arm
// is an enum or a flags mask is a table-closure construct
// (ir.Union.TableClosureOnly), assembled beside the decl stream, so the
// packet emitter (internal/codegen/dart) never declares its class. Reached
// from a table, <Base>Fixed.dart must declare it itself; it once took the
// union for the packet class, imported only the arm's enum, and left U, zeroU
// and UType undefined under `dart analyze`.
func TestFixedFormDeclaresATableClosureUnion(t *testing.T) {
	t.Parallel()
	for name, decl := range map[string]string{
		"an enum arm":   "enum Mode\n{ Alpha, Beta }\n\nunion U\n{ mode Mode }\n",
		"a flags arm":   "flags Bits\n{ A, B }\n\nunion U\n{ fl Bits }\n",
		"two enum arms": "enum Mode\n{ Alpha, Beta }\n\nenum Side\n{ Left, Right }\n\nunion U\n{\n    mode Mode\n    side Side\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, "package probe\n\n"+decl+"\nfixed table Rec\n{\n    pick U\n}\n")
			if !u.TableUnions["U"].TableClosureOnly() {
				t.Fatal("U is not a table-closure union: this case no longer holds the defect it is for")
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			src, ok := files["ProbeFixed.dart"]
			if !ok {
				t.Fatalf("no ProbeFixed.dart among %d files", len(files))
			}
			fixed := string(src)
			if !strings.Contains(fixed, "final U pick = U();") {
				t.Fatalf("Rec's storage no longer holds a U; the case checks nothing:\n%s", fixed)
			}
			declared := regexp.MustCompile(`(?m)^final class U \{`).MatchString(fixed)
			shown := slices.Contains(shownNames(fixed), "U")
			if !declared && !shown {
				t.Error("ProbeFixed.dart neither declares class U nor imports it: `dart analyze` reports Undefined class 'U' (the packet emitter never declares a table-closure union)")
			}
			if shown {
				t.Error("ProbeFixed.dart imports U, but no packet library declares a table-closure union")
			}
			if strings.Contains(fixed, "UType") {
				t.Error("ProbeFixed.dart spells UType, the packet class's tag type, which no library declares for a table-closure union")
			}
		})
	}
}
