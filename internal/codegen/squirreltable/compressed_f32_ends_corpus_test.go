package squirreltable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// f32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each row
// owes; internal/tablewire's TestCompressedF32EndsCorpus runs it on the
// reference.
var f32EndsCorpusDir = filepath.Join("..", "..", "..", "testdata", "conformance", "feature-witnesses", "compressed-f32-ends")

// f32EndsMain runs every cases.tsv row (I.cases, the data rows only) through
// the generated Squirrel and prints one line per row: "<case> refused" or
// "<case> saved <hex>", the save being in the row's form (a write) or the
// root's own form (a read).
const f32EndsMain = `local I = dofile("inputs.nut", true);
local T = dofile("probe/schema_module.nut", true).require(%q);
local text = function(b) { local s = ""; for (local i = 0; i < b.len(); i++) s += (b[i] & 0xff).tochar(); return s; };
local hex = function(b) { local s = ""; for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff); return s; };
local own = { V = "variable", F = "fixed" };
local save = function(form, root, v) {
  try {
    if (form == "variable") return T.VSave(v);
    local out = blob(T.FFixedMeasure(1));
    return T.FFixedSave(out, [v]) < 0 ? null : out;
  } catch (e) { return null; }
};
local read = function(form, root, src) {
  try {
    if (form == "json") return root == "V" ? T.VFromJson(text(src)) : T.FFromJson(text(src));
    if (form == "variable") return T.VLoad(src);
    local got = [T.F()];
    return T.FFixedLoad(src, got, null) == 1 ? got[0] : null;
  } catch (e) { return null; }
};
local set = function(v, path, x) {
  local open = path.find("[");
  local name = open == null ? path : path.slice(0, open);
  local field = name.slice(0, 1).toupper() + name.slice(1);
  if (open == null) v[field] = x;
  else v[field][path.slice(open + 1, path.len() - 1).tointeger()] = x;
};
local f32bits = function(hexbits) {
  local n = 0;
  foreach (c in hexbits) n = n * 16 + (c <= '9' ? c - '0' : c - 'a' + 10);
  local b = blob(4); b.writen(n, 'i'); b.seek(0);
  return b.readn('f');
};
foreach (line in split(text(I.cases), "\n")) {
  local w = split(line, "\t");
  local name = w[0], op = w[1], form = w[2], root = w[3], input = w[4], path = w[5], bits = w[6];
  local out;
  if (op == "read") {
    local v = read(form, root, form == "json" ? I[input + ".json"] : I[input + "." + form + ".bin"]);
    if (v == null) out = "refused";
    else { local b = save(own[root], root, v); out = b == null ? "unsaved" : "saved " + hex(b); }
  } else {
    local v = read("json", root, I[input + ".json"]);
    if (v == null) out = "unloaded";
    else {
      set(v, path, f32bits(bits));
      local b = save(form, root, v);
      out = b == null ? "refused" : "saved " + hex(b);
    }
  }
  print(name + " " + out + "\n");
}
`

// TestSquirrelCompressedF32EndsCorpus runs the compressed-f32-ends corpus on
// the generated Squirrel: every read and write of a compressed float holds its
// range at float32, in the variable, fixed and JSON forms, and the saves match
// the reference byte for byte.
func TestSquirrelCompressedF32EndsCorpus(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	sqrun.Require(t)
	t.Parallel()
	inputs, rows := f32EndsCorpus(t)
	schema, err := os.ReadFile(filepath.Join(f32EndsCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unitFromSchema(t, string(schema)))
	writeFile(t, filepath.Join(dir, "main.nut"), fmt.Appendf(nil, f32EndsMain, mod))
	f32EndsCheck(t, inputs, rows, sqrun.Run(t, dir, "main.nut", inputs))
}

// f32EndsCorpus reads the corpus: every file by name, and the data rows of
// cases.tsv (also handed to the probe as inputs["cases"]).
func f32EndsCorpus(t *testing.T) (map[string][]byte, [][]string) {
	t.Helper()
	entries, err := os.ReadDir(f32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(f32EndsCorpusDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		inputs[e.Name()] = b
	}
	var rows [][]string
	var cases strings.Builder
	for line := range strings.SplitSeq(string(inputs["cases.tsv"]), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv: %q has %d fields, want 8", line, len(f))
		}
		rows = append(rows, f)
		cases.WriteString(line + "\n")
	}
	if len(rows) == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
	inputs["cases"] = []byte(cases.String())
	return inputs, rows
}

// f32EndsCheck holds the probe's lines to cases.tsv: a `refused` row owes the
// refusal, a stem owes the save <stem>.<form>.bin byte for byte.
func f32EndsCheck(t *testing.T, inputs map[string][]byte, rows [][]string, raw []byte) {
	t.Helper()
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		k, v, _ := strings.Cut(line, " ")
		got[k] = v
	}
	own := map[string]string{"V": "variable", "F": "fixed"}
	for _, r := range rows {
		name, op, form, root, expect := r[0], r[1], r[2], r[3], r[7]
		if expect == "refused" {
			if got[name] != "refused" {
				t.Errorf("%s: %q, want refused", name, got[name])
			}
			continue
		}
		saved := own[root]
		if op == "write" {
			saved = form
		}
		want := inputs[expect+"."+saved+".bin"]
		have, ok := strings.CutPrefix(got[name], "saved ")
		if b, err := hex.DecodeString(have); !ok || err != nil || !bytes.Equal(b, want) {
			t.Errorf("%s: %q, want saved %s", name, got[name], hex.EncodeToString(want))
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", raw)
	}
}
