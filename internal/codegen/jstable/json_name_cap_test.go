package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// jsNameCapSchema is the unit testdata/wire/json-read-closed reads: a flags
// field, an enum, a union, a string-keyed map and a string field, plus a
// pointered root for the &node name's site.
const jsNameCapSchema = `package closed

enum Kind { Alpha, Beta }

flags Perms { Read, Write }

type Buff
{
    mult float32 = 1.0
}

union Effect
{
    buff Buff
    ping
}

table Node
{
    value int32 = 0
}

table Scene
{
    head *Node
}

table Closed
{
    kind   Kind
    perms  Perms
    effect Effect
    tags   map[string]int32
    note   string(300)
}
`

// A schema-name string is at most 255 bytes after decoding, at every name
// site, including a key inside a skipped object (§16.2). A longer one refuses
// the whole read. A map key and a string value are data and are not refused
// for that cap. The verdict is read or refused; the reason is not compared.
func TestJsJsonNameCap(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)

	const dir = "../../../testdata/wire/json-read-closed"
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		root, file, verdict string
	}
	named := map[string]vector{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("MANIFEST.tsv:%d: want 5 columns, got %d", i+1, len(fields))
		}
		named[fields[0]] = vector{root: fields[1], file: fields[2], verdict: fields[3]}
	}

	// The name-cap rows only. Depth and UTF-8 rows belong to other cards.
	want := []string{
		"field-key-255", "field-key-256",
		"skipped-key-255", "skipped-key-256",
		"enum-name-255", "enum-name-256",
		"flags-element-255", "flags-element-256",
		"union-arm-255", "union-arm-256",
		"ampersand-255", "ampersand-256",
		"map-key-long", "string-value-long",
	}

	u := unitFrom(t, jsNameCapSchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(work, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	home := runtimeHome(u)

	type input struct {
		Name string `json:"name"`
		Root string `json:"root"`
		Text string `json:"text"`
	}
	inputs := make([]input, len(want))
	expectRead := make([]bool, len(want))
	for i, name := range want {
		v, ok := named[name]
		if !ok {
			t.Fatalf("MANIFEST.tsv names no name-cap vector %q", name)
		}
		if v.verdict != "read" && v.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, v.verdict)
		}
		body, err := os.ReadFile(filepath.Join(dir, v.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = input{Name: name, Root: v.root, Text: string(body)}
		expectRead[i] = v.verdict == "read"
	}
	spec, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "cases.json"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import { readFileSync } from "node:fs";
import { ClosedFromJson, SceneFromJson, TableVariableReport } from "./` + home + `Variable.js";
const rows = JSON.parse(readFileSync("cases.json", "utf8"));
const read = { Closed: ClosedFromJson, Scene: SceneFromJson };
for (const row of rows) {
  const value = read[row.root](row.text, new TableVariableReport());
  console.log(JSON.stringify({ name: row.name, ok: value !== null }));
}
`
	if err := os.WriteFile(filepath.Join(work, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "probe.mjs")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native name-cap probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(want) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(want), out)
	}
	for i, name := range want {
		var got struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, lines[i])
		}
		if got.Name != name {
			t.Errorf("row %d is %q, want %q", i, got.Name, name)
		}
		if got.OK != expectRead[i] {
			wantWord := "refused"
			if expectRead[i] {
				wantWord = "read"
			}
			gotWord := "refused"
			if got.OK {
				gotWord = "read"
			}
			t.Errorf("%s: JavaScript %s, want %s", name, gotWord, wantWord)
		}
	}
}
