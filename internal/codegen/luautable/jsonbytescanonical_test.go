package luautable_test

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

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE JSON READ ACCEPTS ONLY CANONICAL BASE64 (docs/SPEC-TABLES.md §16.2 and
// §16.8; docs/rulings.json json-base64-canonical). The shared vectors under
// testdata/wire/json-base64-canonical/ name a verdict per row: the canonical
// spelling of two bytes reads, and a spelling that decodes but is not the text
// the writer writes — non-zero pad bits, missing padding — refuses the whole
// read, as does text that does not decode at all.
//
// The unit is tables/stream/Stream.schema, root Chunk, whose `data` field is
// bytes(8); every JSON text in the vectors holds two bytes, AB. The probe reads
// each row through the strict FromJson (the one read, §6.5) and prints the
// canonical bytes it placed, or `refused`.

type jsonBytesVectorRow struct {
	name, schema, root, file, verdict string
}

// readJSONBytesVectors reads the shared manifest: name, schema, root, json
// file, verdict, recommended reason. The reason is per-language detail
// (docs/rulings.json refusal-verdict-only), so only the verdict is held.
func readJSONBytesVectors(t *testing.T) []jsonBytesVectorRow {
	t.Helper()
	path := filepath.Join(root, "testdata", "wire", "json-base64-canonical", "MANIFEST.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []jsonBytesVectorRow
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
		rows = append(rows, jsonBytesVectorRow{name: w[0], schema: w[1], root: w[2], file: w[3], verdict: w[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// jsonBytesWant is the bytes a reading row's text decodes to. The canonical
// row is the writer's own text, so the standard decoder is the oracle here;
// the refused rows are never decoded.
func jsonBytesWant(t *testing.T, row jsonBytesVectorRow) []byte {
	t.Helper()
	path := filepath.Join(root, "testdata", "wire", "json-base64-canonical", row.file)
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

func TestLuauJsonBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter (it reads every shared base64 vector)")
	rows := readJSONBytesVectors(t)
	if len(rows) == 0 {
		t.Fatal("the shared base64 vectors are empty")
	}
	bySchema := map[string][]jsonBytesVectorRow{}
	for _, row := range rows {
		bySchema[row.schema] = append(bySchema[row.schema], row)
	}
	for _, schema := range slices.Sorted(maps.Keys(bySchema)) {
		rs := bySchema[schema]
		g := generateUnit(t, map[string][]string{schema: {schema}}, schema)
		if g.err != nil {
			t.Fatalf("%s: luau table: %v", schema, g.err)
		}
		inputs := map[string][]byte{}
		var b strings.Builder
		b.WriteString(jsonModules(g))
		b.WriteString(`local json = require("./schema_json")
local St = require("./schema_tables")
local inputs = require("./inputs")
local cases = {
`)
		for i, row := range rs {
			name := fmt.Sprintf("in%d", i)
			data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "json-base64-canonical", row.file))
			if err != nil {
				t.Fatal(err)
			}
			inputs[name] = data
			fmt.Fprintf(&b, "\t{ %q, %q },\n", name, row.root)
		}
		b.WriteString(`}
for _, c in cases do
	local name, root = c[1], c[2]
	local d = St.lookup(root)
	local fd = nil
	for _, f in d.fields do
		if f.name == "data" then fd = f end
	end
	if fd == nil then
		print("no-field")
	else
		local rep = {}
		local v = json.FromJson(root, inputs[name], rep)
		if v == nil then
			print("refused")
		else
			local s = v[fd.slot]
			local n = v[fd.slot .. "Length"] or buffer.len(s)
			local parts = table.create(n)
			for i = 0, n - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(s, i)) end
			print(table.concat(parts))
		end
	end
end
`)
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", inputs)), "\n"), "\n")
		if len(lines) != len(rs) {
			t.Fatalf("%s: the probe answered %d rows for %d texts:\n%s", schema, len(lines), len(rs), strings.Join(lines, "\n"))
		}
		for i, row := range rs {
			switch row.verdict {
			case "read":
				want := hex.EncodeToString(jsonBytesWant(t, row))
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
