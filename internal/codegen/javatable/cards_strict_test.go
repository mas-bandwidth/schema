package javatable

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaCardsStrict pins the Java cards facade, driver, and tests
// (tables/cards/java) to the strict TableVariable.Report names and strict read
// (docs/SPEC-TABLES.md §4, §6.5; docs/plans/strict-only.md), and (when javac
// and java are available under SCHEMA_SLOW=1, as a functional test) compiles
// and runs CardsTest plus all nine card conformance surfaces (load, save,
// render, parse, bounds, upgrade, edit, taskhash, view) against
// tables/cards/conformance/expected/.
//
// TestJavaCardsStrictNegativeControl plants retired TableVariable.Report field
// names and behavioural sabotages into copies of the real tables/cards/java
// artifacts and requires both the source check and the functional build/run to
// go red.

var javaCardsRetiredNames = []string{
	".kindMismatch",
	".clamped",
	".duplicate",
	".malformed",
	".mismatch",
	".oor",
	".repeated",
	"loadTolerant",
	"fromJsonTolerant",
	"loadRetainTolerant",
}

var javaCardsRequiredFacadeFragments = []string{
	"CardVariable.load(wire, report)",
	"CardVariable.fromJson(normalizeEnumNone(text), report)",
	"CardRetain.loadRetain(wire, retain, report)",
	"if (v == null || report.damaged || report.refused)",
	"if (report.unknown > 0 || report.retainLost > 0)",
}

var javaCardsSurfaces = []string{
	"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view",
}

func javaCardsRepoRoot(t *testing.T) string {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func findJavaToolsForCards(t *testing.T) (javac, java string, ok bool) {
	t.Helper()
	var err1, err2 error
	javac, err1 = exec.LookPath("javac")
	java, err2 = exec.LookPath("java")
	if err1 == nil && err2 == nil {
		return javac, java, true
	}
	t.Skipf("javac/java not on PATH: javac=%v java=%v", err1, err2)
	return "", "", false
}

func javaCardsReadSources(t *testing.T, repo string) map[string]string {
	t.Helper()
	rels := []string{
		"src/cards/Cards.java",
		"src/cards/Driver.java",
		"test/CardsTest.java",
	}
	out := make(map[string]string, len(rels))
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(repo, "tables", "cards", "java", rel))
		if err != nil {
			t.Fatal(err)
		}
		out[rel] = string(b)
	}
	return out
}

func javaCardsStrictSourceCheck(sources map[string]string) []string {
	var bad []string
	for name, src := range sources {
		for _, retired := range javaCardsRetiredNames {
			if strings.Contains(src, retired) {
				bad = append(bad, fmt.Sprintf("%s: uses retired name %q", name, retired))
			}
		}
	}
	if facade, ok := sources["src/cards/Cards.java"]; ok {
		for _, req := range javaCardsRequiredFacadeFragments {
			if !strings.Contains(facade, req) {
				bad = append(bad, fmt.Sprintf("src/cards/Cards.java: missing strict fragment %q", req))
			}
		}
		if strings.Count(facade, "if (report.unknown > 0 || report.retainLost > 0)") < 2 {
			bad = append(bad, "src/cards/Cards.java: expected unknown/retainLost refusal guard in both taskHash and edit")
		}
	} else {
		bad = append(bad, "src/cards/Cards.java: missing")
	}
	if drv, ok := sources["src/cards/Driver.java"]; ok {
		for _, want := range []string{
			`b.append("KindMismatch=0\n");`,
			`b.append("Clamped=0\n");`,
			`b.append("Duplicate=0\n");`,
			`b.append("Malformed=").append(r.damaged).append('\n');`,
		} {
			if !strings.Contains(drv, want) {
				bad = append(bad, fmt.Sprintf("src/cards/Driver.java: missing %q", want))
			}
		}
	} else {
		bad = append(bad, "src/cards/Driver.java: missing")
	}
	if tst, ok := sources["test/CardsTest.java"]; ok {
		for _, want := range []string{
			`+ "KindMismatch=0\n"`,
			`+ "Clamped=0\n"`,
			`+ "Duplicate=0\n"`,
			`+ "Malformed=" + r.damaged + "\n";`,
		} {
			if !strings.Contains(tst, want) {
				bad = append(bad, fmt.Sprintf("test/CardsTest.java: missing %q", want))
			}
		}
	} else {
		bad = append(bad, "test/CardsTest.java: missing")
	}
	return bad
}

func javaCardsGeneratedModules(t *testing.T, repo string) map[string][]byte {
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

func javaCardsBuildAndRun(t *testing.T, javac, java, repo string, generated map[string][]byte, sources map[string]string) error {
	t.Helper()
	stage := t.TempDir()
	srcDir := filepath.Join(stage, "src")
	classesDir := filepath.Join(stage, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}

	var javaFiles []string
	var genNames []string
	for name := range generated {
		if strings.HasSuffix(name, ".java") {
			genNames = append(genNames, name)
		}
	}
	sort.Strings(genNames)
	for _, name := range genNames {
		p := filepath.Join(srcDir, filepath.Base(name))
		if err := os.WriteFile(p, generated[name], 0o600); err != nil {
			return err
		}
		javaFiles = append(javaFiles, p)
	}

	for rel, content := range sources {
		p := filepath.Join(srcDir, filepath.Base(rel))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return err
		}
		javaFiles = append(javaFiles, p)
	}

	runnerSrc := `public final class StrictCardsRunner {
    public static void main(String[] args) throws Exception {
        CardsTest.main(new String[0]);
        String manifest = args[0];
        for (int i = 1; i + 1 < args.length; i += 2) {
            cards.Driver.main(new String[]{manifest, args[i], args[i + 1]});
        }
    }
}
`
	runnerPath := filepath.Join(srcDir, "StrictCardsRunner.java")
	if err := os.WriteFile(runnerPath, []byte(runnerSrc), 0o600); err != nil {
		return err
	}
	javaFiles = append(javaFiles, runnerPath)

	javacArgs := append([]string{"--release", "17", "-nowarn", "-d", classesDir}, javaFiles...)
	buildCmd := exec.Command(javac, javacArgs...)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("javac failed: %v\n%s", err, out)
	}

	manifestPath := filepath.Join(repo, "tables", "cards", "conformance", "MANIFEST.txt")
	expectedDir := filepath.Join(repo, "tables", "cards", "conformance", "expected")
	rowsBySurface, err := javaCardsManifestRows(manifestPath)
	if err != nil {
		return err
	}

	outDirs := make(map[string]string, len(javaCardsSurfaces))
	runArgs := []string{"-XX:+UseSerialGC", "-XX:ActiveProcessorCount=1", "-cp", classesDir, "StrictCardsRunner", manifestPath}
	for _, surface := range javaCardsSurfaces {
		od := t.TempDir()
		outDirs[surface] = od
		runArgs = append(runArgs, surface, od)
	}

	runCmd := exec.Command(java, runArgs...)
	runCmd.Dir = repo
	if out, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("StrictCardsRunner failed: %v\n%s", err, out)
	}

	for _, surface := range javaCardsSurfaces {
		if err := javaCardsCheckSurface(surface, rowsBySurface[surface], outDirs[surface], expectedDir); err != nil {
			return err
		}
	}
	return nil
}

func javaCardsManifestRows(manifestPath string) (map[string][][]string, error) {
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

func javaCardsCheckSurface(surface string, rows [][]string, outDir, expectedDir string) error {
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

func TestJavaCardsStrict(t *testing.T) {
	t.Parallel()
	repo := javaCardsRepoRoot(t)
	sources := javaCardsReadSources(t, repo)
	if bad := javaCardsStrictSourceCheck(sources); len(bad) != 0 {
		t.Fatalf("tables/cards/java sources are not on the strict TableVariable.Report names:\n%s", strings.Join(bad, "\n"))
	}
	t.Run("FunctionalJava", func(t *testing.T) {
		slowtest.Gate(t, "functional test: javac/java builds and runs the Java cards facade, CardsTest, and 9 conformance surfaces")
		javac, java, ok := findJavaToolsForCards(t)
		if !ok {
			return
		}
		generated := javaCardsGeneratedModules(t, repo)
		if err := javaCardsBuildAndRun(t, javac, java, repo, generated, sources); err != nil {
			t.Fatal(err)
		}
	})
}

func TestJavaCardsStrictNegativeControl(t *testing.T) {
	t.Parallel()
	repo := javaCardsRepoRoot(t)
	baseSources := javaCardsReadSources(t, repo)
	if bad := javaCardsStrictSourceCheck(baseSources); len(bad) != 0 {
		t.Fatalf("NEGATIVE CONTROL setup: unmutated sources already fail check: %v", bad)
	}

	sabotages := []struct {
		name string
		file string
		from string
		to   string
	}{
		{
			name: "retired-kind-mismatch-in-cards",
			file: "src/cards/Cards.java",
			from: "if (report.unknown > 0) {",
			to:   "if (report.unknown > 0 || report.kindMismatch > 0) {",
		},
		{
			name: "retired-clamped-in-driver",
			file: "src/cards/Driver.java",
			from: `b.append("Clamped=0\n");`,
			to:   `b.append("Clamped=").append(r.clamped).append('\n');`,
		},
		{
			name: "retired-malformed-in-test",
			file: "test/CardsTest.java",
			from: `+ "Malformed=" + r.damaged + "\n";`,
			to:   `+ "Malformed=" + r.malformed + "\n";`,
		},
		{
			name: "drop-unknown-refusal-in-edit",
			file: "src/cards/Cards.java",
			from: "if (report.unknown > 0 || report.retainLost > 0) {",
			to:   "if (report.retainLost > 0) {",
		},
		{
			name: "wrong-malformed-in-driver-report",
			file: "src/cards/Driver.java",
			from: `b.append("Malformed=").append(r.damaged).append('\n');`,
			to:   `b.append("Malformed=true\n");`,
		},
	}

	for _, tc := range sabotages {
		t.Run(tc.name, func(t *testing.T) {
			mutated := maps.Clone(baseSources)
			orig, ok := mutated[tc.file]
			if !ok {
				t.Fatalf("missing %s", tc.file)
			}
			if !strings.Contains(orig, tc.from) {
				t.Fatalf("%s: sabotage anchor %q not found", tc.file, tc.from)
			}
			mutated[tc.file] = strings.Replace(orig, tc.from, tc.to, 1)

			bad := javaCardsStrictSourceCheck(mutated)
			if len(bad) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): source check stayed green on sabotaged %s", tc.name, tc.file)
			}
		})
	}

	t.Run("FunctionalJava", func(t *testing.T) {
		slowtest.Gate(t, "functional test: javac/java negative controls for Java cards strict names and conformance surfaces")
		javac, java, ok := findJavaToolsForCards(t)
		if !ok {
			return
		}
		generated := javaCardsGeneratedModules(t, repo)
		for _, tc := range sabotages {
			mutated := maps.Clone(baseSources)
			mutated[tc.file] = strings.Replace(mutated[tc.file], tc.from, tc.to, 1)
			if err := javaCardsBuildAndRun(t, javac, java, repo, generated, mutated); err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): functional build/run stayed green on sabotaged %s", tc.name, tc.file)
			}
		}
	})
}
