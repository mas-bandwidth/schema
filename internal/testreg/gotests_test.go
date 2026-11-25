package testreg

import "testing"

// unionRow is a per-language test line whose inputs name each language's
// own Go test for the case.
func unionRow(key, tests string) string {
	return `{"rec":"test","key":"` + key + `","type":"roundtrip","family":"fam","case":"` + key[len("roundtrip.fam."):] + `","tier":"unit","checks":"x","inputs":{"tests":` + tests + `},"expect":"x"}` + "\n"
}

// TestLoadAcceptsAUnionRow: a union row names some languages' Go tests; it
// has an instance in every language all the same (one it does not name is
// red when run, never left out).
func TestLoadAcceptsAUnionRow(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture+unionRow("roundtrip.fam.u", `{"go":{"package":"./g","test":"TestGoU"},"c":{"package":"./c","test":"TestCU","tags":"schema_leg_c"}}`))
	n := 0
	for _, inst := range reg.Instances() {
		if inst.Test.Key == "roundtrip.fam.u" {
			n++
		}
	}
	if n != len(reg.Langs) {
		t.Fatalf("the union row has %d instances, want one per language (%d)", n, len(reg.Langs))
	}
	tests, ok, err := GoTestsOf(reg.Tests[len(reg.Tests)-1])
	if !ok || err != nil || len(tests) != 2 || tests["c"].Tags != "schema_leg_c" {
		t.Fatalf("GoTestsOf = %v %v %v", tests, ok, err)
	}
}

// TestLoadRefusesAUnionRowForALanguageThatDoesNotExist: a union row names
// Go tests only for languages the registry lists, never for none.
func TestLoadRefusesAUnionRowForALanguageThatDoesNotExist(t *testing.T) {
	t.Parallel()
	refused(t, fixture+unionRow("roundtrip.fam.u", `{"cobol":{"package":"./c","test":"TestU"}}`), "fixture.jsonl:15:", `names a Go test for "cobol"`)
	refused(t, fixture+unionRow("roundtrip.fam.u", `{"none":{"package":"./c","test":"TestU"}}`), "fixture.jsonl:15:", `names a Go test for "none"`)
}

// TestLoadRefusesAGoTestTwoUnionRowsName: one Go test answers one case.
func TestLoadRefusesAGoTestTwoUnionRowsName(t *testing.T) {
	t.Parallel()
	refused(t, fixture+unionRow("roundtrip.fam.u", `{"go":{"package":"./g","test":"TestU"}}`)+unionRow("roundtrip.fam.v", `{"lua":{"package":"./g","test":"TestU"}}`),
		"fixture.jsonl:16:", "./g TestU, which roundtrip.fam.u already names")
}

// TestLoadRefusesUnionInputsThatDoNotRead: no language, an unknown field, a
// package that is not ./ relative, a name that is not a Go test, and a
// language-free union row are each refused.
func TestLoadRefusesUnionInputsThatDoNotRead(t *testing.T) {
	t.Parallel()
	for tests, words := range map[string]string{
		`{}`: "name no language",
		`{"go":{"package":"./g","test":"TestU","sub":"x"}}`: "unknown field",
		`{"go":{"package":"g","test":"TestU"}}`:             "want a ./package",
		`{"go":{"package":"./g","test":"Helper"}}`:          "want a ./package",
	} {
		refused(t, fixture+unionRow("roundtrip.fam.u", tests), "fixture.jsonl:15:", words)
	}
	free := `{"rec":"test","key":"tooling.self.u","type":"tooling","family":"self","case":"u","tier":"unit","checks":"x","inputs":{"tests":{"go":{"package":"./g","test":"TestU"}}},"expect":"x","scope":"none","adapter":"fake"}` + "\n"
	refused(t, fixture+free, "fixture.jsonl:15:", `scoped "none"`)
}
