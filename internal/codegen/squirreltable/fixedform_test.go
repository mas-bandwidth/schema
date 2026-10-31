package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func fixedCorpus(t *testing.T) string {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
			t.Fatalf("the fixed-form corpus is the byte oracle and it is absent: %v (run make tables-fixedform-corpus)", err)
		}
		t.Skipf("the fixed-form corpus is not built (make tables-fixedform-corpus): %v", err)
	}
	return corpus
}

func fixedRecords(t *testing.T, corpus string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "file=") {
			continue
		}
		var file string
		n := -1
		for f := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			if k == "file" {
				file = v
			}
			if k == "records" {
				n, _ = strconv.Atoi(v)
			}
		}
		if file != "" && n >= 0 {
			out[file] = n
		}
	}
	return out
}

type fixedVector struct {
	key, schema, module, root, file string
}

var fixedVectors = []fixedVector{
	{"fx1", "test/tables/FX1.schema", "Tblfx1Tables", "FxRoot", "fx1.bin"},
	{"fx2", "test/tables/FX2.schema", "Tblfx2Tables", "FxRoot", "fx2.bin"},
	{"p1", "test/tables/P1.schema", "Tblp1Tables", "Chain", "p1.bin"},
	{"p3", "test/tables/P3.schema", "Tblp3Tables", "Chain", "p3.bin"},
	// the corpus's one WIDE file: a wstring(N) at the body's head and at a
	// nested offset, beside a narrow string (SPEC §4.12, docs/SPEC-TABLES.md §3.4)
	{"fxw", "test/tables/FXW.schema", "TblfxwTables", "FxWide", "fxw.bin"},
}

func fixedLoadUnit(t *testing.T, root, rel string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, rel)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func writeGenerated(t *testing.T, dir string, u *ir.Unit) {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSquirrelFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedRecords(t, corpus)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	p.WriteString("::hexlineN <- function(b, n) { local s = \"\"; for (local i = 0; i < n; i++) s += format(\"%02x\", b[i] & 0xff); print(s + \"\\n\"); };\n")
	for _, v := range fixedVectors {
		n, ok := records[v.file]
		if !ok {
			t.Fatalf("the manifest carries no line for %s", v.file)
		}
		data, err := os.ReadFile(filepath.Join(corpus, v.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[v.key] = data
		u := fixedLoadUnit(t, root, v.schema)
		sub := filepath.Join(dir, v.key)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		writeGenerated(t, sub, u)
		if _, err := os.Stat(filepath.Join(sub, "schema_module.nut")); err != nil {
			names, _ := os.ReadDir(sub)
			var have []string
			for _, e := range names {
				have = append(have, e.Name())
			}
			t.Fatalf("%s: no loader in %s; have %v", v.key, sub, have)
		}
		fmt.Fprintf(&p, "{ local S = dofile(%q, true); local T = S.require(%q);\n", v.key+"/schema_module.nut", v.module)
		fmt.Fprintf(&p, "  local b = inputs[%q]; local back = []; for (local k = 0; k < %d; k++) back.append(T.%s());\n", v.key, n, v.root)
		fmt.Fprintf(&p, "  local got = T.%sFixedLoadTolerant(b, back, null);\n", v.root)
		fmt.Fprintf(&p, "  if (got != %d) { print(\"LOAD:%s:\" + got + \":\" + T.FixedReason + \"\\n\"); }\n", n, v.key)
		fmt.Fprintf(&p, "  else { local out = blob(b.len()); T.%sFixedSave(out, back); ::hexlineN(out, b.len()); }\n", v.root)
		p.WriteString("}\n")
	}

	// The versioning invariant: one appended field, both directions.
	oldU := fixedSynthetic(t, "package vroot\nfixed table VR { a int32 = 5 }\n")
	newU := fixedSynthetic(t, "package vroot\nfixed table VR { a int32 = 5\n b int32 = 11 }\n")
	for _, side := range []struct {
		dir, module string
		u           *ir.Unit
	}{{"vold", "VrootTables", oldU}, {"vnew", "VrootTables", newU}} {
		sub := filepath.Join(dir, side.dir)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		writeGenerated(t, sub, side.u)
	}
	p.WriteString(`{ local O = dofile("vold/schema_module.nut", true).require("VrootTables");
  local N = dofile("vnew/schema_module.nut", true).require("VrootTables");
  local o = O.VR(); o.A = 42; local ob = blob(256); local om = O.VRFixedSave(ob, [o]);
  local ob2 = blob(om); for (local i = 0; i < om; i++) ob2[i] = ob[i];
  local n = N.VR(); local got = N.VRFixedLoadTolerant(ob2, [n], null);
  if (got != 1 || n.A != 42 || n.B != 11) print("VERSION-OLD-NEW:" + got + ":" + N.FixedReason + ":len=" + ob.len() + ":L=" + ((ob[16]|ob[17]<<8|ob[18]<<16|ob[19]<<24)) + "\n");
  local nv = N.VR(); nv.A = 43; nv.B = 44; local nb = blob(256); local nm = N.VRFixedSave(nb, [nv]);
  local nb2 = blob(nm); for (local i = 0; i < nm; i++) nb2[i] = nb[i];
  local ov = O.VR(); local got2 = O.VRFixedLoadTolerant(nb2, [ov], null);
  if (got2 != 1 || ov.A != 43) print("VERSION-NEW-OLD:" + got2 + ":" + O.FixedReason + ":len=" + nb.len() + ":L=" + ((nb[16]|nb[17]<<8|nb[18]<<16|nb[19]<<24)) + "\n");
}
`)
	// Named refusals, each reached by one forged layout.
	ref := inputs["fx1"]
	prev := append([]byte(nil), ref...)
	prev[0] = 1
	inputs["prev"] = prev
	mal := append([]byte(nil), ref...)
	binary.LittleEndian.PutUint32(mal[16:], 0xFFFF)
	inputs["mal"] = mal
	tooBig := append([]byte(nil), ref...)
	binary.LittleEndian.PutUint32(tooBig[33:], 70000)
	inputs["toobig"] = tooBig
	p.WriteString(`{ local S = dofile("fx1/schema_module.nut", true); local T = S.require("Tblfx1Tables");
  local v = [T.FxRoot()];
  T.FxRootFixedLoadTolerant(inputs["prev"], v, null); print("R1:" + T.FixedReason + "\n");
  T.FxRootFixedLoadTolerant(inputs["mal"], v, null); print("R2:" + T.FixedReason + "\n");
  T.FxRootFixedLoadTolerant(inputs["toobig"], v, null); print("R3:" + T.FixedReason + "\n");
  T.FxRootFixedLoadTolerant(inputs["fx1"], v, 0); print("R4:" + T.FixedReason + "\n");
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	hexes := map[string][]byte{}
	refusals := map[string]string{}
	for _, line := range lines {
		if strings.HasPrefix(line, "LOAD:") {
			t.Errorf("the oracle file did not read clean: %s", line)
			continue
		}
		if strings.HasPrefix(line, "VERSION-") {
			t.Errorf("the appended-field invariant did not hold: %s", line)
			continue
		}
		if strings.HasPrefix(line, "R") && strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			refusals[parts[0]] = parts[1]
			continue
		}
		b, err := decodeHex(line)
		if err != nil {
			t.Fatalf("probe line %q: %v", line, err)
		}
		hexes[""] = b
	}
	// Lines are in vector order, so pair them back up.
	got := map[string][]byte{}
	idx := 0
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "LOAD:") || strings.HasPrefix(line, "VERSION-") || strings.HasPrefix(line, "R") {
			continue
		}
		b, err := decodeHex(line)
		if err != nil {
			t.Fatalf("probe line %q: %v", line, err)
		}
		if idx < len(fixedVectors) {
			got[fixedVectors[idx].key] = b
		}
		idx++
	}
	fixed := 0
	for _, v := range fixedVectors {
		want := inputs[v.key]
		if !strings.EqualFold(fmt.Sprintf("%x", got[v.key]), fmt.Sprintf("%x", want)) {
			diff := -1
			for i := 0; i < len(got[v.key]) && i < len(want); i++ {
				if got[v.key][i] != want[i] {
					diff = i
					break
				}
			}
			t.Errorf("%s: write-read-write differs (%d bytes against the oracle's %d), first difference at %d: got %x want %x", v.key, len(got[v.key]), len(want), diff, got[v.key][diff], want[diff])
		}
		fixed++
	}
	wantRefusals := map[string]string{
		"R1": "previous_form", "R2": "layout_malformed",
		"R3": "layout_record_too_large", "R4": "plan_too_large",
	}
	for k, want := range wantRefusals {
		if refusals[k] != want {
			t.Errorf("%s: refusal %q, want %q", k, refusals[k], want)
		}
	}
	if fixed != len(fixedVectors) {
		t.Fatalf("compared %d fixed roots, want %d", fixed, len(fixedVectors))
	}
	t.Logf("compared %d fixed roots against the byte oracle", fixed)
}

func fixedSynthetic(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "V.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func decodeHex(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := range out {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

// TestSquirrelFixedFormNegativeControl patches the emitted runtime in memory
// (one anchor, matched exactly once) so the plan skips the last field, and
// requires the oracle comparison to go red.
func TestSquirrelFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	fixedCorpus(t)
	u := fixedSynthetic(t, "package neg\nfixed table NR { a int32 = 5\n b int32 = 9 }\n")
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for _, side := range []string{"plain", "patched"} {
		if err := os.MkdirAll(filepath.Join(dir, side), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	anchor := "_rt_fixed.put(b, at + 0, 4, v.A);"
	matched := 0
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, "plain", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		body := string(data)
		patched := body
		if strings.Contains(patched, anchor) {
			matched += strings.Count(patched, anchor)
			patched = strings.Replace(patched, anchor, "/* negative control: the last field is skipped */", 1)
		}
		if err := os.WriteFile(filepath.Join(dir, "patched", name), []byte(patched), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if matched != 1 {
		t.Fatalf("the negative-control anchor matched %d times, want exactly 1", matched)
	}
	main := `local P = dofile("plain/schema_module.nut", true).require("NegTables");
local Q = dofile("patched/schema_module.nut", true).require("NegTables");
local r = P.NR(); r.A = 42; r.B = 7; local a = blob(128); local n = P.NRFixedSave(a, [r]);
local q = Q.NR(); q.A = 42; q.B = 7; local b = blob(128); local m = Q.NRFixedSave(b, [q]);
local same = n == m; for (local i = 0; i < n && same; i++) if (a[i] != b[i]) same = false;
print(same ? "NEGATIVE-CONTROL-GREEN\n" : "negative-control-red\n");
`
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", nil))
	if !strings.Contains(out, "negative-control-red") {
		t.Fatalf("the patched writer still agreed with the unpatched writer: %s", out)
	}
}

// fixedNonFiniteValue is one manifest value the reference wrote as a NaN or
// an infinity: its path, then the float's spelled bits ("nan|0x7F800001").
var fixedNonFiniteValue = regexp.MustCompile(`^([A-Za-z0-9_.\[\]]+)=[^|]*\|0x([0-9A-Fa-f]{16}|[0-9A-Fa-f]{8})$`)

// fixedNonFinite reads, per file of the corpus manifest, the path of the first
// value the reference wrote as a NaN or an infinity, and "" for a file that
// holds none, as the Go leg's fixedOracleManifestNonFinite does: such a file
// is a MUST-REFUSE vector under the finite rule (docs/SPEC-TABLES.md §3.3).
func fixedNonFinite(t *testing.T, corpus string) map[string]string {
	t.Helper()
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			continue
		}
		file := ""
		for field := range strings.FieldsSeq(head) {
			if v, ok := strings.CutPrefix(field, "file="); ok {
				file = v
			}
		}
		for value := range strings.SplitSeq(values, ",") {
			m := fixedNonFiniteValue.FindStringSubmatch(value)
			if m == nil {
				continue
			}
			bits, err := strconv.ParseUint(m[2], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			if (len(m[2]) == 8 && bits&0x7F800000 == 0x7F800000 || len(m[2]) == 16 && bits&0x7FF0000000000000 == 0x7FF0000000000000) && file != "" && out[file] == "" {
				out[file] = m[1]
			}
		}
	}
	return out
}
