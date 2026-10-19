package cpptable

// THE MESSAGE-PATH ENUM MUST HOLD ITS OWN FIXED REASON CONSTANTS
// (docs/SPEC-TABLES.md §3.3, §3.4, §11).
//
// TableFixedReason's three values are a private high range so a name another
// card appends to TableMessageReason cannot collide with them. In an unscoped
// enum with no fixed underlying type C++ gives the enum a range of values and
// clang 21 refuses a constexpr cast that lands outside it — the generated
// headers stopped compiling under every sanitizer target. The emitter now
// declares the enum's underlying type; these tests pin that the declaration is
// wide enough for the values and that the emitted headers compile under both
// compilers with -Werror.

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
	cppReasonEnumRE    = regexp.MustCompile(`(?s)enum\s+TableMessageReason\s*(?::\s*([A-Za-z_][A-Za-z0-9_:]*))?\s*\{(.*?)\}\s*;`)
	cppReasonFixedRE   = regexp.MustCompile(`static\s+constexpr\s+TableMessageReason\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*\(\s*TableMessageReason\s*\)\s*(0[xX][0-9A-Fa-f]+|[0-9]+)`)
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
	for _, entry := range strings.Split(body, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name := entry
		if eq := strings.IndexByte(entry, '='); eq >= 0 {
			name = strings.TrimSpace(entry[:eq])
			raw := strings.TrimSpace(entry[eq+1:])
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

func TestCPPMessageReasonEnumHoldsTheFixedReasonConstants(t *testing.T) {
	t.Parallel()

	files, header := cppReasonFiles(t, cppReasonFixedUnit(t))
	text := string(files[header])

	m := cppReasonEnumRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s carries no enum TableMessageReason", header)
	}
	underlying, body := m[1], m[2]
	enumerators, ok := cppReasonEnumValues(body)
	if !ok {
		t.Fatalf("%s: TableMessageReason's enumerators are not the plain list this test reads", header)
	}

	constants := map[string]uint64{}
	for _, c := range cppReasonFixedRE.FindAllStringSubmatch(text, -1) {
		v, err := strconv.ParseUint(c[2], 0, 64)
		if err != nil {
			t.Fatalf("%s: TableFixedReason::%s value %q: %v", header, c[1], c[2], err)
		}
		constants[c[1]] = v
	}
	for _, name := range []string{"layout_unknown", "truncated", "reserved_not_zero"} {
		if _, ok := constants[name]; !ok {
			t.Fatalf("%s carries no TableFixedReason::%s constant", header, name)
		}
	}

	if underlying == "" {
		for name, value := range constants {
			if _, ok := enumerators[name]; ok {
				continue
			}
			inRange := false
			for _, ev := range enumerators {
				if ev == value {
					inRange = true
					break
				}
			}
			if !inRange {
				t.Fatalf("TableMessageReason has no fixed underlying type and TableFixedReason::%s = 0x%x is outside its enumerators; clang 21 refuses the constexpr cast", name, value)
			}
		}
		return
	}

	bits, signed, ok := cppReasonEnumUnderlying(underlying)
	if !ok {
		t.Fatalf("TableMessageReason's underlying type %q is one this test cannot measure", underlying)
	}
	var max uint64 = ^uint64(0)
	if bits < 64 {
		max = (uint64(1) << bits) - 1
	}
	if signed {
		max >>= 1
	}
	for name, value := range constants {
		if value > max {
			t.Fatalf("TableMessageReason : %s (max 0x%x) cannot hold TableFixedReason::%s = 0x%x", underlying, max, name, value)
		}
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

	// THE REVERSED WITNESS. Put the BASE declaration back in a copy of the
	// examples header — enum TableMessageReason with no underlying type — and
	// clang must refuse the 0x40000001 constexpr, naming the variable.
	files, header := cppReasonFiles(t, loadHeldUnitPaths(t, "../../..", []string{"tables/examples"}))
	base := string(files[header])
	unfixed := regexp.MustCompile(`enum\s+TableMessageReason\s*:\s*[A-Za-z_][A-Za-z0-9_:]*`).ReplaceAllString(base, "enum TableMessageReason")
	if unfixed == base {
		t.Fatalf("%s does not declare TableMessageReason's underlying type, so the witness has nothing to reverse", header)
	}
	if !strings.Contains(unfixed, "(TableMessageReason) 0x40000001") {
		t.Fatalf("%s no longer carries the 0x40000001 cast the witness needs", header)
	}
	witness := map[string][]byte{}
	maps.Copy(witness, files)
	witness[header] = []byte(unfixed)
	dir := cppReasonStage(t, witness, header)
	out, err := cppReasonCompile(clang, dir)
	if err == nil {
		t.Fatalf("the reversed witness compiled: clang accepted enum TableMessageReason with no underlying type and the 0x40000001 cast")
	}
	line := ""
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "error: constexpr variable 'layout_unknown'") &&
			strings.Contains(l, "must be initialized by a constant expression") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("clang refused the reversed witness without naming the constexpr:\n%s", out)
	}
	t.Logf("reversed witness refused at: %s", line)
}
