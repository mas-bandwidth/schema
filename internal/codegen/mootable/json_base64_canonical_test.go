package mootable

// THE JSON READ ACCEPTS ONLY CANONICAL BASE64 (docs/SPEC-TABLES.md §16.2 and
// §16.8; docs/rulings.json json-base64-canonical). The shared vectors under
// testdata/wire/json-base64-canonical/ name a verdict per row: the canonical
// spelling of two bytes reads, and a spelling that decodes but is not the text
// the writer writes — non-zero pad bits, missing padding — refuses the whole
// read, as does text that does not decode at all.
//
// The unit is tables/stream/Stream.schema, root Chunk, whose `data` field is
// bytes(8); every JSON text in the vectors holds two bytes, AB. The probe reads
// each row through the generated ChunkFromJson and prints the bytes it placed,
// or `refused`. The same verdict is then held at every bytes site a declaration
// can carry: a field, a map value, an optional and a union arm.

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func mooBase64UnitFromSource(t *testing.T, src string) *ir.Unit {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{p})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	return u
}

type mooBase64Vector struct{ name, schema, root, file, verdict string }

// readMooBase64Vectors reads the shared manifest: name, schema, root, json
// file, verdict, recommended reason. The reason is per-language detail
// (docs/rulings.json refusal-verdict-only), so only the verdict is held.
func readMooBase64Vectors(t *testing.T) []mooBase64Vector {
	t.Helper()
	path := filepath.Join(repoRoot, "testdata", "wire", "json-base64-canonical", "MANIFEST.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []mooBase64Vector
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
		rows = append(rows, mooBase64Vector{name: w[0], schema: w[1], root: w[2], file: w[3], verdict: w[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// mooBase64VectorWant is the hex of the bytes a reading row's text decodes to.
// The canonical row is the writer's own text, so the strict standard decoder is
// the oracle here; the refused rows are never decoded.
func mooBase64VectorWant(t *testing.T, row mooBase64Vector) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "wire", "json-base64-canonical", row.file))
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
	want, err := base64.StdEncoding.Strict().DecodeString(body)
	if err != nil {
		t.Fatalf("%s: canonical row does not decode: %v", row.file, err)
	}
	return hex.EncodeToString(want)
}

const mooBase64BytesReadProbe = `local function probe(mod: any, root: string, input: string)
	local value = mod["New" .. root]()
	local ok = mod[root .. "FromJson"](value, inputs[input], {})
	if not ok then
		hexs("refused")
		return
	end
	local d = mod[root .. "Descriptor"]
	local slot: string? = nil
	for _, fd in d.fields do
		if fd.s == "bytes" then
			slot = fd.slot
			break
		end
	end
	if slot == nil then
		hexs("no-bytes-field")
		return
	end
	local n = value[slot .. "Length"]
	local parts = table.create(n)
	for i = 0, n - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(value[slot], i))
	end
	hexs(table.concat(parts))
end
`

const mooBase64SiteProbe = `local function site(mod: any, root: string, input: string)
	local value = mod["New" .. root]()
	local ok = mod[root .. "FromJson"](value, inputs[input], {})
	if not ok then
		hexs("refused")
		return
	end
	local size = mod[root .. "ToJsonMeasure"](value)
	if size < 0 then
		hexs("write-refused")
		return
	end
	local out = buffer.create(size)
	mod[root .. "ToJson"](value, out)
	hexs(buffer.tostring(out))
end
`

func TestMooJsonBytesReadCanonicalBase64Only(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs json.moo's canonical base64 read)")
	rows := readMooBase64Vectors(t)
	if len(rows) == 0 {
		t.Fatal("the shared base64 vectors are empty")
	}
	bySchema := map[string][]mooBase64Vector{}
	for _, row := range rows {
		bySchema[row.schema] = append(bySchema[row.schema], row)
	}
	for _, schema := range sortedKeys(bySchema) {
		rs := bySchema[schema]
		u := loadUnit(t, []string{schema})
		dir := generateUnitDir(t, u, nil)
		var b strings.Builder
		b.WriteString(probePrelude)
		b.WriteString(mooBase64BytesReadProbe)
		inputs := map[string][]byte{}
		for i, row := range rs {
			name := fmt.Sprintf("r%d", i)
			data, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "wire", "json-base64-canonical", row.file))
			if err != nil {
				t.Fatal(err)
			}
			inputs[name] = data
			fmt.Fprintf(&b, "probe(require(\"./%s\"), %q, %q)\n", tableModule(u, row.root), row.root, name)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := decodeHexLines(t, moorun.Run(t, dir, "main.moo", inputs))
		if len(lines) != len(rs) {
			t.Fatalf("%s: the probe answered %d rows for %d texts:\n%s", schema, len(lines), len(rs), strings.Join(lines, "\n"))
		}
		for i, row := range rs {
			switch row.verdict {
			case "read":
				if want := mooBase64VectorWant(t, row); lines[i] != want {
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

	mooBase64Sites(t)
}

// mooBase64Sites holds the same verdict where a bytes value is not a bare
// field: a map value, an optional and a union arm. An array of bytes(N) is a
// named follow-on the compiler refuses (§15), so it cannot appear here.
func mooBase64Sites(t *testing.T) {
	t.Helper()
	const schema = `package probe

table Field {
    v bytes(8)
}

table Pointer {
    v *bytes
}

table OptionalBlob {
    v bytes(8)
}

table Optional {
    v ?OptionalBlob
}

table Map {
    v map[uint32]bytes(8)
}

union ArmUnion {
    blob bytes(8)
}

table Arm {
    v ArmUnion
}
`
	type siteCase struct {
		name, root, text string
		read             bool
	}
	var cases []siteCase
	add := func(name, root, text string, read bool) {
		cases = append(cases, siteCase{name, root, text, read})
	}
	for _, root := range []string{"Field", "Pointer", "Optional", "Map", "Arm"} {
		var wrap func(string) string
		switch root {
		case "Map":
			wrap = func(v string) string { return `{"v":{"7":"` + v + `"}}` }
		case "Optional":
			wrap = func(v string) string { return `{"v":{"v":"` + v + `"}}` }
		case "Arm":
			wrap = func(v string) string { return `{"v":{"blob":"` + v + `"}}` }
		default:
			wrap = func(v string) string { return `{"v":"` + v + `"}` }
		}
		add(root+"/canonical", root, wrap("QUI="), true)
		add(root+"/unpadded", root, wrap("QUI"), false)
		add(root+"/pad-bit-set", root, wrap("QUJ="), false)
		add(root+"/not-base64", root, wrap("$$$$"), false)
	}

	u := mooBase64UnitFromSource(t, schema)
	dir := generateUnitDir(t, u, nil)
	var b strings.Builder
	b.WriteString(probePrelude)
	b.WriteString(mooBase64SiteProbe)
	inputs := map[string][]byte{}
	for i, c := range cases {
		name := fmt.Sprintf("s%d", i)
		inputs[name] = []byte(c.text)
		fmt.Fprintf(&b, "site(require(\"./%s\"), %q, %q)\n", tableModule(u, c.root), c.root, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := decodeHexLines(t, moorun.Run(t, dir, "main.moo", inputs))
	if len(lines) != len(cases) {
		t.Fatalf("the site probe answered %d rows for %d texts:\n%s", len(lines), len(cases), strings.Join(lines, "\n"))
	}
	for i, c := range cases {
		if !c.read {
			if lines[i] != "refused" {
				t.Errorf("%s: read %q, want refused", c.name, lines[i])
			}
			continue
		}
		if lines[i] == "refused" || lines[i] == "write-refused" {
			t.Errorf("%s: %s, want the canonical value", c.name, lines[i])
			continue
		}
		if !strings.Contains(lines[i], `"QUI="`) {
			t.Errorf("%s: wrote %q, want the canonical bytes QUI=", c.name, lines[i])
		}
	}
}
