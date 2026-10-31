package luatable

// TestLuaStrictReadRefusesWhatGoRefuses pins the Lua strict read to the
// generated Go strict read, byte for byte over the conformance manifest's
// report and refusal rows and over one forged wire per strict-only name
// (docs/SPEC-TABLES.md §6.5, §11): unknown_form for a form byte, integrity_
// mismatch, wire_damaged where the framing is damaged, malformed, blob_shared,
// clamped, kind_mismatch and non_finite_float, while unknown, widened and duplicate
// ALONE read clean. internal/codegen/gotable/refusal.go (TableReport.Err) is
// the oracle.

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type strictCase struct {
	label string
	root  string
	wire  []byte
	want  string // "" means clean; used only for forged wires
}

type strictGroup struct {
	name  string
	unit  *ir.Unit
	cases []strictCase
}

func strictLeb(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

func strictField(id uint64, kind int, payload []byte) []byte {
	out := strictLeb(id)
	out = append(out, byte(kind))
	return append(out, payload...)
}

func strictTrailer(ids ...uint64) []byte {
	var out []byte
	for _, id := range ids {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], id)
		out = append(out, b[:]...)
	}
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(ids)))
	return append(out, n[:]...)
}

func strictWire(body []byte, ids ...uint64) []byte {
	out := []byte{1}
	out = append(out, body...)
	return append(out, strictTrailer(ids...)...)
}

// strictForgeFloat builds the nil/unknown/duplicate/NaN/frame wires from a
// unit whose Root is { f float64 }.
func strictForgeFloat(t *testing.T, u *ir.Unit) []strictCase {
	t.Helper()
	f := u.Tables["Root"].Fields[0]
	id := ir.TableFieldWireId(f)
	kind := ir.TableWireFieldKind(f)
	cleanBody := strictField(1, kind, func() []byte {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(1.5))
		return b[:]
	}())
	cleanBody = append(cleanBody, 0)
	nanBody := strictField(1, kind, func() []byte {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(math.NaN()))
		return b[:]
	}())
	nanBody = append(nanBody, 0)
	nanWire := strictWire(nanBody, id)
	// unknown: one more id in the trailer and one field naming it, which the
	// reader's descriptor does not declare.
	unknownBody := append([]byte(nil), cleanBody[:len(cleanBody)-1]...)
	unknownBody = append(unknownBody, strictField(2, 1, []byte{0})...)
	unknownBody = append(unknownBody, 0)
	unknownWire := strictWire(unknownBody, id, 0xdeadbeefcafebabe)
	// a frame past the root body: a byte after the terminator, before the
	// trailer, which the framing walk reads as damage.
	frameBody := append(append([]byte(nil), cleanBody...), 0)
	frameWire := strictWire(frameBody, id)
	return []strictCase{
		{label: "forged_unknown", root: "Root", wire: unknownWire, want: ""},
		{label: "forged_nan", root: "Root", wire: nanWire, want: "non_finite_float"},
		{label: "forged_frame_past_body", root: "Root", wire: frameWire, want: "wire_damaged"},
	}
}

// strictForgeDuplicate builds a map with one key twice: duplicate alone is
// data, not loss, so both strict reads answer clean.
func strictForgeDuplicate(t *testing.T, u *ir.Unit) []strictCase {
	t.Helper()
	st := u.Tables["Root"]
	e, a := st.Fields[0], st.Fields[1]
	k, v := ir.MapKeyField(e), ir.MapValueField(e)
	i32 := func(n int32) []byte {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(n))
		return b[:]
	}
	entry := strictField(2, ir.TableWireFieldKind(k), i32(7))
	entry = append(entry, strictField(3, ir.TableWireFieldKind(v), i32(1))...)
	entry = append(entry, 0)
	mapBody := []byte{13}
	mapBody = append(mapBody, strictLeb(2)...)
	mapBody = append(mapBody, strictLeb(uint64(len(entry)))...)
	mapBody = append(mapBody, entry...)
	mapBody = append(mapBody, strictLeb(uint64(len(entry)))...)
	mapBody = append(mapBody, entry...)
	body := strictField(1, ir.TableWireFieldKind(e), append(strictLeb(uint64(len(mapBody))), mapBody...))
	body = append(body, strictField(4, ir.TableWireFieldKind(a), i32(42))...)
	body = append(body, 0)
	return []strictCase{{
		label: "forged_duplicate", root: "Root",
		wire: strictWire(body, ir.TableFieldWireId(e), ir.TableFieldWireId(k), ir.TableFieldWireId(v), ir.TableFieldWireId(a)),
		want: "",
	}}
}

func strictWriteSchema(t *testing.T, dir, pkg, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(dir, pkg+".schema")
	if err := os.WriteFile(p, []byte("package "+pkg+"\n"+src), 0o644); err != nil {
		t.Fatal(err)
	}
	return luarun.LoadUnit(t, p)
}

func strictRefusalRows(t *testing.T, manifest string) []manifestRow {
	t.Helper()
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var rows []manifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "instance" {
			continue
		}
		for _, x := range f[5:] {
			if strings.HasPrefix(x, "go-refusal=") {
				rows = append(rows, manifestRow{caseName: f[1], unit: f[2], root: f[3], wire: f[4]})
			}
		}
	}
	return rows
}

// strictGoOracle generates the Go unit and returns case label -> strict name.
func strictGoOracle(t *testing.T, u *ir.Unit, cases []strictCase) map[string]string {
	t.Helper()
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate Go: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate Go tables: %v", err)
	}
	maps.Copy(goFiles, tables)
	dir := t.TempDir()
	goFiles["go.mod"] = []byte("module strictprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.1\n")
	var roots []string
	seen := map[string]bool{}
	var body strings.Builder
	for i, c := range cases {
		file := fmt.Sprintf("w_%d.bin", i)
		if err := os.WriteFile(filepath.Join(dir, file), c.wire, 0o600); err != nil {
			t.Fatal(err)
		}
		if !seen[c.root] {
			seen[c.root] = true
			roots = append(roots, c.root)
		}
		fmt.Fprintf(&body, "\t\t{%q, %q, %q},\n", c.label, c.root, file)
	}
	sort.Strings(roots)
	var sw strings.Builder
	for _, r := range roots {
		fmt.Fprintf(&sw, "\tcase %q:\n\t\t_, serr = %sReadStrict(data)\n", r, ir.GoExportName(r))
	}
	probe := "package " + u.Package + "\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
		"type strictProbeCase struct { name, root, file string }\n\n" +
		"var strictProbeCases = []strictProbeCase{\n" + body.String() + "}\n\n" +
		"func strictName(err error) string { if err == nil { return \"clean\" }; s := err.Error(); if i := strings.IndexByte(s, ':'); i >= 0 { return s[:i] }; return s }\n\n" +
		"func TestStrictProbe(t *testing.T) {\n" +
		"\tfor _, c := range strictProbeCases {\n" +
		"\t\tdata, err := os.ReadFile(c.file); if err != nil { t.Fatal(err) }\n" +
		"\t\tvar serr error\n\t\tswitch c.root {\n" + sw.String() + "\t\tdefault: t.Fatalf(\"no root %s\", c.root)\n\t\t}\n" +
		"\t\tfmt.Printf(\"STRICT\\t%s\\t%s\\n\", c.name, strictName(serr))\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "strictprobe_test.go"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestStrictProbe$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go strict read: %v\n%s", err, out)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "STRICT\t") {
			f := strings.Split(line, "\t")
			if len(f) == 3 {
				got[f[1]] = f[2]
			}
		}
	}
	if len(got) != len(cases) {
		t.Fatalf("the Go probe answered %d of %d cases:\n%s", len(got), len(cases), out)
	}
	return got
}

func strictLuaAnswers(t *testing.T, u *ir.Unit, cases []strictCase) map[string]string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate Lua: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	inputs := map[string][]byte{}
	var rootMap strings.Builder
	rootMap.WriteString("local roots = {\n")
	for _, c := range cases {
		inputs[c.label] = c.wire
		fmt.Fprintf(&rootMap, "  [%q] = %q,\n", c.label, c.root)
	}
	rootMap.WriteString("}\n")
	probe := "local T = require(" + fmt.Sprintf("%q", ModuleName(u)) + ")\n" +
		"local R = require(\"refusal\")\nlocal inputs = require(\"inputs\")\n" + rootMap.String() +
		"local names = {}\nfor k in pairs(inputs) do names[#names+1] = k end\ntable.sort(names)\n" +
		"for _, k in ipairs(names) do\n" +
		"  local ok, err = pcall(function() return R.ReadStrict(T, roots[k], inputs[k]) end)\n" +
		"  local n = \"clean\"\n" +
		"  if not ok then n = tostring(err); local i = n:find(\":\", 1, true); if i then n = n:sub(1, i-1) end end\n" +
		"  print(\"STRICT\\t\" .. k .. \"\\t\" .. n)\nend\n"
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "STRICT\t") {
			f := strings.Split(line, "\t")
			if len(f) == 3 {
				got[f[1]] = f[2]
			}
		}
	}
	if len(got) != len(cases) {
		t.Fatalf("the Lua probe answered %d of %d cases:\n%s", len(got), len(cases), out)
	}
	return got
}

func TestLuaStrictReadRefusesWhatGoRefuses(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned lua")
	repo := "../../.."
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	groups := map[string]*strictGroup{}
	load := func(key string, paths []string) *strictGroup {
		g := groups[key]
		if g != nil {
			return g
		}
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		g = &strictGroup{name: key, unit: luarun.LoadUnit(t, full...)}
		groups[key] = g
		return g
	}
	for _, row := range manifestRows(t, manifest, "report") {
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: %v", row.caseName, err)
		}
		load(row.unit, units[row.unit]).cases = append(
			load(row.unit, units[row.unit]).cases,
			strictCase{label: row.caseName, root: row.root, wire: wire})
	}
	for _, row := range strictRefusalRows(t, manifest) {
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("%s: %v", row.caseName, err)
		}
		g := load(row.unit, units[row.unit])
		g.cases = append(g.cases, strictCase{label: row.caseName, root: row.root, wire: wire})
	}

	// The forged wires live in units of their own, so the reader declares no
	// field the wire names and no non-finite is in the manifest.
	floatDir := t.TempDir()
	floatUnit := strictWriteSchema(t, floatDir, "strictfloat", "table Root { f float64 }\n")
	dupDir := t.TempDir()
	dupUnit := strictWriteSchema(t, dupDir, "strictdup", "table Root { entries map[int32]int32\n past int32 }\n")
	groups["strictfloat"] = &strictGroup{name: "strictfloat", unit: floatUnit, cases: strictForgeFloat(t, floatUnit)}
	groups["strictdup"] = &strictGroup{name: "strictdup", unit: dupUnit, cases: strictForgeDuplicate(t, dupUnit)}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		g := groups[key]
		if _, err := Generate(g.unit); err != nil {
			// The Lua table backend does not carry the fixed-point and 128-bit
			// kinds yet (docs/SPEC-TABLES.md §3, §15); a unit it refuses to emit
			// has no Lua strict read to pin.
			t.Logf("unit %s is not carried by the Lua table backend: %v", g.name, err)
			continue
		}
		goAns := strictGoOracle(t, g.unit, g.cases)
		luaAns := strictLuaAnswers(t, g.unit, g.cases)
		for _, c := range g.cases {
			if c.want != "" && goAns[c.label] != c.want {
				t.Errorf("%s/%s root %s: Go says %q, the forged wire pins %q\n    wire %s",
					g.name, c.label, c.root, goAns[c.label], c.want, hex.EncodeToString(c.wire))
			}
			if luaAns[c.label] != goAns[c.label] {
				t.Errorf("%s/%s root %s: Lua says %q, Go says %q\n    wire %s",
					g.name, c.label, c.root, luaAns[c.label], goAns[c.label], hex.EncodeToString(c.wire))
			}
		}
	}
}
