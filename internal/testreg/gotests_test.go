package testreg

import (
	"strings"
	"testing"
)

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
		`{"go":{"package":"./g/","test":"TestU"}}`:          "want it written ./g",
		`{"go":{"package":"./a/../g","test":"TestU"}}`:      "want it written ./g",
		`{"go":{"package":"./../g","test":"TestU"}}`:        "want it written",
	} {
		refused(t, fixture+unionRow("roundtrip.fam.u", tests), "fixture.jsonl:15:", words)
	}
	free := `{"rec":"test","key":"tooling.self.u","type":"tooling","family":"self","case":"u","tier":"unit","checks":"x","inputs":{"tests":{"go":{"package":"./g","test":"TestU"}}},"expect":"x","scope":"none","adapter":"fake"}` + "\n"
	refused(t, fixture+free, "fixture.jsonl:15:", `scoped "none"`)
}

// intrinsicRow is a union row whose subject exists in one language.
func intrinsicRow(key, lang, reason, tests string) string {
	part := strings.SplitN(key, ".", 3)
	return `{"rec":"test","key":"` + key + `","type":"roundtrip","family":"` + part[1] + `","case":"` + part[2] + `","tier":"unit","checks":"x","inputs":{"tests":` + tests + `},"expect":"x","intrinsic":{"lang":"` + lang + `","reason":"` + reason + `"}}` + "\n"
}

// TestAnIntrinsicTestHasOneInstance: a test intrinsic to one language has
// that language's instance and no other; every other language is declared
// unrepresentable with the reason, so running it there by name fails, and
// the declarations are counted.
func TestAnIntrinsicTestHasOneInstance(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", "a Lua string escape", `{"lua":{"package":"./l","test":"TestLuaEscape"}}`))
	var got []string
	for _, inst := range reg.Instances() {
		if inst.Test.Key == "roundtrip.lua-fam.i" {
			got = append(got, inst.Full)
		}
	}
	if len(got) != 1 || got[0] != "lua.unit.roundtrip.lua-fam.i" {
		t.Fatalf("instances %v, want only lua's", got)
	}
	if _, err := reg.Resolve("go.unit.roundtrip.lua-fam.i"); err == nil || !strings.Contains(err.Error(), "intrinsic to lua: a Lua string escape") {
		t.Errorf("go's instance resolves: %v", err)
	}
	n := 0
	for _, u := range reg.Declared() {
		if u.Test == "roundtrip.lua-fam.i" {
			n++
			if u.Lang == "lua" {
				t.Errorf("the intrinsic language is declared unrepresentable")
			}
		}
	}
	if n != len(reg.Langs)-1 {
		t.Errorf("%d declared pairs, want %d", n, len(reg.Langs)-1)
	}
	without, err := load(t, fixture).Select(Query{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := reg.Select(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if with.Unrep-without.Unrep != len(reg.Langs)-1 {
		t.Errorf("a selection counts %d unrepresentable pairs for the intrinsic test, want %d", with.Unrep-without.Unrep, len(reg.Langs)-1)
	}
}

// TestLoadRefusesAnUnrepresentableLineOnAUnionRow: a union row's language
// with no Go test is red, so no line can declare it unrepresentable (and
// drop its instance while the ledger credits its test).
func TestLoadRefusesAnUnrepresentableLineOnAUnionRow(t *testing.T) {
	t.Parallel()
	refused(t, fixture+unionRow("roundtrip.fam.u", `{"go":{"package":"./g","test":"TestGoU"},"lua":{"package":"./l","test":"TestLuaU"}}`)+
		`{"rec":"unrepresentable","test":"roundtrip.fam.u","lang":"lua","reason":"r"}`+"\n", "fixture.jsonl:16:", "is a union row")
}

// TestLoadRefusesABadIntrinsic: an intrinsic test names a language of the
// registry and a reason that says something (not empty, not boilerplate,
// not under four words), is a per-language row, names no other language's
// Go test, and takes no unrepresentable line of its own.
func TestLoadRefusesABadIntrinsic(t *testing.T) {
	t.Parallel()
	lua := `{"lua":{"package":"./l","test":"TestLuaEscape"}}`
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "cobol", "a Lua string escape", lua), "fixture.jsonl:15:", `intrinsic to "cobol"`)
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", " ", lua), "fixture.jsonl:15:", "gives no reason")
	for _, r := range []string{"intrinsic", "Language-specific.", "no counterpart in other languages", "lua only",
		"this is really just lua", "it is language-specific code", "Exists only in this language, nowhere else.",
		"TODO fill this reason later", "placeholder reason to be written", "lua lua lua lua", "a unique Lua-only feature"} {
		refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", r, lua), "fixture.jsonl:15:", "boilerplate reason")
	}
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", "a Lua string escape", `{"lua":{"package":"./l","test":"TestLuaEscape"},"go":{"package":"./g","test":"TestGoEscape"}}`), "fixture.jsonl:15:", "names a Go test for go")
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", "a Lua string escape", lua)+`{"rec":"unrepresentable","test":"roundtrip.lua-fam.i","lang":"go","reason":"r"}`+"\n", "fixture.jsonl:16:", "already declares every other language")
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", "a Lua string escape", `{"go":{"package":"./g","test":"TestGoEscape"}}`), "fixture.jsonl:15:", "names no Go test for lua")
	plain := `{"rec":"test","key":"roundtrip.lua-fam.i","type":"roundtrip","family":"lua-fam","case":"i","tier":"unit","checks":"x","inputs":{"n":9},"expect":"x","intrinsic":{"lang":"lua","reason":"a Lua string escape"}}` + "\n"
	refused(t, fixture+plain, "fixture.jsonl:15:", "is not a union row")
	refused(t, fixture+intrinsicRow("roundtrip.fam.i", "lua", "a Lua string escape", lua), "fixture.jsonl:15:", "its family is named lua-<subject>")
	refused(t, fixture+intrinsicRow("roundtrip.lua-fam.i", "lua", "a Lua string escape", lua)+intrinsicRow("roundtrip.go-fam.i", "go", "a Go string escape", `{"go":{"package":"./g","test":"TestGoEscape"}}`), "fixture.jsonl:16:", "a case two languages answer is one union row")
	free := `{"rec":"test","key":"tooling.lua-self.i","type":"tooling","family":"lua-self","case":"i","tier":"unit","checks":"x","inputs":{"n":1},"expect":"x","scope":"none","adapter":"fake","intrinsic":{"lang":"lua","reason":"a Lua string escape"}}` + "\n"
	refused(t, fixture+free, "fixture.jsonl:15:", "so it is a per-language row")
}
