package goosetable

// THE JSON READ ACCEPTS ONLY CANONICAL BASE64 (docs/SPEC-TABLES.md §16.2 and
// §16.8; docs/rulings.json json-base64-canonical). The shared vectors under
// testdata/wire/json-base64-canonical/ name a verdict per row: the canonical
// spelling of two bytes reads, and a spelling that decodes but is not the text
// the writer writes — non-zero pad bits, missing padding — refuses the whole
// read, as does text that does not decode at all.
//
// The unit is tables/stream/Stream.schema, root Chunk, whose `data` field is
// bytes(8); every JSON text in the vectors holds two bytes, AB. The probe reads
// each row through the flat value table (the variable class's text walk,
// runtime/json_value.goose) and prints the canonical eight bytes it placed, or
// `refused`.

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type jsonBase64Row struct {
	name, schema, root, file, verdict string
}

// readJSONBase64Vectors reads the shared manifest: name, schema, root, json
// file, verdict, recommended reason. The reason is per-language detail
// (docs/rulings.json refusal-verdict-only), so only the verdict is held.
func readJSONBase64Vectors(t *testing.T) []jsonBase64Row {
	t.Helper()
	path := filepath.Join(fixedRoot, "testdata", "wire", "json-base64-canonical", "MANIFEST.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []jsonBase64Row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		w := strings.Fields(line)
		if len(w) < 5 {
			t.Fatalf("%s: malformed row %q", path, line)
		}
		rows = append(rows, jsonBase64Row{name: w[0], schema: w[1], root: w[2], file: w[3], verdict: w[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// jsonBase64Want is the bytes a reading row's text decodes to. The canonical
// row is the writer's own text, so the standard decoder is the oracle here;
// the refused rows are never decoded.
func jsonBase64Want(t *testing.T, row jsonBase64Row) []byte {
	t.Helper()
	path := filepath.Join(fixedRoot, "testdata", "wire", "json-base64-canonical", row.file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("%s: %v", row.file, err)
	}
	var body string
	if err := json.Unmarshal(obj["data"], &body); err != nil {
		t.Fatalf("%s: data is not a string: %v", row.file, err)
	}
	want, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("%s: canonical row does not decode: %v", row.file, err)
	}
	return want
}

func TestGooseJsonBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it reads every shared base64 vector)")
	rows := readJSONBase64Vectors(t)
	if len(rows) == 0 {
		t.Fatal("the shared base64 vectors are empty")
	}
	bySchema := map[string][]jsonBase64Row{}
	for _, row := range rows {
		bySchema[row.schema] = append(bySchema[row.schema], row)
	}
	for _, schema := range slices.Sorted(maps.Keys(bySchema)) {
		rs := bySchema[schema]
		u, err := loadSchemaPaths([]string{filepath.Join(fixedRoot, filepath.FromSlash(schema))})
		if err != nil {
			t.Fatalf("load %s: %v", schema, err)
		}
		files, err := goose.Generate(u)
		if err != nil {
			t.Fatalf("%s: goose: %v", schema, err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: goosetable: %v", schema, err)
		}
		maps.Copy(files, tables)
		var mods []string
		for name := range files {
			if before, ok := strings.CutSuffix(name, ".goose"); ok {
				mods = append(mods, before)
			}
		}
		slices.Sort(mods)
		ns := goose.Namespace(u)
		var b strings.Builder
		b.WriteString("import os;\n")
		for _, m := range mods {
			fmt.Fprintf(&b, "import .%s;\n", m)
		}
		b.WriteString("\n" + gooserun.HexProbe + "\n")
		b.WriteString(strings.ReplaceAll(`fn probe(name: const u8[:], root: const u8[:]) {
    var text: u8[>..] = [];
    if !read_file(name, text) { print("read-failed"); return; }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::json_report();
    let at = schema_rt::json_load_table(NS::TableUnit, NS::TextUnit, root, text, data, vals, nodes, r);
    if at < 0 || r.damaged { print("refused"); return; }
    var ord: i64 = -1;
    for tb in NS::TableUnit.tables {
        if tb.name == root {
            for f, i in tb.fields {
                if f.shape == schema_rt::ShapeBytes && ord < 0 { ord = i; }
            }
        }
    }
    if ord < 0 { print("no-bytes-field"); return; }
    let s = vals[at].first + ord;
    if vals[s].state != schema_rt::ValPresent { print("absent"); return; }
    var out: u8[>..] = [];
    for i in vals[s].len { out.push(data[vals[s].off + i]); }
    hex(out);
}

fn main() {
`, "NS", ns))
		inputs := map[string][]byte{}
		for i, row := range rs {
			name := fmt.Sprintf("in%d.json", i)
			data, err := os.ReadFile(filepath.Join(fixedRoot, "testdata", "wire", "json-base64-canonical", row.file))
			if err != nil {
				t.Fatal(err)
			}
			inputs[name] = data
			fmt.Fprintf(&b, "    probe(%q, %q);\n", name, row.root)
		}
		b.WriteString("}\n")
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n"), "\n")
		if len(lines) != len(rs) {
			t.Fatalf("%s: the probe answered %d rows for %d texts:\n%s", schema, len(lines), len(rs), strings.Join(lines, "\n"))
		}
		for i, row := range rs {
			switch row.verdict {
			case "read":
				want := strings.ToLower(hex.EncodeToString(jsonBase64Want(t, row)))
				if lines[i] != want {
					t.Errorf("%s (%s %s): read %q, want the canonical bytes %q", row.name, schema, row.root, lines[i], want)
				}
			case "refused":
				if lines[i] != "refused" {
					t.Errorf("%s (%s %s): read %q, want refused", row.name, schema, row.root, lines[i])
				}
			default:
				t.Fatalf("%s: unknown verdict %q", row.name, row.verdict)
			}
		}
	}
}
