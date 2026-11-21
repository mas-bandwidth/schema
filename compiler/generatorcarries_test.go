// A REGISTERED GENERATOR DECLARES WHAT IT CARRIES (review finding 12).
//
// Compiler.Generate refuses a unit that uses a centrally gated construct (the
// keys of CarryConstructs) unless the selected target carries it. A generator
// written outside this module reaches the driver through Register alone, so
// it declares its constructs there, through the public ConstructCarrier
// interface, and the driver honours the declaration construct by construct.
// These tests use the public API only, as an external generator does.
package compiler_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// catalog inspects a unit's metadata and emits a list of its timestamp
// fields; it declares the constructs it was built with.
type catalog struct {
	carried []string
	calls   *int
}

func (catalog) Names() []string     { return []string{"catalog"} }
func (g catalog) Carries() []string { return g.carried }
func (g catalog) Generate(u *ir.Unit, _ compiler.Options) (map[string][]byte, error) {
	*g.calls++
	return map[string][]byte{"catalog.txt": []byte(strings.Join(ir.TableTimestampFields(u), "\n"))}, nil
}

// plain declares nothing: the driver's refusals stand for it as before.
type plain struct{ calls *int }

func (plain) Names() []string { return []string{"plain"} }
func (g plain) Generate(*ir.Unit, compiler.Options) (map[string][]byte, error) {
	*g.calls++
	return map[string][]byte{}, nil
}

func loadSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return u
}

const carriesTimestampSrc = "package probe\n\ntable Event\n{\n    id        uint32\n    opened_at timestamp\n}\n"

// one unit per construct, each using that construct and no other gated one
var carriesUnits = []struct {
	construct string
	src       string
}{
	{"timestamp", carriesTimestampSrc},
	{"required", "package probe\n\ntable Ship\n{\n    name string(32) | required\n}\n"},
	{"text-list", "package probe\n\ntable Zone\n{\n    rules [..4]string(8)\n}\n"},
	{"optional-map", "package probe\n\ntable Zmap\n{\n    tags ?map[string(8)]int32\n}\n"},
}

func TestExternalGeneratorWithoutDeclarationIsRefused(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	c := compiler.New()
	calls := 0
	if err := c.Register(plain{&calls}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Generate(u, "plain", nil); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("a generator declaring nothing was not refused the timestamp kind: %v", err)
	}
	if calls != 0 {
		t.Fatal("the refused generator was invoked")
	}
}

func TestExternalGeneratorDeclaresTimestamp(t *testing.T) {
	u := loadSource(t, carriesTimestampSrc)
	c := compiler.New()
	calls := 0
	if err := c.Register(catalog{[]string{"timestamp"}, &calls}); err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "catalog", nil)
	if err != nil {
		t.Fatalf("a generator declaring timestamp was refused: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the generator was invoked %d times, want 1", calls)
	}
	if got := string(files["catalog.txt"]); !strings.Contains(got, "opened_at") {
		t.Fatalf("the generator did not see the timestamp field: %q", got)
	}
}

// The declaration opens exactly the constructs it names: a construct it
// declares is handed to the generator, every other gated one is refused.
func TestExternalGeneratorDeclarationIsPerConstruct(t *testing.T) {
	for _, tc := range carriesUnits {
		t.Run(tc.construct, func(t *testing.T) {
			if !slices.Contains(compiler.CarryConstructs(), tc.construct) {
				t.Fatalf("%q is not a construct key", tc.construct)
			}
			u := loadSource(t, tc.src)
			if got := compiler.UnitConstructs(u); !slices.Equal(got, []string{tc.construct}) {
				t.Fatalf("the unit uses %v, want only %s", got, tc.construct)
			}

			c := compiler.New()
			calls := 0
			if err := c.Register(catalog{[]string{tc.construct}, &calls}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Generate(u, "catalog", nil); err != nil {
				t.Fatalf("declared %s and was refused: %v", tc.construct, err)
			}

			other := "integrity"
			c = compiler.New()
			if err := c.Register(catalog{[]string{other}, &calls}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Generate(u, "catalog", nil); err == nil {
				t.Fatalf("declared only %s and was handed a unit using %s", other, tc.construct)
			}
			if calls != 1 {
				t.Fatalf("the generator was invoked %d times, want 1", calls)
			}
		})
	}
}

// A misspelled construct is a registration error, never a silent no-op, and
// nothing is registered when it fails.
func TestRegisterRefusesUnknownCarriedConstruct(t *testing.T) {
	c := compiler.New()
	calls := 0
	err := c.Register(catalog{[]string{"timestamp", "timestmp"}, &calls})
	if err == nil || !strings.Contains(err.Error(), "timestmp") {
		t.Fatalf("an unknown construct key registered: %v", err)
	}
	if slices.Contains(c.Targets(), "catalog") {
		t.Fatal("the refused generator was registered anyway")
	}
}
