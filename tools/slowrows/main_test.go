package main

import (
	"os"
	"strings"
	"testing"
)

// fixture is a go-test job in ci-full.yml's shape: a complement row, a
// two-row package split, an Elixir row, a setup step that is no row, and a
// row step that runs make (a leg gate, not a go test row).
const fixture = `jobs:
  go-test:
    steps:
      - name: install the Lua pin
        if: startsWith(matrix.group, 'unit-codegen-lua-')
        run: make dist-lua
      - name: go test the emitter packages (complement)
        if: matrix.group == 'unit-codegen'
        env:
          SCHEMA_SLOW: '1'
        run: go test -timeout 0 -skip '^TestNameCorpusBuilds' $(go list ./internal/codegen/... | grep -v '/luatable$' | grep -v '/lua$')
      - name: go test the name corpus
        if: matrix.group == 'unit-codegen'
        env:
          SCHEMA_SLOW: '1'
          SCHEMA_REQUIRE_CORPUS: '1'
        run: go test -timeout 0 -v ./internal/codegen/c -run '^TestNameCorpusBuilds'
      - name: go test the Lua emitter packages, row 1 of 2
        if: matrix.group == 'unit-codegen-lua-1'
        env:
          SCHEMA_SLOW: '1'
        run: |
          go test -v ./internal/codegen/lua
          go test -v ./internal/codegen/luatable -run '^TestLuaA$'
      - name: go test the Lua emitter packages, row 2 of 2
        if: matrix.group == 'unit-codegen-lua-2'
        env:
          SCHEMA_SLOW: '1'
        run: go test -v ./internal/codegen/luatable -skip '^TestLuaA$'
      - name: the Lobster leg gates
        if: matrix.group == 'unit-codegen-lobster-3'
        run: |
          make tables-lobster
      - name: go test the Elixir emitter package, row 1 of 1
        if: matrix.group == 'unit-elixir-1'
        env:
          SCHEMA_SLOW: '1'
        run: go test -count=1 -v ./internal/codegen/elixirtable
      - name: go test the compiler package, row 1 of 1
        if: matrix.group == 'unit-compiler-1'
        env:
          SCHEMA_SLOW: '1'
        run: go test -v ./compiler
  next-job:
    steps:
      - name: not this job
        if: matrix.group == 'unit-codegen-go-1'
        env:
          SCHEMA_SLOW: '1'
        run: go test ./internal/codegen/gotable
`

func fakeList(pattern string) ([]string, error) {
	return []string{"./internal/codegen/c", "./internal/codegen/lua", "./internal/codegen/luatable", "./internal/codegen/elixirtable", "./internal/codegen/rust"}, nil
}

func allHaveTests(string) bool { return true }

func mustRows(t *testing.T, content string) []row {
	t.Helper()
	rows, err := parseRows(content)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestParseRowsReadsTheEmitterRowsOnly(t *testing.T) {
	rows := mustRows(t, fixture)
	var groups []string
	for _, r := range rows {
		groups = append(groups, r.Group)
	}
	if got := strings.Join(groups, " "); got != "unit-codegen unit-codegen-lua-1 unit-codegen-lua-2 unit-elixir-1" {
		t.Fatalf("rows = %q", got)
	}
	comp := rows[0]
	if len(comp.Commands) != 2 {
		t.Fatalf("unit-codegen has %d commands, want the complement and the name corpus", len(comp.Commands))
	}
	c := comp.Commands[0]
	if c.ListPattern != "./internal/codegen/..." || strings.Join(c.Drop, ",") != "/luatable$,/lua$" || c.Skip != "^TestNameCorpusBuilds" || c.Timeout != "0" || c.RequireCorpus {
		t.Fatalf("complement parsed as %+v", c)
	}
	if !comp.Commands[1].RequireCorpus || comp.Commands[1].Run != "^TestNameCorpusBuilds" {
		t.Fatalf("name corpus parsed as %+v", comp.Commands[1])
	}
	if l1 := rows[1]; len(l1.Commands) != 2 || l1.Commands[1].Run != "^TestLuaA$" {
		t.Fatalf("unit-codegen-lua-1 parsed as %+v", l1)
	}
	if e := rows[3].Commands[0]; !e.CountOne || !e.Verbose {
		t.Fatalf("unit-elixir-1 parsed as %+v", e)
	}
}

func TestParseRowsRefusesWhatItCannotRun(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"a row step without SCHEMA_SLOW": func(s string) string {
			return strings.Replace(s, "        env:\n          SCHEMA_SLOW: '1'\n        run: go test -v ./internal/codegen/luatable -skip", "        run: go test -v ./internal/codegen/luatable -skip", 1)
		},
		"a go test word it cannot read": func(s string) string {
			return strings.Replace(s, "go test -count=1 -v ./internal/codegen/elixirtable", "go test -count=1 -race ./internal/codegen/elixirtable", 1)
		},
		"shell syntax": func(s string) string {
			return strings.Replace(s, "go test -count=1 -v ./internal/codegen/elixirtable", "go test -count=1 -v ./internal/codegen/elixirtable || true", 1)
		},
		"a row step that mixes make and go test": func(s string) string {
			return strings.Replace(s, "          make tables-lobster\n", "          make tables-lobster\n          go test ./internal/codegen/lobster\n", 1)
		},
		"no go-test job": func(s string) string { return strings.Replace(s, "  go-test:", "  go-tests:", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			mutated := mutate(fixture)
			if mutated == fixture {
				t.Fatal("the mutation did not apply")
			}
			if _, err := parseRows(mutated); err == nil {
				t.Fatal("parseRows accepted it")
			}
		})
	}
}

func TestSelectRowsByPackage(t *testing.T) {
	rows := mustRows(t, fixture)
	names := func(ss []setup) string {
		var out []string
		for _, s := range ss {
			out = append(out, s.Row)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		pkgs []string
		all  bool
		want string
	}{
		{[]string{"./internal/codegen/luatable"}, false, "unit-codegen-lua-1 unit-codegen-lua-2"},
		{[]string{"./internal/codegen/lua"}, false, "unit-codegen-lua-1"},
		{[]string{"./internal/codegen/rust"}, false, "unit-codegen"},
		{[]string{"internal/codegen/c/"}, false, "unit-codegen"},
		{[]string{"./internal/codegen/elixirtable", "./compiler"}, false, "unit-codegen unit-elixir-1"},
		{[]string{"./compiler", "./internal/ci"}, false, ""},
		{nil, true, "unit-codegen unit-codegen-lua-1 unit-codegen-lua-2 unit-elixir-1"},
	} {
		got, err := selectRows(rows, nil, tc.pkgs, tc.all, fakeList, allHaveTests)
		if err != nil {
			t.Fatalf("%v: %v", tc.pkgs, err)
		}
		if names(got) != tc.want {
			t.Errorf("select %v (all=%v) = %q, want %q", tc.pkgs, tc.all, names(got), tc.want)
		}
	}
}

// TestSelectRowsRefusesAnEmitterPackageWithNoRow is the plant: the
// complement stops covering internal/codegen/rust and no row names it, so a
// pull request touching it would run none of its slow tests.
func TestSelectRowsRefusesAnEmitterPackageWithNoRow(t *testing.T) {
	planted := strings.Replace(fixture, "grep -v '/lua$')", "grep -v '/lua$' | grep -v '/rust$')", 1)
	if planted == fixture {
		t.Fatal("the plant did not apply")
	}
	rows := mustRows(t, planted)
	list := func(string) ([]string, error) { return fakeList("") }
	_, err := selectRows(rows, nil, []string{"./internal/codegen/rust"}, false, list, allHaveTests)
	if err == nil || !strings.Contains(err.Error(), "./internal/codegen/rust") {
		t.Fatalf("an emitter package with no slow row was selected quietly: %v", err)
	}
	// a package without tests, or outside internal/codegen, owes no row
	if _, err := selectRows(rows, nil, []string{"./internal/codegen/rust"}, false, list, func(string) bool { return false }); err != nil {
		t.Fatalf("a package with no tests was refused: %v", err)
	}
	if _, err := selectRows(rows, nil, []string{"./tools/gotestrows"}, false, list, allHaveTests); err != nil {
		t.Fatalf("a package outside internal/codegen was refused: %v", err)
	}
}

func TestSetupForNamesEachRowsToolchain(t *testing.T) {
	for group, want := range map[string]setup{
		"unit-codegen":            {Name: "unit-codegen", Row: "unit-codegen"},
		"unit-codegen-lua-2":      {Name: "unit-codegen-lua-2", Row: "unit-codegen-lua-2", Pins: "lua"},
		"unit-codegen-luau-1":     {Name: "unit-codegen-luau-1", Row: "unit-codegen-luau-1", Pins: "luau"},
		"unit-codegen-goose-5":    {Name: "unit-codegen-goose-5", Row: "unit-codegen-goose-5", Pins: "goose"},
		"unit-codegen-lobster-1":  {Name: "unit-codegen-lobster-1", Row: "unit-codegen-lobster-1", Pins: "lobster"},
		"unit-codegen-squirrel-1": {Name: "unit-codegen-squirrel-1", Row: "unit-codegen-squirrel-1", Squirrel: "yes"},
		"unit-codegen-moo-2":      {Name: "unit-codegen-moo-2", Row: "unit-codegen-moo-2", Moo: "yes"},
		"unit-codegen-python-16":  {Name: "unit-codegen-python-16", Row: "unit-codegen-python-16", Python: "yes"},
		"unit-codegen-ruby-3":     {Name: "unit-codegen-ruby-3", Row: "unit-codegen-ruby-3", Ruby: "yes"},
		"unit-codegen-cs-1":       {Name: "unit-codegen-cs-1", Row: "unit-codegen-cs-1"},
		"unit-elixir-15":          {Name: "unit-elixir-15", Row: "unit-elixir-15"},
	} {
		if got := setupFor(group); got != want {
			t.Errorf("setupFor(%s) = %+v, want %+v", group, got, want)
		}
	}
}

func TestArgvKeepsTheRowsFlags(t *testing.T) {
	inv := invocation{cmd: command{Verbose: true, Timeout: "0"}, pkgs: []string{"./a", "./b"}, run: "^TestA$", skip: "^TestB$"}
	got := strings.Join(inv.argv(), " ")
	if got != "test -v -count=1 -timeout 0 ./a ./b -run ^TestA$ -skip ^TestB$" {
		t.Fatalf("argv = %q", got)
	}
}

func TestVerifyRowHoldsTheTestCount(t *testing.T) {
	r := row{Group: "unit-codegen-lua-2", Commands: []command{{Packages: []string{"./internal/codegen/luatable"}, Skip: "^TestLuaA$"}}}
	tests := func(string) ([]string, error) { return []string{"TestLuaA", "TestLuaB", "TestLuaC", "FuzzLua"}, nil }
	key := func(n string) pkgTest { return pkgTest{"./internal/codegen/luatable", n} }

	line, err := verifyRow(r, part{}, fakeList, tests, map[pkgTest]string{key("TestLuaB"): "pass", key("TestLuaC"): "skip", key("FuzzLua"): "pass"})
	if err != nil || line != "unit-codegen-lua-2: ran 3, failed 0" {
		t.Fatalf("a whole row: %q %v", line, err)
	}
	for name, tc := range map[string]struct {
		got  map[pkgTest]string
		want string
	}{
		"a selected test never reported": {map[pkgTest]string{key("TestLuaB"): "pass", key("FuzzLua"): "pass"}, "TestLuaC was selected and never reported"},
		"no record of the row":           {map[pkgTest]string{}, "never reported"},
		"every selected test skipped":    {map[pkgTest]string{key("TestLuaB"): "skip", key("TestLuaC"): "skip", key("FuzzLua"): "skip"}, "passed none of the 3 tests"},
		"a failure":                      {map[pkgTest]string{key("TestLuaB"): "fail", key("TestLuaC"): "pass", key("FuzzLua"): "pass"}, "1 tests failed"},
	} {
		if _, err := verifyRow(r, part{}, fakeList, tests, tc.got); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, tc.want)
		}
	}
	// a row whose patterns select nothing ran nothing, whatever the record
	empty := row{Group: "unit-codegen-lua-9", Commands: []command{{Packages: []string{"./internal/codegen/luatable"}, Run: "^TestNoSuchTest$"}}}
	if _, err := verifyRow(empty, part{}, fakeList, tests, map[pkgTest]string{key("TestLuaB"): "pass"}); err == nil || !strings.Contains(err.Error(), "selects no test") {
		t.Fatalf("a row that selects no test: %v", err)
	}
}

func TestShellWords(t *testing.T) {
	got, err := shellWords(`go test -run '^Test(A|B)$' $(go list ./x/... | grep -v '/y$') ./z`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "test", "-run", "^Test(A|B)$", "$(go list ./x/... | grep -v '/y$')", "./z"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("words = %q", got)
	}
	for _, bad := range []string{`go test './x`, `go test $(go list ./x/...`, `go test ./x && echo`, `go test "$PKG"`} {
		if _, err := shellWords(bad); err == nil {
			t.Errorf("shellWords(%q) accepted it", bad)
		}
	}
}

// TestPartsRunEverySelectedTestExactlyOnce deals a two-command row round
// three parts: every test the row selects runs in exactly one part, and the
// parts' go tests name their tests exactly.
func TestPartsRunEverySelectedTestExactlyOnce(t *testing.T) {
	r := row{Group: "unit-codegen-lua-1", Commands: []command{
		{Packages: []string{"./internal/codegen/lua"}, Verbose: true},
		{Packages: []string{"./internal/codegen/luatable"}, Skip: "^TestLuaZ$", RequireCorpus: true},
	}}
	tests := func(pkg string) ([]string, error) {
		if pkg == "./internal/codegen/lua" {
			return []string{"TestA", "TestB"}, nil
		}
		return []string{"TestLuaA", "TestLuaB", "TestLuaC", "TestLuaZ"}, nil
	}
	seen := map[pkgTest]int{}
	for k := 1; k <= 3; k++ {
		invs, err := invocations(r, part{k, 3}, fakeList, tests)
		if err != nil {
			t.Fatal(err)
		}
		for _, inv := range invs {
			if len(inv.pkgs) != 1 || inv.skip != "" {
				t.Fatalf("part %d runs %+v; want one package by name", k, inv)
			}
			names, err := tests(inv.pkgs[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range names {
				ok, err := inv.selects(n)
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					seen[pkgTest{inv.pkgs[0], n}]++
				}
			}
			if inv.pkgs[0] == "./internal/codegen/luatable" && !inv.cmd.RequireCorpus {
				t.Fatalf("part %d dropped its command's SCHEMA_REQUIRE_CORPUS", k)
			}
		}
	}
	want := []pkgTest{{"./internal/codegen/lua", "TestA"}, {"./internal/codegen/lua", "TestB"}, {"./internal/codegen/luatable", "TestLuaA"}, {"./internal/codegen/luatable", "TestLuaB"}, {"./internal/codegen/luatable", "TestLuaC"}}
	if len(seen) != len(want) {
		t.Fatalf("the parts ran %v; want each of %v once", seen, want)
	}
	for _, w := range want {
		if seen[w] != 1 {
			t.Errorf("%v ran in %d parts", w, seen[w])
		}
	}
	// a part with nothing dealt to it is refused, never an empty green
	if _, err := invocations(r, part{6, 6}, fakeList, tests); err == nil || !strings.Contains(err.Error(), "selects no test") {
		t.Fatalf("part 6 of 6 over five tests: %v", err)
	}
	// a part holds its own count: a dealt test that never reported is red
	invs, err := invocations(r, part{1, 3}, fakeList, tests)
	if err != nil {
		t.Fatal(err)
	}
	got := map[pkgTest]string{}
	for _, inv := range invs {
		names, err := tests(inv.pkgs[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			ok, err := inv.selects(n)
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				got[pkgTest{inv.pkgs[0], n}] = "pass"
			}
		}
	}
	if line, err := verifyRow(r, part{1, 3}, fakeList, tests, got); err != nil || !strings.HasPrefix(line, "unit-codegen-lua-1 1/3: ran ") {
		t.Fatalf("a whole part: %q %v", line, err)
	}
	for k := range got {
		delete(got, k)
		break
	}
	if _, err := verifyRow(r, part{1, 3}, fakeList, tests, got); err == nil || !strings.Contains(err.Error(), "never reported") {
		t.Fatalf("a part missing a test: %v", err)
	}
}

func TestParsePart(t *testing.T) {
	if p, err := parsePart("2/5"); err != nil || p != (part{2, 5}) {
		t.Fatalf("2/5: %v %v", p, err)
	}
	if p, err := parsePart(""); err != nil || p != (part{}) {
		t.Fatalf("empty: %v %v", p, err)
	}
	for _, bad := range []string{"0/2", "3/2", "1/1", "1/2x", "a/b", "01/2"} {
		if _, err := parsePart(bad); err == nil {
			t.Errorf("parsePart(%q) accepted it", bad)
		}
	}
}

func TestReadTableAndSelectParts(t *testing.T) {
	rows := mustRows(t, fixture)
	dir := t.TempDir()
	write := func(body string) string {
		p := dir + "/slow-rows.txt"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	parts, err := readTable(write("# comment\nparts unit-elixir-1 3  # measured 2026-11-23\n"), rows)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := selectRows(rows, parts, []string{"./internal/codegen/elixirtable"}, false, fakeList, allHaveTests)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range sel {
		names = append(names, s.Name+"="+s.Part)
	}
	if got := strings.Join(names, ","); got != "unit-codegen=,unit-elixir-1 1/3=1/3,unit-elixir-1 2/3=2/3,unit-elixir-1 3/3=3/3" {
		t.Fatalf("selected %s", got)
	}
	for _, bad := range []string{"parts unit-elixir-9 2\n", "parts unit-elixir-1 1\n", "parts unit-elixir-1\n", "parts unit-elixir-1 2\nparts unit-elixir-1 3\n", "split unit-elixir-1 2\n"} {
		if _, err := readTable(write(bad), rows); err == nil {
			t.Errorf("readTable accepted %q", bad)
		}
	}
}
