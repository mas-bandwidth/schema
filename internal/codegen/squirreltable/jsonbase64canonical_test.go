package squirreltable_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonBase64Unit is one generated table surface the rows are read through.
type jsonBase64Unit struct {
	key    string
	module string
	unit   *ir.Unit
	model  *tabletext.Model
}

// jsonBase64Row is one JSON text, the root and field it places, and the
// verdict (and bytes) the standard states for it.
type jsonBase64Row struct {
	name      string
	unit      *jsonBase64Unit
	root      string
	field     string
	text      []byte
	wantRead  bool
	wantBytes []byte
}

// TestSquirrelJsonBase64Canonical holds the Squirrel JSON read of a bytes(N)
// value to canonical base64 of the string's DECODED value: the standard
// alphabet, padded, pad bits zero (docs/SPEC-TABLES.md §16.2, §16.8;
// docs/rulings.json json-base64-canonical). The engine (internal/tabletext)
// is the reference, confirmed first on every row: the vectors the standard
// landed under testdata/wire/json-base64-canonical/, the six grammar rows,
// and the canonical spellings written with JSON escapes. The Squirrel read is
// compared with the engine on the verdict alone, and on the bytes read where
// the text is accepted.
func TestSquirrelJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	generate := func(key string, paths []string) *jsonBase64Unit {
		t.Helper()
		u := loadUnit(t, repo, paths)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("%s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("%s: the table surface refused: %v", key, err)
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
		module := squirreltable.ModuleName(u)
		return &jsonBase64Unit{key: key, module: module, unit: u, model: tabletext.NewModel(u)}
	}

	stream := generate("stream", []string{"tables/stream/Stream.schema"})
	examples := generate("examples", []string{"tables/examples"})

	vectorDir := filepath.Join(repo, "testdata", "wire", "json-base64-canonical")
	manifest, err := os.Open(filepath.Join(vectorDir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []jsonBase64Row
	vectors := 0
	sc := bufio.NewScanner(manifest)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 5 {
			t.Fatalf("manifest row %q has %d columns, want at least 5", line, len(cols))
		}
		if cols[1] != "tables/stream/Stream.schema" || cols[2] != "Chunk" {
			t.Fatalf("manifest row %s names %s %s; this test reads Chunk in tables/stream/Stream.schema", cols[0], cols[1], cols[2])
		}
		if cols[4] != "read" && cols[4] != "refused" {
			t.Fatalf("manifest row %s states verdict %q", cols[0], cols[4])
		}
		text, err := os.ReadFile(filepath.Join(vectorDir, cols[3]))
		if err != nil {
			t.Fatal(err)
		}
		row := jsonBase64Row{
			name:     cols[0],
			unit:     stream,
			root:     "Chunk",
			field:    "data",
			text:     bytes.TrimRight(text, "\n"),
			wantRead: cols[4] == "read",
		}
		if row.wantRead {
			row.wantBytes = []byte("AB")
		}
		rows = append(rows, row)
		vectors++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	manifest.Close()
	if vectors == 0 {
		t.Fatal("the vector manifest names no rows")
	}
	entries, err := os.ReadDir(vectorDir)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files++
		}
	}
	if files != vectors {
		t.Fatalf("%d vectors on disk, the manifest names %d", files, vectors)
	}

	// The six grammar rows: none is the canonical spelling of its bytes.
	for _, body := range []string{"AQ=", "A===", "====", "AQL6=", "AQ==AQ==", "A"} {
		rows = append(rows, jsonBase64Row{
			name:  "grammar-" + body,
			unit:  stream,
			root:  "Chunk",
			field: "data",
			text:  []byte(`{ "data": "` + body + `" }`),
		})
	}

	// The canonical spelling written with JSON escapes: the JSON layer decides
	// first, so the escaped text decodes to canonical base64 and reads.
	rows = append(rows,
		jsonBase64Row{
			name:      "escaped-solidus",
			unit:      examples,
			root:      "ProfileConfig",
			field:     "icon",
			text:      []byte(`{ "icon": "\/\/\/\/" }`),
			wantRead:  true,
			wantBytes: []byte{0xff, 0xff, 0xff},
		},
		jsonBase64Row{
			name:      "escaped-letter",
			unit:      examples,
			root:      "ProfileConfig",
			field:     "icon",
			text:      []byte(`{ "icon": "\u0041Q==" }`),
			wantRead:  true,
			wantBytes: []byte{0x01},
		},
	)

	// STEP ONE: the engine states the standard's verdict (and bytes) on every
	// row. A disagreement means the reference is not what the card assumes.
	engine := make([]string, len(rows))
	for i, row := range rows {
		st := row.unit.model.Lookup(row.root)
		if st == nil {
			t.Fatalf("%s: the engine has no %s table", row.name, row.root)
		}
		inst := row.unit.model.New(st)
		var report tabletext.Report
		ok := row.unit.model.Read(inst, row.text, &report)
		if ok != row.wantRead {
			t.Fatalf("refuted: the engine reads %s as %t, the standard states %t: text %s (report %+v)",
				row.name, ok, row.wantRead, row.text, report)
		}
		if !ok {
			engine[i] = "refused"
			continue
		}
		fv, found := inst.FieldByKey(row.field)
		if !found {
			t.Fatalf("%s: the engine has no %s field", row.name, row.field)
		}
		got := fv.Cell.Str
		if fv.Cell.Blob != nil {
			got = fv.Cell.Blob.Data
		}
		if row.wantBytes != nil && !bytes.Equal(got, row.wantBytes) {
			t.Fatalf("refuted: the engine reads %s as %x, the standard states %x", row.name, got, row.wantBytes)
		}
		engine[i] = "read " + hex.EncodeToString(got)
	}

	inputs := map[string][]byte{}
	var probe strings.Builder
	probe.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	probe.WriteString(`local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local join = function(a) {
  if (a.len() == 0) return "";
  while (a.len() > 1) {
    local b = array((a.len() + 1) / 2);
    for (local i = 0; i < b.len(); i++) b[i] = (2 * i + 1 < a.len()) ? a[2 * i] + a[2 * i + 1] : a[2 * i];
    a = b;
  }
  return a[0];
};
local hexOf = function(b) {
  local parts = array(b.len());
  for (local i = 0; i < b.len(); i++) parts[i] = HEXB[b[i] & 0xff];
  return join(parts);
};
`)
	probe.WriteString("local rows = [\n")
	for _, row := range rows {
		inputs[row.name] = row.text
		fmt.Fprintf(&probe, "  { key=%q, unit=%q, mod=%q, root=%q, field=%q },\n",
			row.name, row.unit.key, row.unit.module, row.root, row.field)
	}
	probe.WriteString(`];
foreach (row in rows) {
  local schema = dofile(row.unit + "/schema_module.nut", true);
  local T = schema.require(row.mod);
  local J = schema.require("schema_table_json");
  local r = { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
  local v = J.FromJson(T, row.root, inputs[row.key], r);
  if (v == null) { print(row.key + " refused\n"); continue; }
  local member = T.Descriptors[row.root].byKey[row.field].member;
  print(row.key + " read " + hexOf(v[member]) + "\n");
}
`)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe.String()))
	got := map[string]string{}
	lines := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n"), "\n") {
		key, result, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("probe line %q is not key result", line)
		}
		got[key] = result
		lines++
	}
	if lines != len(rows) {
		t.Fatalf("the probe printed %d rows, want %d", lines, len(rows))
	}
	for i, row := range rows {
		g, ok := got[row.name]
		if !ok {
			t.Errorf("%s: the probe printed no row", row.name)
			continue
		}
		if g != engine[i] {
			t.Errorf("%s: Squirrel answered %q, the engine %q, text %s", row.name, g, engine[i], row.text)
		}
	}
}
