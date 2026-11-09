package rusttable

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRustCardsStrictNames pins the Rust cards facade, driver, and tests
// (tables/cards/rust) to the strict TableReport names and strict read
// (docs/SPEC-TABLES.md §4, §6.5; docs/plans/strict-only.md), builds and runs
// the crate unit tests, and checks all nine card conformance surfaces
// (load, save, render, parse, bounds, upgrade, edit, taskhash, view) against
// tables/cards/conformance/expected/.
//
// TestRustCardsStrictNamesNegativeControl plants retired TableReport field
// names and behavioural sabotages into copies of the real tables/cards/rust
// artifacts and requires each check to go red.

var rustCardsRetiredNames = []string{
	".kind_mismatch",
	".clamped",
	".duplicate",
	".malformed",
	".mismatch",
	".oor",
	".repeated",
	"card_load_tolerant",
	"card_from_json_tolerant",
	"card_load_retain_tolerant",
}

var rustCardsRequiredLibFragments = []string{
	"card_load(wire, &mut report)",
	"card_from_json(text, &mut report)",
	"card_load_retain(wire, &mut retain, &mut report)",
	"if report.damaged || report.refused",
}

var rustCardsSurfaces = []string{
	"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view",
}

func rustCardsRepoRoot(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func requireRustcForCards(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("rustc"); err == nil {
		return p
	}
	for _, candidate := range []string{
		"/opt/homebrew/opt/rustup/bin/rustc",
		filepath.Join(os.Getenv("HOME"), ".cargo/bin/rustc"),
	} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH")
	}
	t.Skip("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset")
	return ""
}

func rustCardsReadSources(t *testing.T, repo string) map[string]string {
	t.Helper()
	rels := []string{
		"src/lib.rs",
		"src/bin/cards-driver.rs",
		"tests/cards.rs",
	}
	out := make(map[string]string, len(rels))
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(repo, "tables", "cards", "rust", rel))
		if err != nil {
			t.Fatal(err)
		}
		out[rel] = string(b)
	}
	return out
}

func rustCardsStrictSourceCheck(sources map[string]string) []string {
	var bad []string
	for name, src := range sources {
		for _, retired := range rustCardsRetiredNames {
			if strings.Contains(src, retired) {
				bad = append(bad, fmt.Sprintf("%s: uses retired name %q", name, retired))
			}
		}
	}
	if lib, ok := sources["src/lib.rs"]; ok {
		for _, req := range rustCardsRequiredLibFragments {
			if !strings.Contains(lib, req) {
				bad = append(bad, fmt.Sprintf("src/lib.rs: missing strict fragment %q", req))
			}
		}
	} else {
		bad = append(bad, "src/lib.rs: missing")
	}
	if drv, ok := sources["src/bin/cards-driver.rs"]; ok {
		const wantZeroLines = `s.push_str("KindMismatch=0\nClamped=0\nDuplicate=0\n");`
		const wantMalformed = `s.push_str(&format!("Malformed={}\n", r.damaged));`
		if !strings.Contains(drv, wantZeroLines) {
			bad = append(bad, "src/bin/cards-driver.rs: missing strict zero report lines")
		}
		if !strings.Contains(drv, wantMalformed) {
			bad = append(bad, "src/bin/cards-driver.rs: missing Malformed from r.damaged")
		}
	} else {
		bad = append(bad, "src/bin/cards-driver.rs: missing")
	}
	if tst, ok := sources["tests/cards.rs"]; ok {
		const wantFmt = `KindMismatch=0\nClamped=0\nDuplicate=0\nMalformed={}\n`
		if !strings.Contains(tst, wantFmt) {
			bad = append(bad, "tests/cards.rs: missing strict report_lines format")
		}
	} else {
		bad = append(bad, "tests/cards.rs: missing")
	}
	return bad
}

func rustCardsGeneratedModules(t *testing.T, repo string) map[string][]byte {
	t.Helper()
	schemaPath := filepath.Join(repo, "tables", "cards", "Card.schema")
	src, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse("Card.schema", src)
	if len(perrs) > 0 {
		t.Fatalf("parse Card.schema: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Card.schema", Name: "Card.schema", Base: "Card", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check Card.schema: %v", cerrs[0])
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate Card.schema: %v", err)
	}
	return modules
}

func rustCardsBuildAndRun(t *testing.T, rustc, repo string, generated map[string][]byte, sources map[string]string) error {
	t.Helper()
	stage := t.TempDir()
	for _, name := range []string{
		"variable_runtime.rs",
		"card_table.rs",
		"table_enums.rs",
		"card_variable.rs",
		"card_descriptors.rs",
	} {
		data, ok := generated[name]
		if !ok {
			return fmt.Errorf("missing generated module %s", name)
		}
		if err := os.WriteFile(filepath.Join(stage, name), data, 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "lib.rs"), []byte(sources["src/lib.rs"]), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "tests.rs"), []byte(sources["tests/cards.rs"]), 0o600); err != nil {
		return err
	}
	binDir := filepath.Join(stage, "src", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(binDir, "cards-driver.rs"), []byte(sources["src/bin/cards-driver.rs"]), 0o600); err != nil {
		return err
	}

	testBin := filepath.Join(stage, "cards-tests")
	buildTests := exec.Command(rustc, "--edition", "2024", "--test", "--crate-name", "cards", "-o", testBin, filepath.Join(stage, "lib.rs"))
	buildTests.Dir = stage
	if out, err := buildTests.CombinedOutput(); err != nil {
		return fmt.Errorf("rustc --test lib.rs: %v\n%s", err, out)
	}
	runTests := exec.Command(testBin)
	runTests.Dir = repo
	if out, err := runTests.CombinedOutput(); err != nil {
		return fmt.Errorf("cards unit tests failed: %v\n%s", err, out)
	}

	rlib := filepath.Join(stage, "libcards.rlib")
	buildLib := exec.Command(rustc, "--edition", "2024", "--crate-type", "rlib", "--crate-name", "cards", "-o", rlib, filepath.Join(stage, "lib.rs"))
	buildLib.Dir = stage
	if out, err := buildLib.CombinedOutput(); err != nil {
		return fmt.Errorf("rustc rlib lib.rs: %v\n%s", err, out)
	}

	driverBin := filepath.Join(stage, "cards-driver")
	buildDriver := exec.Command(rustc, "--edition", "2024", "--extern", "cards="+rlib, "-o", driverBin, filepath.Join(binDir, "cards-driver.rs"))
	buildDriver.Dir = stage
	if out, err := buildDriver.CombinedOutput(); err != nil {
		return fmt.Errorf("rustc cards-driver.rs: %v\n%s", err, out)
	}

	manifestPath := filepath.Join(repo, "tables", "cards", "conformance", "MANIFEST.txt")
	expectedDir := filepath.Join(repo, "tables", "cards", "conformance", "expected")
	rowsBySurface, err := rustCardsManifestRows(manifestPath)
	if err != nil {
		return err
	}

	for _, surface := range rustCardsSurfaces {
		outDir := t.TempDir()
		cmd := exec.Command(driverBin, manifestPath, surface, outDir)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("driver surface %s failed: %v\n%s", surface, err, out)
		}
		if err := rustCardsCheckSurface(surface, rowsBySurface[surface], outDir, expectedDir); err != nil {
			return err
		}
	}
	return nil
}

func rustCardsManifestRows(manifestPath string) (map[string][][]string, error) {
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string][][]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("short manifest row: %q", sc.Text())
		}
		out[fields[0]] = append(out[fields[0]], fields)
	}
	return out, sc.Err()
}

func rustCardsCheckSurface(surface string, rows [][]string, outDir, expectedDir string) error {
	if len(rows) == 0 {
		return fmt.Errorf("surface %s: zero manifest rows", surface)
	}
	checkFile := func(name string) error {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return fmt.Errorf("surface %s: missing output %s: %v", surface, name, err)
		}
		want, err := os.ReadFile(filepath.Join(expectedDir, name))
		if err != nil {
			return fmt.Errorf("surface %s: missing expected %s: %v", surface, name, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("surface %s: %s differs from expected:\ngot:\n%s\nwant:\n%s", surface, name, got, want)
		}
		return nil
	}
	checkEither := func(caseName, cleanExt string) error {
		refusedName := caseName + ".refused"
		cleanName := caseName + "." + cleanExt
		if _, err := os.Stat(filepath.Join(expectedDir, refusedName)); err == nil {
			if _, err := os.Stat(filepath.Join(outDir, cleanName)); err == nil {
				return fmt.Errorf("surface %s: %s produced %s when %s was owed", surface, caseName, cleanName, refusedName)
			}
			return checkFile(refusedName)
		}
		if _, err := os.Stat(filepath.Join(outDir, refusedName)); err == nil {
			return fmt.Errorf("surface %s: %s refused when %s was owed", surface, caseName, cleanName)
		}
		return checkFile(cleanName)
	}

	for _, row := range rows {
		name := row[1]
		switch surface {
		case "load":
			if err := checkFile(name + ".json"); err != nil {
				return err
			}
			if err := checkFile(name + ".report"); err != nil {
				return err
			}
		case "save", "parse", "bounds", "edit":
			if err := checkEither(name, "wire"); err != nil {
				return err
			}
		case "render":
			if err := checkFile(name + ".txt"); err != nil {
				return err
			}
		case "upgrade":
			if err := checkEither(name, "wire"); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(expectedDir, name+".report")); err == nil {
				if err := checkFile(name + ".report"); err != nil {
					return err
				}
			}
		case "taskhash":
			if err := checkFile(name + ".hex"); err != nil {
				return err
			}
		case "view":
			if err := checkFile(name + ".txt"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown surface %s", surface)
		}
	}
	return nil
}

func TestRustCardsStrictNames(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (builds and runs the Rust cards facade, unit tests, and 9 conformance surfaces)")
	rustc := requireRustcForCards(t)
	repo := rustCardsRepoRoot(t)
	sources := rustCardsReadSources(t, repo)
	if bad := rustCardsStrictSourceCheck(sources); len(bad) != 0 {
		t.Fatalf("tables/cards/rust sources are not on the strict TableReport names:\n%s", strings.Join(bad, "\n"))
	}
	generated := rustCardsGeneratedModules(t, repo)
	if err := rustCardsBuildAndRun(t, rustc, repo, generated, sources); err != nil {
		t.Fatal(err)
	}
}

func TestRustCardsStrictNamesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (negative controls for the Rust cards strict names and conformance surfaces)")
	rustc := requireRustcForCards(t)
	repo := rustCardsRepoRoot(t)
	baseSources := rustCardsReadSources(t, repo)
	generated := rustCardsGeneratedModules(t, repo)

	sabotages := []struct {
		name      string
		file      string
		from      string
		to        string
		checkOnly bool
	}{
		{
			name: "retired-kind-mismatch-in-lib",
			file: "src/lib.rs",
			from: "if report.unknown > 0 {",
			to:   "if report.unknown > 0 || report.kind_mismatch > 0 {",
		},
		{
			name: "retired-clamped-in-driver",
			file: "src/bin/cards-driver.rs",
			from: `s.push_str("KindMismatch=0\nClamped=0\nDuplicate=0\n");`,
			to:   `s.push_str(&format!("KindMismatch=0\nClamped={}\nDuplicate=0\n", r.clamped));`,
		},
		{
			name: "retired-malformed-in-tests",
			file: "tests/cards.rs",
			from: "r.retained, r.retain_lost, r.widened, r.unknown, r.damaged",
			to:   "r.retained, r.retain_lost, r.widened, r.unknown, r.malformed",
		},

		{
			name: "drop-unknown-refusal-in-edit",
			file: "src/lib.rs",
			from: "if report.unknown > 0 || report.retain_lost > 0 || report.damaged || report.refused {",
			to:   "if report.retain_lost > 0 || report.damaged || report.refused {",
		},
		{
			name: "wrong-malformed-in-driver-report",
			file: "src/bin/cards-driver.rs",
			from: `s.push_str(&format!("Malformed={}\n", r.damaged));`,
			to:   `s.push_str("Malformed=true\n");`,
		},
	}

	for _, tc := range sabotages {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutated := maps.Clone(baseSources)
			orig, ok := mutated[tc.file]
			if !ok {
				t.Fatalf("missing %s", tc.file)
			}
			if !strings.Contains(orig, tc.from) {
				t.Fatalf("%s: sabotage anchor %q not found", tc.file, tc.from)
			}
			mutated[tc.file] = strings.Replace(orig, tc.from, tc.to, 1)

			bad := rustCardsStrictSourceCheck(mutated)
			err := rustCardsBuildAndRun(t, rustc, repo, generated, mutated)
			if len(bad) == 0 && err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): sabotaged %s stayed green", tc.name, tc.file)
			}
		})
	}
}
