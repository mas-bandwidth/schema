package compiler

import (
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LOBSTER RUNTIME NAMES, SCANNED BOTH WAYS WITH A CONTROL (docs/PORTING.md
// I6; docs/SPEC-TABLES.md §11; internal/tablenames/lobster.go).
//
// WHAT A LOBSTER RUNTIME CAN COLLIDE WITH. Every runtime module the table
// emitter writes declares `namespace schema_rt`, and everything it defines at
// column zero (a def, a class, a let, a var) is a member of that namespace,
// which the generated table modules reach as `schema_rt.<name>`. A schema
// declaration is emitted inside the unit's own namespace, its package, so no
// declaration can land on a runtime name, and the registry's Lobster names all
// claim nothing. What holds the list honest is this scan: every name the
// emitted runtime defines is registered, and every registered name is
// defined, in both directions. The two claimable Lobster spellings are refused
// by name instead: a package named schema_rt (TestLobsterRefusesByName) and a
// schema file named for a runtime module, refused beside a table and kept in a
// table-free unit (TestLobsterRuntimeFileNamesAreClaimed).
//
// The scan reads the emitted text, never a list: a name a later card adds and
// forgets to register fails here whatever it is called.

var (
	lobsterNamespaceDecl = regexp.MustCompile(`(?m)^(?:def|class|let|var)\s+([A-Za-z_]\w*)`)
	lobsterNamespaceLine = regexp.MustCompile(`(?m)^namespace\s+(\w+)\s*$`)
)

// lobsterRuntimeNames is every name a file defines at column zero inside
// `namespace schema_rt`; a file in any other namespace (a generated table
// module, whose names are its declarations') answers none.
func lobsterRuntimeNames(text string) []string {
	ns := lobsterNamespaceLine.FindStringSubmatch(text)
	if ns == nil || ns[1] != "schema_rt" {
		return nil
	}
	seen := map[string]bool{}
	for _, m := range lobsterNamespaceDecl.FindAllStringSubmatch(text, -1) {
		seen[m[1]] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

// lobsterEmittedNames is the union, over the units the Lobster table backend
// generates (every unit of the conformance manifest, lobsterManifestUnits: the
// corpus reaches every form, so the union is the whole runtime), of every name its emitted runtime modules define.
func lobsterEmittedNames(t *testing.T, units map[string]*ir.Unit) map[string]bool {
	t.Helper()
	emitted := map[string]bool{}
	for _, u := range units {
		files, err := lobstertable.Generate(u)
		if err != nil {
			continue // a unit the backend refuses by name spells nothing
		}
		for _, data := range files {
			for _, n := range lobsterRuntimeNames(string(data)) {
				emitted[n] = true
			}
		}
	}
	return emitted
}

// lobsterNameFindings holds one emitted name set against the registry's list,
// both ways.
func lobsterNameFindings(emitted map[string]bool) (unregistered, unemitted []string) {
	known := map[string]bool{}
	for _, n := range tablenames.LobsterRuntimeNames() {
		known[n] = true
	}
	for _, n := range slices.Sorted(maps.Keys(emitted)) {
		if !known[n] {
			unregistered = append(unregistered, n)
		}
	}
	for _, n := range tablenames.LobsterRuntimeNames() {
		if !emitted[n] {
			unemitted = append(unemitted, n)
		}
	}
	return unregistered, unemitted
}

func TestLobsterTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	emitted := lobsterEmittedNames(t, lobsterManifestUnits(t))
	if len(emitted) == 0 {
		t.Fatal("the scan found no runtime name in the emitted Lobster at all: the scan, not the registry, is what broke")
	}
	unregistered, unemitted := lobsterNameFindings(emitted)
	for _, n := range unregistered {
		t.Errorf("the Lobster table runtime defines %s and internal/tablenames does not register it: "+
			"register it in internal/tablenames/lobster.go (a schema_rt member claims nothing, but the scan needs an answer for it)", n)
	}
	for _, n := range unemitted {
		t.Errorf("internal/tablenames says the Lobster runtime defines %s, but nothing in the emitted Lobster defines it: "+
			"drop the registration or fix the backend", n)
	}
}

// TestLobsterRuntimeNameScanGoesRed is the scan's own NEGATIVE CONTROL: it
// feeds the scan a runtime module that defines a name nobody registered, in
// each spelling the scan reads, and an emitted set from which a registered
// name is gone, and requires both findings.
func TestLobsterRuntimeNameScanGoesRed(t *testing.T) {
	t.Parallel()
	sample := strings.Join([]string{
		"import .load",
		"namespace schema_rt",
		"def table_rogue_function(x:int) -> int:",
		"    let nested_local = x", // indented: not namespace scope
		"    return nested_local",
		"class TableRogueClass:",
		"    field:int",
		"let table_rogue_let = 1",
		"var table_rogue_var = 2",
	}, "\n")
	got := lobsterRuntimeNames(sample)
	want := []string{"TableRogueClass", "table_rogue_function", "table_rogue_let", "table_rogue_var"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the namespace scan is not seeing what it must:\n got %v\nwant %v", got, want)
	}
	if other := lobsterRuntimeNames(strings.Replace(sample, "namespace schema_rt", "namespace probe", 1)); other != nil {
		t.Fatalf("a module in the package's namespace answered %v; its names are its declarations' and claim nothing of the runtime", other)
	}
	emitted := map[string]bool{}
	for _, n := range got {
		if slices.Contains(tablenames.LobsterRuntimeNames(), n) {
			t.Fatalf("the planted %s is listed, so the control proves nothing: pick a name the list does not hold", n)
		}
		emitted[n] = true
	}
	unregistered, _ := lobsterNameFindings(emitted)
	if strings.Join(unregistered, ",") != strings.Join(want, ",") {
		t.Fatalf("the planted names were not reported unregistered: %v", unregistered)
	}
	some := tablenames.LobsterRuntimeNames()[0]
	if _, unemitted := lobsterNameFindings(map[string]bool{}); !slices.Contains(unemitted, some) {
		t.Fatalf("an emitted set without %s was not reported as a registered name nothing emits", some)
	}
}

// TestLobsterRuntimeFileNamesAreClaimed is the claimable half: a Lobster
// import names a file beside the module, so a schema file named for any
// emitted runtime module is REFUSED beside a table, and the same file in a
// table-free unit is kept, because no table runtime is emitted there.
func TestLobsterRuntimeFileNamesAreClaimed(t *testing.T) {
	t.Parallel()
	const withTable = "package probe\n\ntable Gadget\n{\n    n int32\n}\n"
	const tableFree = "package probe\n\ntype Gadget\n{\n    n int32\n}\n"
	var homes []string
	for name := range lobstertable.Runtime() {
		if base, ok := strings.CutSuffix(name, ".lobster"); ok {
			homes = append(homes, base)
		}
	}
	sort.Strings(homes)
	if len(homes) < 10 {
		t.Fatalf("the Lobster table backend emits only %v: the scan is blind", homes)
	}
	for _, base := range homes {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if _, err := (lobsterTarget{}).Generate(unitFromNamedSource(t, base, withTable), Options{}); err == nil {
				t.Fatalf("a schema file named %s beside a table was accepted: the unit would emit %s.lobster twice", base, base)
			}
			if _, err := (lobsterTarget{}).Generate(unitFromNamedSource(t, base, tableFree), Options{}); err != nil {
				t.Fatalf("a schema file named %s in a table-free unit was refused (%v): a claim costs a schema nothing until it declares a table", base, err)
			}
		})
	}
}
