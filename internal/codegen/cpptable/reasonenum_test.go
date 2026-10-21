package cpptable

// THE ONE REFUSAL ENUM MUST HOLD ITS OWN HIGH-RANGE CONSTANTS
// (docs/SPEC-TABLES.md §3.3, §3.4, §6.5, §11).
//
// The three message-vocabulary names Go spells only in its report's reason
// text sit in a private high range of TableRefuseReason, so a name Go appends
// cannot collide with them. In an unscoped enum with no fixed underlying type
// C++ gives the enum a range of values and clang 21 refuses a constexpr cast
// that lands outside it. The emitter declares the underlying type; these tests
// pin that the declaration is wide enough for the values and that the emitted
// headers compile under both compilers with -Werror.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppReasonFixedUnit is a fixed table, so the generated unit carries the fixed
// form's runtime and its TableFixedReason names.
func cppReasonFixedUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	const src = `package probe

fixed table Point
{
    x int32 = 1
    y int32 = 2
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

// cppReasonFiles generates a unit's C++ files and answers them with the name
// of its `<Base>Table.h`.
func cppReasonFiles(t *testing.T, u *ir.Unit) (map[string][]byte, string) {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("generate lineage: %v", err)
	}
	maps.Copy(files, tables)
	var headers []string
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
	}
	if len(headers) == 0 {
		t.Fatal("the generated reader carries no <Base>Table.h")
	}
	sort.Strings(headers)
	header := headers[0]
	for _, name := range headers {
		if name == "TablesTable.h" {
			header = name
			break
		}
	}
	return files, header
}

var (
	cppReasonEnumRE    = regexp.MustCompile(`(?s)enum\s+TableRefuseReason\s*(?::\s*([A-Za-z_][A-Za-z0-9_:]*))?\s*\{(.*?)\}\s*;`)
	cppReasonFixedRE   = regexp.MustCompile(`constexpr\s+TableRefuseReason\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\(\s*TableRefuseReason\s*\)\s*(0[xX][0-9A-Fa-f]+|[0-9]+)`)
	cppReasonCommentRE = regexp.MustCompile(`//[^\n]*`)
)

// cppReasonEnumUnderlying answers the width and signedness a fixed underlying
// type promises. A mapping absent from this table is refused by the caller: the
// test cannot say the type is wide enough, so it does not pretend it is.
func cppReasonEnumUnderlying(name string) (bits uint, signed bool, ok bool) {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "std::")
	switch name {
	case "int8_t":
		return 8, true, true
	case "uint8_t":
		return 8, false, true
	case "int16_t":
		return 16, true, true
	case "uint16_t":
		return 16, false, true
	case "int32_t", "int":
		return 32, true, true
	case "uint32_t", "unsigned", "unsigned int":
		return 32, false, true
	case "int64_t", "long long":
		return 64, true, true
	case "uint64_t", "unsigned long long":
		return 64, false, true
	case "long":
		return 32, true, true
	case "unsigned long":
		return 32, false, true
	}
	return 0, false, false
}

func cppReasonEnumValues(body string) (map[string]uint64, bool) {
	values := map[string]uint64{}
	body = cppReasonCommentRE.ReplaceAllString(body, "")
	var next uint64
	for entry := range strings.SplitSeq(body, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name := entry
		if before, after, ok := strings.Cut(entry, "="); ok {
			name = strings.TrimSpace(before)
			raw := strings.TrimSpace(after)
			v, err := strconv.ParseUint(raw, 0, 64)
			if err != nil {
				return nil, false
			}
			next = v
		}
		if name == "" {
			return nil, false
		}
		values[name] = next
		next++
	}
	return values, true
}

// TestCPPRefuseReasonEnumHoldsItsHighRangeConstants pins the one vocabulary's
// shape (docs/SPEC-TABLES.md §6.5): TableRefuseReason declares a fixed
// underlying type wide enough for the private high range the three
// message-vocabulary constants sit in, TableFixedReason's three names are the
// enum's own values, and TableMessageReason is the message path's spelling of
// the same type rather than a second enum.
func TestCPPRefuseReasonEnumHoldsItsHighRangeConstants(t *testing.T) {
	t.Parallel()

	files, header := cppReasonFiles(t, cppReasonFixedUnit(t))
	text := string(files[header])

	m := cppReasonEnumRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s carries no enum TableRefuseReason", header)
	}
	underlying, body := m[1], m[2]
	enumerators, ok := cppReasonEnumValues(body)
	if !ok {
		t.Fatalf("%s: TableRefuseReason's enumerators are not the plain list this test reads", header)
	}
	if underlying == "" {
		t.Fatalf("%s: TableRefuseReason declares no underlying type; clang 21 refuses a constexpr cast into the high range", header)
	}
	bits, signed, ok := cppReasonEnumUnderlying(underlying)
	if !ok {
		t.Fatalf("TableRefuseReason's underlying type %q is one this test cannot measure", underlying)
	}
	var max uint64 = ^uint64(0)
	if bits < 64 {
		max = (uint64(1) << bits) - 1
	}
	if signed {
		max >>= 1
	}

	constants := map[string]uint64{}
	for _, c := range cppReasonFixedRE.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseUint(c[2], 0, 64)
		if err != nil {
			t.Fatalf("%s: %s value %q: %v", header, c[1], c[2], err)
		}
		constants[c[1]] = v
	}
	for _, name := range []string{"no_vocabulary", "second_announcement", "vocabulary_too_large"} {
		v, ok := constants[name]
		if !ok {
			t.Fatalf("%s carries no high-range constant %s", header, name)
		}
		if _, clash := enumerators[name]; clash {
			t.Fatalf("%s: %s is both an enumerator and a high-range constant", header, name)
		}
		if v > max {
			t.Fatalf("TableRefuseReason : %s (max 0x%x) cannot hold %s = 0x%x", underlying, max, name, v)
		}
		for en, ev := range enumerators {
			if ev == v {
				t.Fatalf("%s = 0x%x collides with enumerator %s", name, v, en)
			}
		}
	}
	for _, name := range []string{"layout_unknown", "truncated", "reserved_not_zero"} {
		if _, ok := enumerators[name]; !ok {
			t.Fatalf("TableRefuseReason carries no %s", name)
		}
		alias := "static constexpr TableRefuseReason " + name
		at := strings.Index(text, alias)
		if at < 0 || !strings.Contains(text[at:at+len(alias)+64], "= TableRefuseReason::"+name+";") {
			t.Fatalf("%s: TableFixedReason::%s is not the enum's own value", header, name)
		}
	}
	if !strings.Contains(text, "struct TableMessageReason") {
		t.Fatalf("%s carries no TableMessageReason over the one enum", header)
	}
	if regexp.MustCompile(`enum\s+TableMessageReason\b`).MatchString(text) {
		t.Fatalf("%s still declares TableMessageReason as a second enum", header)
	}
}

func cppReasonCompiler(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skipf("none of %v is on PATH", names)
	return ""
}

func cppReasonStage(t *testing.T, files map[string][]byte, header string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wrapper := "#include \"" + header + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tu.cpp"), []byte(wrapper), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func cppReasonCompile(cxx, dir string) ([]byte, error) {
	args := []string{
		"-std=c++17", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
		"-I" + dir, filepath.Join(dir, "tu.cpp"),
	}
	return exec.Command(cxx, args...).CombinedOutput()
}

func TestCPPGeneratedHeadersCompileUnderClangAndGxxWerror(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compilers (the generated headers are compiled, not read)")

	clang := cppReasonCompiler(t, "clang++-21", "clang++")
	gxx := cppReasonCompiler(t, "g++")
	compilers := []struct{ name, path string }{{"clang++", clang}, {"g++", gxx}}

	units := []struct {
		label string
		unit  *ir.Unit
	}{
		{"examples", loadHeldUnitPaths(t, "../../..", []string{"tables/examples"})},
		{"fixed", cppReasonFixedUnit(t)},
	}
	for _, tc := range units {
		files, header := cppReasonFiles(t, tc.unit)
		dir := cppReasonStage(t, files, header)
		for _, cxx := range compilers {
			if out, err := cppReasonCompile(cxx.path, dir); err != nil {
				t.Fatalf("%s refused the %s unit's %s under -std=c++17 -Wall -Wextra -Werror: %v\n%s", cxx.name, tc.label, header, err, out)
			}
		}
	}

	// THE REVERSED WITNESS. Drop the underlying type from a copy of the
	// examples header — enum TableRefuseReason with none — and clang must
	// refuse the high-range constexpr cast, naming the variable.
	files, header := cppReasonFiles(t, loadHeldUnitPaths(t, "../../..", []string{"tables/examples"}))
	base := string(files[header])
	unfixed := regexp.MustCompile(`enum\s+TableRefuseReason\s*:\s*[A-Za-z_][A-Za-z0-9_:]*`).ReplaceAllString(base, "enum TableRefuseReason")
	if unfixed == base {
		t.Fatalf("%s does not declare TableRefuseReason's underlying type, so the witness has nothing to reverse", header)
	}
	if !strings.Contains(unfixed, "(TableRefuseReason) 0x20000000") {
		t.Fatalf("%s no longer carries the 0x20000000 cast the witness needs", header)
	}
	witness := map[string][]byte{}
	maps.Copy(witness, files)
	witness[header] = []byte(unfixed)
	dir := cppReasonStage(t, witness, header)
	out, err := cppReasonCompile(clang, dir)
	if err == nil {
		t.Fatalf("the reversed witness compiled: clang accepted enum TableRefuseReason with no underlying type and the 0x20000000 cast")
	}
	line := ""
	for l := range strings.SplitSeq(string(out), "\n") {
		// clang 21 names the variable; clang 18 names the value and the enum
		if (strings.Contains(l, "error: constexpr variable 'no_vocabulary'") &&
			strings.Contains(l, "must be initialized by a constant expression")) ||
			(strings.Contains(l, "error: integer value 536870912 is outside the valid range") &&
				strings.Contains(l, "'TableRefuseReason'")) {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("clang refused the reversed witness without naming the constexpr:\n%s", out)
	}
	t.Logf("reversed witness refused at: %s", line)
}
