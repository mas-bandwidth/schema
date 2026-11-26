package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// The C leg's units are listed once, in test/conformance/c/driver.h's
// CONFORMANCE_C_UNITS, and both C drivers look a root up in that list: the
// conformance driver (test/conformance/c/main.c) and the wire-fuzz driver
// (test/c-tables/wire_fuzz_main.c). Two hand-kept lists drifted: the wire
// fuzz lacked six units, and the harness counted their roots as seeds absent
// (make tables-c-wire-fuzz: "wire-fuzz roster incomplete").

var (
	cCodecDefinition = regexp.MustCompile(`conformance_codecs_([a-z0-9_]+)\s*\(\s*int\s*\*\s*\w+\s*\)\s*\{`)
	cComment         = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)
	cUnitsMacro      = regexp.MustCompile(`(?s)#define CONFORMANCE_C_UNITS \\\n(.*?)\n\n`)
	cCodecName       = regexp.MustCompile(`conformance_codecs_([a-z0-9_]+)`)
)

// cUnitListProblems names every way the C sources fail the one-list rule:
// a unit defined in the leg and missing from CONFORMANCE_C_UNITS, a name
// listed twice or with no definition, and a driver whose unit array is not
// the list. files maps a repo-relative path to its text.
func cUnitListProblems(files map[string]string) []string {
	var problems []string
	defined := map[string]string{}
	for path, text := range files {
		for _, m := range cCodecDefinition.FindAllStringSubmatch(text, -1) {
			defined[m[1]] = path
		}
	}
	m := cUnitsMacro.FindStringSubmatch(files["test/conformance/c/driver.h"])
	if m == nil {
		return []string{"test/conformance/c/driver.h: no #define CONFORMANCE_C_UNITS"}
	}
	// a name commented out of the list is not listed
	listed := map[string]int{}
	for _, n := range cCodecName.FindAllStringSubmatch(cComment.ReplaceAllString(m[1], ""), -1) {
		listed[n[1]]++
	}
	for unit, path := range defined {
		if listed[unit] == 0 {
			problems = append(problems, path+": conformance_codecs_"+unit+" is defined and CONFORMANCE_C_UNITS (driver.h) does not list it")
		}
	}
	for unit, n := range listed {
		if n > 1 {
			problems = append(problems, "CONFORMANCE_C_UNITS lists conformance_codecs_"+unit+" more than once")
		}
		if _, ok := defined[unit]; !ok {
			problems = append(problems, "CONFORMANCE_C_UNITS lists conformance_codecs_"+unit+", which no C source defines")
		}
	}
	for path, want := range map[string]string{
		"test/conformance/c/main.c":      "static const UnitFn units[] = { CONFORMANCE_C_UNITS };",
		"test/c-tables/wire_fuzz_main.c": "#else\n    const Unit units[] = { CONFORMANCE_C_UNITS };\n#endif",
	} {
		if strings.Count(files[path], want) != 1 {
			problems = append(problems, path+": its unit array is not CONFORMANCE_C_UNITS (want "+quoteLine(want)+")")
		}
	}
	slices.Sort(problems)
	return problems
}

func quoteLine(s string) string { return "`" + strings.ReplaceAll(s, "\n", `\n`) + "`" }

func readCUnitSources(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, glob := range []string{"test/conformance/c/*.c", "test/conformance/c/*.h", "test/conformance/c/rows/*.c", "test/c-tables/*.c", "test/c-tables/*.cpp"} {
		paths, err := provesran.Glob(filepath.Join(root, glob))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.ToSlash(rel)] = string(b)
		}
	}
	return files
}

func TestTheCDriversShareOneUnitList(t *testing.T) {
	t.Parallel()
	files := readCUnitSources(t, repoRoot(t))
	for _, p := range cUnitListProblems(files) {
		t.Error(p)
	}
	m := cUnitsMacro.FindStringSubmatch(files["test/conformance/c/driver.h"])
	if m == nil {
		t.FailNow() // named above: driver.h has no CONFORMANCE_C_UNITS
	}
	units := len(cCodecName.FindAllString(m[1], -1))
	if units == 0 {
		t.Fatal("CONFORMANCE_C_UNITS lists no unit")
	}
	t.Logf("CONFORMANCE_C_UNITS: %d units, both drivers read it", units)
}

// The rule's plants, each on the real sources: a unit dropped from the list,
// a driver back on a hand-kept array, a name listed twice.
func TestTheCDriversShareOneUnitListRefusesEachDrift(t *testing.T) {
	t.Parallel()
	sources := readCUnitSources(t, repoRoot(t))
	if p := cUnitListProblems(sources); len(p) != 0 {
		t.Fatalf("the real sources already fail: %v", p)
	}
	for _, tc := range []struct{ name, path, from, to, want string }{
		{"a unit dropped from the list", "test/conformance/c/driver.h", ", conformance_codecs_tblmoodmap\n", "\n", "conformance_codecs_tblmoodmap is defined and CONFORMANCE_C_UNITS (driver.h) does not list it"},
		{"the wire fuzz on a hand-kept array", "test/c-tables/wire_fuzz_main.c", "{ CONFORMANCE_C_UNITS }", "{ conformance_codecs_tblw1 }", "test/c-tables/wire_fuzz_main.c: its unit array is not CONFORMANCE_C_UNITS"},
		{"the conformance driver on a hand-kept array", "test/conformance/c/main.c", "{ CONFORMANCE_C_UNITS }", "{ conformance_codecs_tblw1 }", "test/conformance/c/main.c: its unit array is not CONFORMANCE_C_UNITS"},
		{"a unit commented out of the list", "test/conformance/c/driver.h", "conformance_codecs_tblmoodmap\n", "/* conformance_codecs_tblmoodmap */\n", "conformance_codecs_tblmoodmap is defined and CONFORMANCE_C_UNITS (driver.h) does not list it"},
		{"a name listed twice", "test/conformance/c/driver.h", "conformance_codecs_tblmoodmap\n", "conformance_codecs_tblmoodmap, conformance_codecs_tblmoodmap\n", "lists conformance_codecs_tblmoodmap more than once"},
	} {
		planted := map[string]string{}
		for k, v := range sources {
			planted[k] = v
		}
		if strings.Count(planted[tc.path], tc.from) == 0 {
			t.Fatalf("%s: the plant does not apply to %s", tc.name, tc.path)
		}
		planted[tc.path] = strings.Replace(planted[tc.path], tc.from, tc.to, 1)
		problems := cUnitListProblems(planted)
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
			t.Errorf("%s: not refused; got %v", tc.name, problems)
		}
	}
}
