package gotable

import (
	"bytes"
	"maps"
	"regexp"
	"strings"
	"testing"
)

// TestGoTestgenRoundTripAndDamage is the gate of `schema testgen --lang go`
// (docs/SPEC-TABLES.md §16.11). It generates tables/examples, writes the
// generated tests beside it in a t.TempDir() and runs them: every emitted test
// RUNs and PASSes. Then it sabotages one byte of one emitted decode (a field id
// in Patrol's body reader, so the field reads as unknown) and the emitted round
// trip has to catch it.
func TestGoTestgenRoundTripAndDamage(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/examples"})
	tests, err := Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	// ONE FILE PER UNIT FILE THAT DECLARES A ROOT TABLE (§16.11).
	var want []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsMapEntry() {
				want = append(want, f.Base+"Table_gen_test.go")
				break
			}
		}
	}
	if len(tests) != len(want) {
		t.Fatalf("testgen wrote %d files, want %d (%v)", len(tests), len(want), want)
	}
	var funcs []string
	for _, name := range want {
		src, ok := tests[name]
		if !ok {
			t.Fatalf("testgen wrote no %s", name)
		}
		if !bytes.Contains(src, []byte("package "+u.Package+"\n")) {
			t.Fatalf("%s is not in package %s", name, u.Package)
		}
		for _, banned := range []string{"\"time\"", "\"net\"", "\"os\"", "math/rand"} {
			if bytes.Contains(src, []byte(banned)) {
				t.Fatalf("%s imports %s: a generated test uses the generated API and the standard library's pure parts only", name, banned)
			}
		}
		for _, m := range regexp.MustCompile(`(?m)^func (Test\w+)\(t \*testing\.T\) \{\n\tt\.Parallel\(\)`).FindAllSubmatch(src, -1) {
			funcs = append(funcs, string(m[1]))
		}
	}
	if !strings.Contains(strings.Join(funcs, " "), "TestPatrolGenRoundTrip") || !strings.Contains(strings.Join(funcs, " "), "TestKeyedConfigGenDamage") {
		t.Fatalf("expected tests missing: %v", funcs)
	}
	again, err := Testgen(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range tests {
		if !bytes.Equal(again[name], src) {
			t.Fatalf("%s is not deterministic", name)
		}
	}
	stub := "package " + u.Package + "\n"
	add := func(files map[string][]byte) { maps.Copy(files, tests) }

	t.Run("green", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, add, "-v")
		if err != nil {
			t.Fatalf("emitted tests: %v\n%s", err, out)
		}
		for _, fn := range funcs {
			if !bytes.Contains(out, []byte("=== RUN   "+fn+"\n")) || !bytes.Contains(out, []byte("--- PASS: "+fn+" ")) {
				t.Fatalf("emitted test %s did not RUN and PASS:\n%s", fn, out)
			}
		}
		t.Logf("%d emitted tests RUN and PASS", len(funcs))
	})

	t.Run("sabotaged_reader", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, u, stub, func(files map[string][]byte) {
			add(files)
			src := files["GuardedTable.go"]
			at := bytes.Index(src, []byte("func PatrolLoadBody(r *TableReader"))
			if at < 0 {
				t.Fatal("no PatrolLoadBody in GuardedTable.go")
			}
			// the first field-id case of the body reader: swap one byte of it
			c := bytes.Index(src[at:], []byte("\t\tcase 0x"))
			if c < 0 {
				t.Fatal("no field-id case in PatrolLoadBody")
			}
			b := at + c + len("\t\tcase 0x")
			sabotaged := bytes.Clone(src)
			if sabotaged[b] == '0' {
				sabotaged[b] = '1'
			} else {
				sabotaged[b] = '0'
			}
			files["GuardedTable.go"] = sabotaged
		}, "-run", "^TestPatrolGen")
		if err == nil || !bytes.Contains(out, []byte("--- FAIL: TestPatrolGenRoundTrip")) {
			t.Fatalf("a sabotaged Patrol reader went unnoticed by the emitted tests: %v\n%s", err, out)
		}
	})
}

// TestGoTestgenRefusesWhatItCannotSpell: a table that does not carry the JSON
// form has no sample spelling, and testgen refuses it by name (§16.11).
func TestGoTestgenRefusesWhatItCannotSpell(t *testing.T) {
	t.Parallel()
	_, err := Testgen(unitFrom(t, "package probe\ntable Only | forms = \"wire\" { n int32 }\n"))
	if err == nil || !strings.Contains(err.Error(), "Only") || !strings.Contains(err.Error(), "json") {
		t.Fatalf("forms without json: %v", err)
	}
}
