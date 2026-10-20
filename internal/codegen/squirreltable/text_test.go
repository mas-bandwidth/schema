package squirreltable_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// textPrelude is the text probe's one helper: load a wire file through the
// emitted load runtime, SaveText it, and LoadText the Go-computed base64,
// printing both texts for the Go side to compare (docs/SPEC-TABLES.md §16.8).
const textPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key, wantb64, d1, d2, d3) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local TX = schema.require("schema_table_text");
  local r1 = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
  local v = L.loadTolerant(T, root, inputs[key], r1);
  if (v == null || r1.refused) { print(key + " loadrefused\n"); return; }
  local saveText = TX.SaveText(T, root, v);
  if (saveText == null) { print(key + " savenull\n"); return; }
  local r2 = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
  local v2 = TX.LoadText(T, root, wantb64, r2);
  if (v2 == null || r2.refused) { print(key + " loadtextrefused\n"); return; }
  local saveText2 = TX.SaveText(T, root, v2);
  if (saveText2 == null) { print(key + " savenull2\n"); return; }
  print(key + " " + saveText + " " + saveText2 + "\n");
  local r3 = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
  TX.LoadText(T, root, d1, r3);
  print(key + " d1 " + (r3.refused ? "true" : "false") + "\n");
  r3 = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
  TX.LoadText(T, root, d2, r3);
  print(key + " d2 " + (r3.refused ? "true" : "false") + "\n");
  r3 = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
  TX.LoadText(T, root, d3, r3);
  print(key + " d3 " + (r3.refused ? "true" : "false") + "\n");
};
`

// TestSquirrelBase64TextRoundTripsEveryInstance checks the base64 text carrier
// for every instance row the target generates: SaveText of a loaded wire must
// equal Go's encoding/base64.StdEncoding of the same wire, LoadText of that
// text must save back to the same wire, and three damaged texts per run must
// refuse as base64_damaged (docs/SPEC-TABLES.md §16.8).
func TestSquirrelBase64TextRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	instances := readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(textPrelude)
	inputs := map[string][]byte{}
	mods := map[string]string{}
	refusedSet := map[string]bool{}
	var ordered []string
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.unit] {
			ordered = append(ordered, in.unit)
			seen[in.unit] = true
		}
	}
	for _, key := range ordered {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("instance rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			refusedSet[key] = true
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mods[key] = squirreltable.ModuleName(u)
		for _, in := range instances {
			if in.unit != key {
				continue
			}
			if in.goRefusal != "" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
			if err != nil {
				t.Fatal(err)
			}
			inputs[in.name] = data
			want := base64.StdEncoding.EncodeToString(data)
			d1 := damageByte(data)
			d2 := damagePad(want)
			d3 := damageCut(want)
			fmt.Fprintf(&p, "run(%q, %q, %q, %q, %q, %q, %q, %q);\n",
				key, mods[key], in.root, in.name, want, d1, d2, d3)
		}
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	got := map[string]string{}
	gotD := map[string]map[string]bool{}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) < 3 {
			t.Fatalf("probe line %q is not a case and texts", line)
		}
		switch f[1] {
		case "d1", "d2", "d3":
			if gotD[f[0]] == nil {
				gotD[f[0]] = map[string]bool{}
			}
			gotD[f[0]][f[1]] = f[2] == "true"
		case "loadrefused", "savenull", "loadtextrefused", "savenull2":
			t.Errorf("%s: the text probe reported %s", f[0], f[1])
		default:
			got[f[0]] = f[1] + " " + f[2]
		}
	}
	count := 0
	for _, in := range instances {
		if refusedSet[in.unit] {
			continue
		}
		if in.goRefusal != "" {
			continue
		}
		count++
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		want := base64.StdEncoding.EncodeToString(data)
		g, ok := got[in.name]
		if !ok {
			t.Errorf("%s: no text probe output", in.name)
			continue
		}
		fields := strings.Fields(g)
		if len(fields) != 2 {
			t.Errorf("%s: probe output %q is not two texts", in.name, g)
			continue
		}
		if fields[0] != want {
			t.Errorf("%s: SaveText %q, want Go base64 %q", in.name, fields[0], want)
		}
		back, err := base64.StdEncoding.DecodeString(fields[1])
		if err != nil {
			t.Errorf("%s: SaveText after LoadText is not base64: %v", in.name, err)
			continue
		}
		if string(back) != string(data) {
			t.Errorf("%s: SaveText(LoadText(base64(wire))) does not equal the wire", in.name)
		}
		for _, d := range []string{"d1", "d2", "d3"} {
			if !gotD[in.name][d] {
				t.Errorf("%s: damaged text %s was not refused as base64_damaged", in.name, d)
			}
		}
	}
	t.Logf("compared %d instances", count)
}

// TestSquirrelBase64TextNegativeControl patches the emitted text runtime in
// memory, one anchor matched exactly once, so the base64 pad is dropped on
// write; the text comparison must then go red.
func TestSquirrelBase64TextNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var paths []string
	for _, u := range units {
		if u.key == "tblv1" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tblv1 unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tblv1")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	const anchor = "    out += \"=\";"
	runtime := squirreltable.Runtime()["schema_table_text.nut"]
	if n := strings.Count(string(runtime), anchor); n != 1 {
		t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
	}

	inst := instanceRow{}
	for _, in := range readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")) {
		if in.unit == "tblv1" {
			inst = in
			break
		}
	}
	if inst.name == "" {
		t.Fatal("no tblv1 instance")
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(inst.wire)))
	if err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString(data)

	var p strings.Builder
	p.WriteString(textPrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q, %q, %q, %q, %q);\n",
		"tblv1", squirreltable.ModuleName(unit), inst.root, inst.name, want, "!", "!", "!")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	inputs := map[string][]byte{inst.name: data}

	got := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
	first := "loadrefused"
	for _, line := range got {
		if strings.HasPrefix(line, inst.name+" ") {
			first = line
			break
		}
	}
	fields := strings.Fields(first)
	if len(fields) >= 3 && fields[1] != "loadrefused" && fields[1] != "savenull" {
		if fields[1] != want {
			t.Fatalf("unpatched SaveText %q, want Go base64 %q", fields[1], want)
		}
	} else {
		t.Fatalf("probe failed before save: %q", first)
	}

	patched := strings.Replace(string(runtime), anchor, "    if (false) out += \"=\";", 1)
	writeFile(t, filepath.Join(out, "schema_table_text.nut"), []byte(patched))

	got2 := strings.Split(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n")
	first2 := "loadrefused"
	for _, line := range got2 {
		if strings.HasPrefix(line, inst.name+" ") {
			first2 = line
			break
		}
	}
	fields2 := strings.Fields(first2)
	if len(fields2) < 3 || fields2[1] == "loadrefused" || fields2[1] == "savenull" {
		t.Fatalf("patched probe failed: %q", first2)
	}
	if fields2[1] == want {
		t.Fatalf("the patched runtime still equals Go base64; the comparison did not go red")
	}
	t.Logf("unpatched %q, patched %q: the base64 comparison goes red", want, fields2[1])
}

// damageByte rewrites the wire so its base64 body carries a byte outside the
// standard alphabet (docs/SPEC-TABLES.md §16.8: base64_damaged).
func damageByte(wire []byte) string {
	s := base64.StdEncoding.EncodeToString(wire)
	out := []byte(s)
	out[0] = '!'
	return string(out)
}

// damagePad corrupts the standard padding of the base64 text.
func damagePad(b64 string) string {
	if len(b64) == 0 {
		return "="
	}
	out := []byte(b64)
	last := len(out) - 1
	if out[last] == '=' {
		out[last] = 'A'
	} else {
		out = append(out, '=')
	}
	return string(out)
}

// damageCut truncates the base64 text to a cut length.
func damageCut(b64 string) string {
	if len(b64) < 4 {
		return b64[:1]
	}
	return b64[:len(b64)-3]
}
