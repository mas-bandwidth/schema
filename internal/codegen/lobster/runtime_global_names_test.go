package lobster

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A DECLARATION SPELLED LIKE A PACKET RUNTIME GLOBAL IS REFUSED (LOBSTER-2,
// review-owed/cross-leg/admitted-names-generate): the serialize_*.lobster
// files declare their names at global scope, and a generated module that
// declares an enum named align_bits converts to it with align_bits(v), which
// Lobster resolves to the runtime's function; a type named
// serialize_read_stream takes the runtime stream's place in every Read
// signature. Each such type, enum, flags or union is refused by name; a
// const of the same spelling folds to a literal and stays admitted.
func TestLobsterRuntimeGlobalNamesAreRefused(t *testing.T) {
	t.Parallel()
	globals := runtimeGlobals()
	if len(globals) < 100 || globals["align_bits"] == "" || globals["serialize_read_stream"] == "" {
		t.Fatalf("the global scan found %d names: the inventory is not the packet runtime's", len(globals))
	}
	if globals["fround"] != "" {
		t.Fatal("a name inside namespace schema_rt was taken for a global")
	}
	var names []string
	for n := range globals {
		names = append(names, n)
	}
	sort.Strings(names)
	shapes := []func(string) string{
		func(n string) string { return "enum " + n + " { Alpha, Beta }\n" },
		func(n string) string { return "flags " + n + " { Alpha, Beta }\n" },
		func(n string) string { return "type " + n + "\n{\n    x uint8\n}\n" },
		func(n string) string { return "type ProbeArm\n{\n    x uint8\n}\n\nunion " + n + " { a ProbeArm }\n" },
	}
	if len(names) == 0 {
		t.Fatal("names is empty: the loop below would check nothing")
	}
	generated := 0
	for i, n := range names {
		dir := t.TempDir()
		writeFile(t, dir, "Probe.schema", "package probe\n\n"+shapes[i%len(shapes)](n))
		// A name the checker already refuses for every leg (a serialize.h
		// macro the generated C++ would expand, SPEC §4.6) never reaches this
		// emitter: it is refused, which is all this test asks, so the
		// checker's refusal must name the declaration.
		u, errs := checkDir(t, dir)
		if len(errs) != 0 {
			if !strings.Contains(fmt.Sprint(errs), strconv.Quote(n)) {
				t.Errorf("%s: the checker refused something else: %v", n, errs)
			}
			continue
		}
		generated++
		_, err := Generate(u)
		if err == nil || !strings.Contains(err.Error(), "packet runtime declares globally") {
			t.Errorf("%s: %v, want refused as a packet runtime global", strings.TrimSpace(shapes[i%len(shapes)](n)), err)
		}
	}
	if generated < len(names)/2 {
		t.Fatalf("only %d of %d runtime globals reached the Lobster emitter: the checker refusal took the test over", generated, len(names))
	}
	dir := t.TempDir()
	writeFile(t, dir, "Probe.schema", "package probe\n\nconst align_bits = 3\n")
	if _, err := Generate(unitFromDir(t, dir)); err != nil {
		t.Errorf("a const spelled like a runtime global: %v, want admitted", err)
	}
}
