package lobstertable_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// base64Row is one JSON text read into a bytes(N) field: its unit and root,
// the text, and for a canonical spelling the bytes the read must place.
type base64Row struct {
	name, unit, root, field string
	text                    []byte
	verdict                 string
	want                    []byte
}

// base64Rows is every vector of testdata/wire/json-base64-canonical (its
// MANIFEST.tsv), then the canonical spelling written with JSON escapes, which
// the reference decodes first (internal/tabletext/base64escape_test.go), then
// the same rule on tables/examples' ProfileConfig icon.
func base64Rows(t *testing.T) []base64Row {
	t.Helper()
	dir := filepath.Join(root, "testdata", "wire", "json-base64-canonical")
	f, err := os.Open(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string]string{"tables/stream/Stream.schema": "streamdemo"}
	var rows []base64Row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		x := strings.Split(line, "\t")
		if len(x) != 6 {
			t.Fatalf("MANIFEST.tsv: %q has %d columns, want 6", line, len(x))
		}
		unit, ok := units[x[1]]
		if !ok {
			t.Fatalf("MANIFEST.tsv: %s names schema %s, which this test does not generate", x[0], x[1])
		}
		text, err := os.ReadFile(filepath.Join(dir, x[3]))
		if err != nil {
			t.Fatal(err)
		}
		r := base64Row{name: x[0], unit: unit, root: x[2], field: "data", text: text, verdict: x[4]}
		if r.verdict == "read" {
			r.want = []byte("AB")
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vector")
	}
	return append(rows,
		base64Row{name: "escaped-solidus-read", unit: "streamdemo", root: "Chunk", field: "data", text: []byte(`{ "data": "\/\/\/\/" }`), verdict: "read", want: []byte{0xff, 0xff, 0xff}},
		base64Row{name: "escaped-letter-read", unit: "streamdemo", root: "Chunk", field: "data", text: []byte(`{ "data": "AQ==" }`), verdict: "read", want: []byte{0x01}},
		base64Row{name: "icon-canonical-read", unit: "tabledemo", root: "ProfileConfig", field: "icon", text: []byte(`{"icon":"AQ=="}`), verdict: "read", want: []byte{0x01}},
		base64Row{name: "icon-pad-bit-set-refused", unit: "tabledemo", root: "ProfileConfig", field: "icon", text: []byte(`{"icon":"AR=="}`), verdict: "refused"},
		base64Row{name: "icon-unpadded-refused", unit: "tabledemo", root: "ProfileConfig", field: "icon", text: []byte(`{"icon":"AR"}`), verdict: "refused"},
	)
}

// TestLobsterJsonBase64Canonical holds the Lobster JSON read of a bytes(N)
// value to canonical base64 (docs/SPEC-TABLES.md §16.2, §16.8): the standard
// alphabet, padded, pad bits zero, of the string's decoded value; any other
// spelling that decodes is refused. Every row is first held to the reference
// engine (internal/tabletext): its verdict is the row's, and on a read its
// bytes are the row's. The Lobster verdict must then be the engine's, and a
// Lobster read writes back the engine's text.
func TestLobsterJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]jsonRow{}
	rowsOf := map[string][]base64Row{}
	var order []string
	for _, r := range base64Rows(t) {
		if _, seen := byUnit[r.unit]; !seen {
			order = append(order, r.unit)
		}
		byUnit[r.unit] = append(byUnit[r.unit], jsonRow{kind: "json-hostile", name: r.name, unit: r.unit, root: r.root})
		rowsOf[r.unit] = append(rowsOf[r.unit], r)
	}
	compared, refusals := 0, 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: the Lobster table surface refuses it: %v", key, err)
		}
		rows := rowsOf[key]
		// the reference first: its verdict and bytes are the row's
		m := tabletext.NewModel(g.u)
		want := make([][]byte, len(rows))
		for i, r := range rows {
			inst := m.New(m.Lookup(r.root))
			var rep tabletext.Report
			goV := "refused"
			if m.Read(inst, r.text, &rep) && !rep.Refused {
				goV = "read"
			}
			if goV != r.verdict {
				t.Fatalf("%s: the reference %s, the standard %s", r.name, goV, r.verdict)
			}
			if goV == "read" {
				fv, ok := inst.FieldByKey(r.field)
				if !ok {
					t.Fatalf("%s: no field keyed %q on %s", r.name, r.field, r.root)
				}
				got := fv.Cell.Str
				if !bytes.Equal(got, r.want) {
					t.Fatalf("%s: the reference read %x, want %x", r.name, got, r.want)
				}
				_, want[i] = goJsonRead(t, g.u, r.root, r.text)
			}
		}
		// the hostile program reads <root>/<tree>/<table>.json for each row
		hostile := byUnit[key]
		for i, r := range rows {
			dir := filepath.Join(g.dir, "b64", r.name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, r.root+".json"), r.text, 0o644); err != nil {
				t.Fatal(err)
			}
			absRoot, err := filepath.Abs(root)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(absRoot, dir)
			if err != nil {
				t.Fatal(err)
			}
			hostile[i].tree = rel
		}
		res := runJsonUnit(t, g, hostile)
		for _, e := range res.errs {
			t.Errorf("unit %s: %s", key, e)
		}
		for i, r := range rows {
			got := res.verdict[i]
			if got != r.verdict {
				t.Errorf("%s: lobster %s, the reference %s", r.name, got, r.verdict)
				continue
			}
			if got == "read" && !bytes.Equal(res.written[i], want[i]) {
				t.Errorf("%s: the read value writes back %q, the reference %q", r.name, res.written[i], want[i])
				continue
			}
			if got == "refused" {
				refusals++
			}
			compared++
		}
	}
	if compared == 0 || refusals == 0 {
		t.Fatalf("%d rows compared, %d refusals: the comparison is vacuous", compared, refusals)
	}
	t.Logf("%d rows hold the reference's verdict, %d of them refusals", compared, refusals)
}
