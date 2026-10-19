package ci

// Reversed witnesses for the pin rules a pin's tier and a nested module add:
// docs/SPEC-TABLES.md, "Row ids and pins". The fixture is specFixture's, with
// a slow-gated test, a structure test and a nested module a fixture ci-full.yml
// may or may not run.

import (
	"testing"
	"testing/fstest"
)

func tiersFixture() fstest.MapFS {
	fx := specFixture()
	fx["pkg/a_test.go"] = &fstest.MapFile{Data: []byte(`package pkg

import (
	"testing"

	"example.com/fx/helper"
	"example.com/fx/internal/slowtest"
)

func TestShort(t *testing.T) { check(t) }

func TestCompiler(t *testing.T) { helper.Run(t) }

func TestRow3(t *testing.T) {}

func TestSkips(t *testing.T) { t.Skip("later") }

func TestHelperSkips(t *testing.T) { helper.Gate(t) }

func TestShortGated(t *testing.T) {
	if testing.Short() {
		return
	}
}

func check(t *testing.T) { t.Helper() }

func Testify(t *testing.T) {}

func TestNoT() {}

func TestMethodSkips(t *testing.T) { helper.G{}.Check(t) }

func TestSlowOnly(t *testing.T) { slowtest.Gate(t, "the slow bit") }

func TestSlowAndSkip(t *testing.T) {
	slowtest.Gate(t, "the slow bit")
	t.Skip("and another")
}

func TestSlowAndHelperSkip(t *testing.T) {
	slowtest.Gate(t, "the slow bit")
	helper.Gate(t)
}

func TestStructure(t *testing.T) {}
`)}
	fx["internal/slowtest/slowtest.go"] = &fstest.MapFile{Data: []byte(`package slowtest

import (
	"os"
	"testing"
)

func Gate(t *testing.T, why string) {
	if os.Getenv("SCHEMA_SLOW") == "" {
		t.Skip("slow: " + why)
	}
}
`)}
	return fx
}

func tiersPin(pins []specPin, i int, p specPin) []specPin {
	pins[i] = p
	return pins
}

func tiersCI(run string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`name: fixture
on: [push]
jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - name: the slow half
        env:
          SCHEMA_SLOW: '1'
        run: ` + run + `
`)}
}

func tiersNestedCI() *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(`name: fixture
on: [push]
jobs:
  go:
    runs-on: ubuntu-latest
    steps:
      - name: the nested module
        working-directory: nested
        run: go test ./...
`)}
}

func wantFinding(t *testing.T, got []string, want string) {
	t.Helper()
	for _, f := range got {
		if contains(f, want) {
			return
		}
	}
	t.Fatalf("want a finding containing %q, got:\n%v", want, got)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSpecPinTiersAndNestedModules(t *testing.T) {
	t.Parallel()

	t.Run("nested module a CI step runs resolves", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		fx[".github/workflows/ci-full.yml"] = tiersNestedCI()
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "nested/n_test.go:TestNested", Digest: specFixtureT11})
		if f := runSpecFixture(fx, pins); len(f) != 0 {
			t.Fatalf("a nested module a CI step runs was refused:\n%v", f)
		}
	})

	t.Run("nested module with no CI step is refused naming the module", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "nested/n_test.go:TestNested", Digest: specFixtureT11})
		wantFinding(t, runSpecFixture(fx, pins), "is in the nested module nested")
	})

	t.Run("only slowtest.Gate resolves as tier slow", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		findings, tier, _ := resolvePin(fx, specPin{Test: "pkg/a_test.go:TestSlowOnly"})
		if len(findings) != 0 {
			t.Fatalf("a slow-only test was refused:\n%v", findings)
		}
		if tier != "slow" {
			t.Fatalf("tier = %q, want slow", tier)
		}
	})

	t.Run("slowtest.Gate beside t.Skip is refused", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		findings, _, _ := resolvePin(fx, specPin{Test: "pkg/a_test.go:TestSlowAndSkip"})
		wantFinding(t, findings, "calls Skip")
	})

	t.Run("slowtest.Gate beside a helper's t.SkipNow is refused", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		findings, _, _ := resolvePin(fx, specPin{Test: "pkg/a_test.go:TestSlowAndHelperSkip"})
		wantFinding(t, findings, "SkipNow")
	})

	t.Run("a slow pin in the SCHEMA_SLOW=1 step passes", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		fx[".github/workflows/ci-full.yml"] = tiersCI("go test ./pkg/...")
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestSlowOnly", Digest: specFixtureT11})
		if f := runSpecFixture(fx, pins); len(f) != 0 {
			t.Fatalf("a slow pin the SCHEMA_SLOW=1 step runs was refused:\n%v", f)
		}
	})

	t.Run("a slow pin with the step removed is refused", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestSlowOnly", Digest: specFixtureT11})
		wantFinding(t, runSpecFixture(fx, pins), "TestSlowOnly")
	})

	t.Run("a -run filter that excludes the name is refused", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		fx[".github/workflows/ci-full.yml"] = tiersCI("go test -run=^TestOther$ ./pkg/...")
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestSlowOnly", Digest: specFixtureT11})
		wantFinding(t, runSpecFixture(fx, pins), "TestSlowOnly")
	})

	t.Run("a behavior pin beside a structure pin passes", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		pins := tiersPin(specFixturePins(), 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestStructure", Tier: "structure", Digest: specFixtureT11})
		if f := runSpecFixture(fx, pins); len(f) != 0 {
			t.Fatalf("a structure pin beside a behavior pin was refused:\n%v", f)
		}
	})

	t.Run("a runtime row with only a structure pin is refused naming the row", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		pins := tiersPin(specFixturePins(), 2, specPin{Row: "T1-2", Port: "compiler", Test: "pkg/a_test.go:TestStructure", Tier: "structure", Digest: "ae19772652c55406"})
		wantFinding(t, runSpecFixture(fx, pins), "row T1-2")
	})

	t.Run("a kind=structure row with only a structure pin passes", func(t *testing.T) {
		t.Parallel()
		fx := tiersFixture()
		s := string(fx["docs/SPEC.md"].Data)
		fx["docs/SPEC.md"] = &fstest.MapFile{Data: []byte(replaceOnce(s, "<!-- row T1-2 feature=compiler -->", "<!-- row T1-2 feature=compiler kind=structure -->"))}
		pins := tiersPin(specFixturePins(), 2, specPin{Row: "T1-2", Port: "compiler", Test: "pkg/a_test.go:TestStructure", Tier: "structure", Digest: "ae19772652c55406"})
		if f := runSpecFixture(fx, pins); len(f) != 0 {
			t.Fatalf("a kind=structure row with a structure pin was refused:\n%v", f)
		}
	})
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	panic("fixture edit misses: " + old)
}
