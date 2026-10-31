package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelFixedLoadNamesEveryRefusal pins docs/FIXED-FORM-ALGORITHM.md
// §5.3's naming rule for the Squirrel fixed reader: every refusal of the
// emitted <Root>FixedLoad has the same name the Go fixed reader answers, for
// every construct the Squirrel fixed form carries (docs/SPEC-TABLES.md §3.4).
// A hostile file per reason is built in Go over a clean fx1 file, and the
// generated Go table for the same schema is run as the oracle: Squirrel's
// FixedReason must equal Go's for every file.
func TestSquirrelFixedLoadNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	// fx1 carries every field kind the reasons touch: string(8), [..4]int32
	// and bytes(6).
	const schemaRel = "test/tables/FX1.schema"
	const module = "Tblfx1Tables"
	const recRoot = "FxRoot"
	const pkg = "tblfx1"

	u := fixedLoadUnit(t, root, schemaRel)
	sqDir := t.TempDir()
	writeGenerated(t, sqDir, u)

	clean := makeFixedCleanFile(t, sqDir, module, recRoot)

	hostileDir := t.TempDir()
	cases := buildHostileCases(t, clean)
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(hostileDir, c.name+".bin"), c.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sqReasons := squirrelFixedReasons(t, sqDir, module, recRoot, hostileDir, cases)
	goReasons := goFixedOracleReasons(t, root, schemaRel, pkg, recRoot, hostileDir)

	if len(sqReasons) != len(cases) {
		t.Fatalf("squirrel probe reported %d reasons, want %d", len(sqReasons), len(cases))
	}
	for _, c := range cases {
		want, ok := goReasons[c.name]
		if !ok {
			t.Fatalf("go oracle reported no reason for %s", c.name)
		}
		if sqReasons[c.name] != want {
			t.Errorf("%s: squirrel reason %q, Go oracle %q", c.name, sqReasons[c.name], want)
		}
	}
	t.Logf("checked %d refusals by name against the Go oracle", len(cases))
}

type hostileCase struct {
	name string
	data []byte
}

func makeFixedCleanFile(t *testing.T, sqDir, module, recRoot string) []byte {
	t.Helper()
	main := fmt.Sprintf(`local S = dofile("schema_module.nut", true);
local T = S.require(%q);
local r = T.%s();
local b = blob(512);
local n = T.%sFixedSave(b, [r]);
print("CLEAN-N=" + n + "\n");
for (local i = 0; i < n; i++) print(format("%%02x", b[i] & 0xff));
print("\n");
`, module, recRoot, recRoot)
	if err := os.WriteFile(filepath.Join(sqDir, "clean.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(sqrun.Run(t, sqDir, "clean.nut", nil)), "\n")
	var n int
	var hexline string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "CLEAN-N=") {
			fmt.Sscanf(line, "CLEAN-N=%d", &n)
		} else if line != "" {
			hexline = line
		}
	}
	b, err := decodeHex(hexline)
	if err != nil {
		t.Fatalf("clean hex: %v (%q)", err, hexline)
	}
	if len(b) != n {
		t.Fatalf("clean length %d, want %d", len(b), n)
	}
	return b
}

// buildHostileCases mutates the clean fx1 file into one hostile file per
// refusal. Offsets are relative to the record start, computed from the emitted
// layout: the root's entry (the first, at layout offset 4) carries the record
// body size, and the record is the body preceded by its eight-byte hash.
func buildHostileCases(t *testing.T, clean []byte) []hostileCase {
	t.Helper()
	cp := func() []byte { return append([]byte(nil), clean...) }
	L := binary.LittleEndian.Uint32(clean[16:20])
	recStart := int(20 + L)
	recBody := 8 + int(binary.LittleEndian.Uint32(clean[33:37]))
	if recBody != 72 {
		t.Fatalf("unexpected fx1 record body %d, want 72 (layout %d)", recBody, L)
	}
	setRecU32 := func(b []byte, off int, v uint32) {
		binary.LittleEndian.PutUint32(b[recStart+off:], v)
	}
	var cs []hostileCase
	cs = append(cs, hostileCase{"previous_form", cp()})
	cs[len(cs)-1].data[0] = 1
	cs = append(cs, hostileCase{"message_form_as_file", cp()})
	cs[len(cs)-1].data[0] = 2
	cs = append(cs, hostileCase{"newer_form", cp()})
	cs[len(cs)-1].data[0] = 4
	cs = append(cs, hostileCase{"truncated_short_header", cp()[:19]})
	cs = append(cs, hostileCase{"truncated_empty", cp()[:0]})
	cs = append(cs, hostileCase{"reserved_not_zero", cp()})
	cs[len(cs)-1].data[3] = 1
	cs = append(cs, hostileCase{"count_over_bound", cp()})
	setRecU32(cs[len(cs)-1].data, 42, 5)
	cs = append(cs, hostileCase{"length_over_bound_string", cp()})
	setRecU32(cs[len(cs)-1].data, 30, 9)
	cs = append(cs, hostileCase{"length_over_bound_bytes", cp()})
	setRecU32(cs[len(cs)-1].data, 62, 7)
	cs = append(cs, hostileCase{"text_ill_formed", cp()})
	setRecU32(cs[len(cs)-1].data, 30, 2)
	cs[len(cs)-1].data[recStart+34] = 0xC0
	cs[len(cs)-1].data[recStart+35] = 0xAF
	cs = append(cs, hostileCase{"no_layout", cp()})
	cs[len(cs)-1].data[recStart] ^= 1
	return cs
}

func squirrelFixedReasons(t *testing.T, sqDir, module, recRoot, hostileDir string, cases []hostileCase) map[string]string {
	t.Helper()
	var sb strings.Builder
	fmt.Fprintf(&sb, `local S = dofile("schema_module.nut", true);
local T = S.require(%q);
local inputs = dofile("inputs.nut", true);
local names = [
`, module)
	for i, c := range cases {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q", c.name+".bin")
	}
	sb.WriteString("];\nforeach (nm in names) {\n")
	fmt.Fprintf(&sb, "  local v = [T.%s()];\n", recRoot)
	fmt.Fprintf(&sb, "  T.%sFixedLoadTolerant(inputs[nm], v, null);\n", recRoot)
	sb.WriteString("  print(nm + \"=\" + T.FixedReason + \"\\n\");\n}\n")
	if err := os.WriteFile(filepath.Join(sqDir, "load.nut"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		d, err := os.ReadFile(filepath.Join(hostileDir, c.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name+".bin"] = d
	}
	out := string(sqrun.Run(t, sqDir, "load.nut", inputs))
	res := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q", line)
		}
		res[strings.TrimSuffix(k, ".bin")] = v
	}
	return res
}

// goFixedOracleReasons generates the Go table for the same schema, compiles it
// in a t.TempDir() and runs it over every hostile file, returning Go's reason
// per case name.
func goFixedOracleReasons(t *testing.T, root, schemaRel, pkg, recRoot, hostileDir string) map[string]string {
	t.Helper()
	u := fixedLoadUnit(t, root, schemaRel)
	files := map[string][]byte{}
	pkt, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, pkt)
	tbl, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tbl)
	probe := fmt.Sprintf(`package %s
import ("os"; "sort"; "strings"; "testing")
func TestOracleR(t *testing.T) {
	ents, _ := os.ReadDir(%q)
	var names []string
	for _, e := range ents { if !e.IsDir() { names = append(names, e.Name()) } }
	sort.Strings(names)
	for _, nm := range names {
		data, err := os.ReadFile(%q + "/" + nm)
		if err != nil { panic(err) }
		plan := make([]TableFixedEntry, 64)
		var rep TableReport
		dst := make([]%s, 1)
		n := %sFixedLoadTolerant(dst, data, plan, &rep)
		r := ""
		if n < 0 { r = rep.Reason }
		name := strings.TrimSuffix(nm, ".bin")
		println(name + "=" + r)
	}
}
`, pkg, hostileDir, hostileDir, recRoot, recRoot)
	files["oracle_test.go"] = []byte(probe)
	mod := "module probe\n\ngo 1.26\n"
	for _, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go"`) {
			runtime := slowtest.SerializeGo(t, "../../../")
			mod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
			break
		}
	}
	files["go.mod"] = []byte(mod)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go oracle build/run: %v\n%s", err, out)
	}
	res := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		res[k] = v
	}
	return res
}
