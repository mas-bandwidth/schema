package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tests here are the negative controls of one rule: the scan proves it
// ran. Each plants, in a scratch tree, a line or a package the scan used to
// leave out without a word, and requires it to be placed or refused by name.

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// helperGatedTree is a module whose leg package reaches the gate only through
// a runner package it imports: no test source of the leg spells slowtest.Gate.
func helperGatedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":           "module example.com/root\n",
		"runner/runner.go": "package runner\n\nimport \"testing\"\n\nfunc Require(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"leg/leg_test.go":  "package leg\n\nimport (\n\t\"testing\"\n\n\t\"example.com/root/runner\"\n)\n\nfunc TestLeg(t *testing.T) { runner.Require(t) }\nfunc TestPlain(t *testing.T) {}\n",
		"direct/a_test.go": "package direct\n\nimport \"testing\"\n\nfunc TestDirect(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
		"plain/a_test.go":  "package plain\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	})
	return root
}

// TestGatedPackagesIncludeThoseGatedThroughAHelper: read by the marker alone,
// ./leg was not a gated package, and a bare `go test ./leg` was a recipe line
// the static audit never judged. In the real tree that was six packages
// holding 83 gated tests.
func TestGatedPackagesIncludeThoseGatedThroughAHelper(t *testing.T) {
	t.Parallel()
	root := helperGatedTree(t)
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for pkg := range gated {
		got = append(got, pkg)
	}
	slices.Sort(got)
	if want := []string{"./direct", "./leg"}; !slices.Equal(got, want) {
		t.Fatalf("gated packages = %v, want %v (./leg reaches the gate through runner.Require)", got, want)
	}

	writeTree(t, root, map[string]string{"Makefile": "check:\n\tgo test ./leg\n"})
	recipes, err := scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipes) != 1 || recipes[0].armed || recipes[0].packages[0] != "./leg" {
		t.Fatalf("a bare `go test ./leg` must be a bare recipe on ./leg, got %+v", recipes)
	}
}

// TestGatedScanRefusesASourceItCannotRead: a test source the parser refuses
// used to be skipped, so its package was called ungated without being read.
func TestGatedScanRefusesASourceItCannotRead(t *testing.T) {
	t.Parallel()
	root := helperGatedTree(t)
	writeTree(t, root, map[string]string{"broken/a_test.go": "package broken\n\nfunc TestHalf(t *testing.T) {\n"})
	if _, err := gatedTestPackages(root); err == nil {
		t.Error("gatedTestPackages read a tree with an unparsable test source and returned no error")
	}
	if _, err := gatedPackages(root); err == nil {
		t.Error("gatedPackages read a tree with an unparsable test source and returned no error")
	}
}

// TestScanReadsEveryGoTestLine plants the spellings of `go test` the scan used
// to pass over, and requires each to be a recipe on the gated package it runs.
func TestScanReadsEveryGoTestLine(t *testing.T) {
	t.Parallel()
	root := helperGatedTree(t)
	writeTree(t, root, map[string]string{
		"Makefile": strings.Join([]string{
			"GO ?= go",
			"LEG_FAST = ./leg -run '^TestPlain$$'",
			"define leg_proof",
			"\tsh test/slowgate/proof $(1) '$(2)' -count=1 -run '^$(2)$$$$' ./$(3)/",
			"endef",
			"through-variable:",
			"\t$(GO) test ./leg",
			"packages-in-variable:",
			"\tgo test -count=1 $(LEG_FAST)",
			"implicit-package:",
			"\tcd leg && go test -run '^TestLeg$$' -count=1",
			"subtree-under-cd:",
			"\tcd plain && go test ./...",
			"through-define:",
			"\t$(call leg_proof,$@,TestLeg,leg)",
			"text-not-command:",
			"\t@printf 'go test/conformance/go/driver\\n' > build/drivers.txt",
			"",
		}, "\n"),
	})
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	recipes, err := scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]recipe{}
	for _, r := range recipes {
		got[r.target] = r
	}
	for _, target := range []string{"through-variable", "packages-in-variable", "implicit-package", "through-define"} {
		r, ok := got[target]
		if !ok {
			t.Errorf("%s runs a test of the gated ./leg and the scan did not read it as a recipe; recipes: %+v", target, recipes)
			continue
		}
		if len(r.packages) != 1 || r.packages[0] != "./leg" {
			t.Errorf("%s: packages = %v, want [./leg]", target, r.packages)
		}
	}
	if r := got["through-define"]; !r.armed {
		t.Errorf("through-define runs the proof script and must be armed: %+v", r)
	}
	if r := got["packages-in-variable"]; r.runPat != "^TestPlain$" {
		t.Errorf("packages-in-variable: -run = %q, want the pattern its variable carries", r.runPat)
	}
	for _, target := range []string{"subtree-under-cd", "text-not-command"} {
		if r, ok := got[target]; ok {
			t.Errorf("%s names no gated package and was read as a recipe: %+v", target, r)
		}
	}

	// the proof list of the define's call is read with the call's arguments
	refs, unread := proofRefsFromRecipes(recipes)
	if len(unread) != 0 {
		t.Errorf("unread proof lines: %v", unread)
	}
	if len(refs) != 1 || refs[0].name != "TestLeg" || refs[0].pkg != "./leg" {
		t.Errorf("proof names = %+v, want TestLeg in ./leg", refs)
	}

	// and the verdict: of the unarmed lines, only the one whose -run pattern
	// selects no gate-reaching test is proven; the others are bare
	gatedTests, err := gatedTestPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	for target, want := range map[string]bool{"packages-in-variable": true, "through-variable": false, "implicit-package": false} {
		if got := selectsNoGatedTest(root, got[target], gatedTests); got != want {
			t.Errorf("selectsNoGatedTest(%s) = %v, want %v", target, got, want)
		}
	}
}

// TestScanRefusesALineItCannotRead: a `go test` without the variable, on
// packages the scan cannot read, is an error by name. It used to be dropped.
func TestScanRefusesALineItCannotRead(t *testing.T) {
	t.Parallel()
	root := helperGatedTree(t)
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ recipe, want string }{
		"unreadable packages":  {"\tgo test -count=1 $$(cat build/packages.txt)", "Makefile:2: check: runs `go test` without the variable on packages this scan cannot read"},
		"unreadable directory": {"\tcd $(SOMEWHERE) && go test -run X", "Makefile:2: check: runs `go test` without the variable on packages this scan cannot read"},
		"proof on no package":  {"\tsh test/slowgate/proof label 'TestLeg' -count=1 $(PACKAGES)", "Makefile:2: check: runs test/slowgate/proof on a package this scan cannot read"},
	} {
		writeTree(t, root, map[string]string{"Makefile": "check:\n" + tc.recipe + "\n"})
		_, err := scanMakefiles(root, gated)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: scanMakefiles returned %v, want a refusal naming %q", name, err, tc.want)
		}
	}
	// armed, the same unreadable line cannot skip at the gate: it is placed
	writeTree(t, root, map[string]string{"Makefile": "check:\n\tSCHEMA_SLOW=1 go test -count=1 $$(cat build/packages.txt)\n"})
	if _, err := scanMakefiles(root, gated); err != nil {
		t.Errorf("an armed line on unreadable packages was refused: %v", err)
	}
	// and makefiles that run no `go test` at all are a scan that judged nothing
	writeTree(t, root, map[string]string{"Makefile": "check:\n\t@echo nothing to do\n"})
	if _, err := scanMakefiles(root, gated); err == nil || !strings.Contains(err.Error(), "judged nothing") {
		t.Errorf("makefiles with no `go test` line returned %v, want the judged-nothing refusal", err)
	}
}

// TestProofListRefusesALineItCannotRead: a proof line whose names the audit
// could not attribute used to be skipped, its list checked against nothing.
func TestProofListRefusesALineItCannotRead(t *testing.T) {
	t.Parallel()
	recipes := []recipe{
		{file: "make/go.mk", line: 108, target: "rows", packages: []string{"./nested/rows"},
			cmd: "\tcd nested && SLOWGATE_LOG_DIR=\"$(CURDIR)/build\" sh $(CURDIR)/test/slowgate/proof rows 'TestRowA TestRowB' ./rows/ -count=1"},
		{file: "Makefile", line: 7, target: "short", packages: []string{"./leg"},
			cmd: "\tsh test/slowgate/proof short"},
		{file: "Makefile", line: 9, target: "wide", packages: []string{"./..."},
			cmd: "\tsh test/slowgate/proof wide 'TestLeg' ./..."},
	}
	refs, unread := proofRefsFromRecipes(recipes)
	if len(refs) != 2 || refs[0].pkg != "./nested/rows" || refs[0].name != "TestRowA" || refs[1].name != "TestRowB" {
		t.Errorf("a proof run as `sh $(CURDIR)/test/slowgate/proof` from `cd nested` must yield its two names in ./nested/rows, got %+v", refs)
	}
	if len(unread) != 2 || !strings.Contains(unread[0], "Makefile:7 short") || !strings.Contains(unread[1], "Makefile:9 wide") {
		t.Errorf("unread = %v, want the short line and the ./... line, each by name", unread)
	}

	// the ownership control's names are read by a pattern: where the Go
	// conformance leg exists, a pattern that finds none is a finding
	root := t.TempDir()
	writeTree(t, root, map[string]string{"test/conformance/go/ownership-negative-control": "#!/bin/sh\n# respelled: no assignment this audit reads\n"})
	findings, err := proofListFindings(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "ownership-negative-control: no `test=<Name>` was read") {
		t.Errorf("findings = %v, want the ownership control's empty read", findings)
	}
}

// TestTreeScanCounts pins what the scan reads in this tree to floors measured
// on 2026-11-21, so a scan that quietly reads less is red here: 58 gated
// packages (52 by the marker alone), 19 proof calls through squirrel_nc, three
// conformance-go-rows proof lines, and no line it cannot place.
func TestTreeScanCounts(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"./internal/codegen/goose", "./internal/codegen/lobster", "./internal/codegen/lua", "./internal/codegen/luau", "./internal/codegen/squirrel", "./test/packet-write-bareint/lobster"} {
		if !gated[pkg] {
			t.Errorf("%s reaches slowtest.Gate through an imported runner and is missing from the gated packages", pkg)
		}
	}
	recipes, err := scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	refs, unread := proofRefsFromRecipes(recipes)
	if len(unread) != 0 {
		t.Errorf("proof lines whose names are checked against nothing:\n%s", strings.Join(unread, "\n"))
	}
	seen := map[string]int{}
	for _, r := range refs {
		switch {
		case r.file == "make/squirrel-negative-controls.mk":
			seen["squirrel_nc"]++
		case strings.HasPrefix(r.label, "conformance-go-rows-"):
			seen["rows"]++
		}
	}
	if seen["squirrel_nc"] < 19 {
		t.Errorf("read %d required names through $(call squirrel_nc,…), want at least 19", seen["squirrel_nc"])
	}
	if seen["rows"] == 0 {
		t.Error("read no required name from the conformance-go-rows proof lines")
	}
	t.Logf("gated packages: %d; recipe lines naming one: %d; required proof names: %d (%d through squirrel_nc, %d on conformance-go-rows)",
		len(gated), len(recipes), len(refs), seen["squirrel_nc"], seen["rows"])
}

// TestUnarmedLineVerdictFollowsGoSelection holds the verdict on an unarmed
// `go test -run <pattern>` to what the real run does, on a package with a gated
// TestGated (which runs a roundtrip subtest), a fast TestFast, a TestFeature
// behind `//go:build include_optional_feature` and a TestWindowsOnly in a
// _windows_test.go file. Only a line that selects a test in the build context
// it runs in, and selects no gated one, is green (0).
func TestUnarmedLineVerdictFollowsGoSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		recipe string
		want   int
		why    string
	}{
		{"go test -run 'TestGated' ./pkg", 1, "it runs the gated test, which skips"},
		{"go test -run 'TestGated/roundtrip' ./pkg", 1, "Go runs the parent when the first element matches, and the parent skips at the gate"},
		{"go test -run '^TestGated$$/^round' ./pkg", 1, "the same, anchored"},
		{"go test -run 'TestMissing' ./pkg", 1, "it selects no test, so it runs nothing"},
		{"go test -run 'TestMissing/roundtrip' ./pkg", 1, "it selects no test, so it runs nothing"},
		{"go test -run 'TestFast' ./pkg", 0, "it runs TestFast alone"},
		{"go test -run '^TestFast$$' ./pkg", 0, "it runs TestFast alone"},
		{"go test -run 'TestFast/anything' ./pkg", 0, "it runs TestFast alone"},
		{"go test -run 'Test' ./pkg", 1, "it runs every test, the gated one among them"},
		{"go test -run '(' ./pkg", 1, "the pattern does not compile"},
		// testing/match.go splits at each top-level `|` before each `/`
		{"go test -run 'TestFast/unused|TestGated/roundtrip' ./pkg", 1, "the second alternative runs TestGated"},
		{"go test -run 'TestGated/roundtrip|TestFast/unused' ./pkg", 1, "the first alternative runs TestGated"},
		{"go test -run 'TestFast/a|TestMissing/b' ./pkg", 0, "the alternatives select TestFast alone"},
		{"go test -run 'TestFast/(a|TestGated)' ./pkg", 0, "a `|` inside parentheses separates nothing: the top level is TestFast"},
		{"go test -run 'Test(Fast|Gated)/x' ./pkg", 1, "the group selects TestGated"},
		{"go test -run 'TestFa[s/|]t' ./pkg", 0, "a `/` or `|` inside brackets separates nothing"},
		{"go test -run 'TestFast' -skip 'TestFast' ./pkg", 1, "-skip is not modelled, so nothing is proven"},
		// a line that runs nothing credits nothing
		{"go test ./pkg -v -count=0 -run TestFast", 1, "-count=0 runs TestFast zero times"},
		{"go test -count 0 -run TestFast ./pkg", 1, "-count 0 runs TestFast zero times"},
		{"go test -count=$(COUNT) -run TestFast ./pkg", 1, "the count cannot be read, so nothing is proven"},
		{"go test -count=1 -run TestFast ./pkg", 0, "it runs TestFast once"},
		{"go test -list TestFast -run TestFast ./pkg", 1, "-list prints names and runs nothing"},
		{"go test -c -run TestFast ./pkg", 1, "-c builds the test binary and runs nothing"},
		// the line is read as the go tool and the test binary read it
		{"go test -run TestFast -run TestGated ./pkg", 1, "a repeated flag: the last -run wins, and it selects the gated test"},
		{"go test -run TestGated -run TestFast ./pkg", 0, "a repeated flag: the last -run wins, and it selects TestFast alone"},
		{"go test ./pkg -args -test.run TestGated", 1, "-args -test.run is the selector the test binary reads"},
		{"go test -run TestFast ./pkg -args -test.run TestGated", 1, "the -test.run after -args is the last selector"},
		{"go test ./pkg -args -test.run TestFast", 0, "-args -test.run selects TestFast alone"},
		{"go test ./pkg -v -run TestFast", 0, "a flag after the package list is still a flag"},
		{"go test ./pkg -test.run=TestGated", 1, "the go tool reads the -test. form too"},
		{"go test -run TestFast ./pkg -args -run TestGated", 1, "-run after -args is not testing's flag: undecided"},
		{"go test -run TestFast ./pkg -args -test.count=0", 1, "-test.count=0 after -args runs nothing"},
		{"go test -run $$(cat pattern.txt) ./pkg", 1, "the pattern cannot be read"},
		// declarations are read under the build context the line runs in
		{"GOOS=linux go test -run 'TestFeature' ./pkg", 1, "TestFeature is behind a build tag the line does not set: no tests to run"},
		{"GOOS=linux go test -tags include_optional_feature -run 'TestFeature' ./pkg", 0, "with the tag it runs TestFeature"},
		{"GOOS=linux go test -tags=other,include_optional_feature -run 'TestFeature' ./pkg", 0, "with the tag it runs TestFeature"},
		{"GOOS=linux go test -tags $(FEATURE_TAGS) -run 'TestFeature' ./pkg", 1, "the tags cannot be read, so nothing is proven"},
		{"GOOS=linux go test -run 'TestWindowsOnly' ./pkg", 1, "the test exists only in a _windows_test.go file: no tests to run on linux"},
		{"GOOS=windows go test -run 'TestWindowsOnly' ./pkg", 0, "on windows it runs TestWindowsOnly"},
	} {
		root := t.TempDir()
		writeTree(t, root, map[string]string{
			"go.mod":                   "module example.com/root\n",
			"pkg/a_test.go":            "package pkg\n\nimport \"testing\"\n\nfunc TestGated(t *testing.T) {\n\tslowtest.Gate(t, \"x\")\n\tt.Run(\"roundtrip\", func(t *testing.T) {})\n}\n\nfunc TestFast(t *testing.T) {}\n",
			"pkg/feature_test.go":      "//go:build include_optional_feature\n\npackage pkg\n\nimport \"testing\"\n\nfunc TestFeature(t *testing.T) {}\n",
			"pkg/only_windows_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestWindowsOnly(t *testing.T) {}\n",
			"Makefile":                 "check:\n\t" + tc.recipe + "\n",
		})
		gatedTests, err := gatedTestPackages(root)
		if err != nil {
			t.Fatal(err)
		}
		gated, err := gatedPackagesWith(root, gatedTests)
		if err != nil {
			t.Fatal(err)
		}
		recipes, err := scanMakefiles(root, gated)
		if err != nil {
			t.Fatal(err)
		}
		if len(recipes) != 1 || recipes[0].armed {
			t.Fatalf("%s: want one unarmed recipe, got %+v", tc.recipe, recipes)
		}
		if got := runStatic(root, recipes, nil, gated, gatedTests); got != tc.want {
			t.Errorf("unarmed `%s`: the static scan exited %d, want %d (%s)", tc.recipe, got, tc.want, tc.why)
		}
	}
}

// TestArmingIsTheEnvironmentTheTestSees holds "armed" to the environment the
// test binary gets, word by word, not to the variable's name appearing on the
// line: an assignment in front of the command arms; `env -u`, `unset`, an
// empty value and a value other than 1 for SCHEMA_SLOW do not; a later word
// overrides an earlier one; and a value this scan cannot read arms nothing.
func TestArmingIsTheEnvironmentTheTestSees(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		"go test ./pkg":                                                      false,
		"SCHEMA_SLOW=1 go test ./pkg":                                        true,
		"SCHEMA_REQUIRE_CORPUS=1 go test ./pkg":                              true,
		"env -u SCHEMA_REQUIRE_CORPUS go test ./pkg":                         false,
		"SCHEMA_REQUIRE_CORPUS=1 env -u SCHEMA_REQUIRE_CORPUS go test ./pkg": false,
		"env -u SCHEMA_SLOW SCHEMA_SLOW=1 go test ./pkg":                     true,
		"env SCHEMA_SLOW=1 go test ./pkg":                                    true,
		"env -i go test ./pkg":                                               false,
		"SCHEMA_SLOW=1 env -i go test ./pkg":                                 false,
		"SCHEMA_SLOW= go test ./pkg":                                         false,
		"SCHEMA_REQUIRE_CORPUS= go test ./pkg":                               false,
		"SCHEMA_SLOW=0 go test ./pkg":                                        false,
		"SCHEMA_SLOW=1 SCHEMA_SLOW=0 go test ./pkg":                          false,
		"SCHEMA_SLOW=0 SCHEMA_SLOW=1 go test ./pkg":                          true,
		"unset SCHEMA_SLOW; go test ./pkg":                                   false,
		"export SCHEMA_SLOW=1; go test ./pkg":                                true,
		"export SCHEMA_SLOW=1; unset SCHEMA_SLOW; go test ./pkg":             false,
		"export SCHEMA_SLOW=1 && SCHEMA_SLOW= go test ./pkg":                 false,
		"SCHEMA_SLOW=1 true; go test ./pkg":                                  false,
		"SCHEMA_SLOW=1; go test ./pkg":                                       false,
		"SCHEMA_SLOW=1 cd pkg && go test .":                                  false,
		"cd pkg && SCHEMA_SLOW=1 go test .":                                  true,
		"echo SCHEMA_SLOW=1 && go test ./pkg":                                false,
		"echo SCHEMA_REQUIRE_CORPUS is not set here; go test ./pkg":          false,
		"SCHEMA_REQUIRE_CORPUS=$(REQUIRE) go test ./pkg":                     false,
		"SCHEMA_REQUIRE_CORPUS=$$require go test ./pkg":                      false,
		"SCHEMA_REQUIRE_CORPUS=1 DART=$(DART) nice -n 19 go test ./pkg":      true,
		"@if SCHEMA_SLOW=1 go test ./pkg > log 2>&1; then echo red; fi":      true,
		"@if ( cd pkg && SCHEMA_SLOW=1 go test . ); then echo red; fi":       true,
		"sh test/slowgate/proof label 'TestGated' ./pkg -count=1":            true,
		"go test ./a && SCHEMA_SLOW=1 go test ./pkg":                         false,
	} {
		if got := parseGoTestLine(cmd).armed(); got != want {
			t.Errorf("armed(%q) = %v, want %v", cmd, got, want)
		}
	}
}
