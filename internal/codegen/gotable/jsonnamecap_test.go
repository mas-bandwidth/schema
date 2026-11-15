package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// The read-closed unit the vectors under testdata/wire/json-read-closed read
// (docs/SPEC-TABLES.md §16.2): a flags field, an enum, a union, a map keyed by
// string and a string field, plus a pointered root for the `&` name's site.
const jsonNameCapSchema = `package closed

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

// jsonNameCapProbe reads the name-cap rows through the generated read and
// reports each VERDICT only: read, or refused. The refusal's name is the
// language's own diagnostic (docs/rulings.json refusal-verdict-only).
const jsonNameCapProbe = `package closed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	jsonNameCapVectors  = %q
	jsonNameCapManifest = %q
)

var jsonNameCapRows = map[string]bool{
	"field-key-255":      true,
	"field-key-256":      true,
	"skipped-key-255":    true,
	"skipped-key-256":    true,
	"enum-name-255":      true,
	"enum-name-256":      true,
	"flags-element-255":  true,
	"flags-element-256":  true,
	"union-arm-255":      true,
	"union-arm-256":      true,
	"ampersand-255":      true,
	"ampersand-256":      true,
	"map-key-long":       true,
	"string-value-long":  true,
}

func jsonNameCapRead(t *testing.T, root string, body []byte) bool {
	t.Helper()
	switch root {
	case "Closed":
		var b ClosedBuilder
		if !b.Init() {
			t.Fatal("Closed builder init")
		}
		defer b.Shutdown()
		var report TableReport
		return ClosedFromJSON(&b, body, &report) && !report.Refused()
	case "Scene":
		var b SceneBuilder
		if !b.Init() {
			t.Fatal("Scene builder init")
		}
		defer b.Shutdown()
		var report TableReport
		return SceneFromJSON(&b, body, &report) && !report.Refused()
	}
	t.Fatalf("unknown root %%q", root)
	return false
}

func TestGoJsonNameCapProbe(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(jsonNameCapManifest)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	rows := 0
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("MANIFEST.tsv:%%d: want name, root, json, verdict, description, got %%d columns", i+1, len(fields))
		}
		name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
		if !jsonNameCapRows[name] {
			continue
		}
		body, err := os.ReadFile(filepath.Join(jsonNameCapVectors, jsonFile))
		if err != nil {
			t.Errorf("%%s: %%v", name, err)
			continue
		}
		got := jsonNameCapRead(t, root, body)
		want := verdict == "read"
		if got != want {
			t.Errorf("%%s (%%s): read=%%v, want %%s", name, root, got, verdict)
		}
		seen[name] = true
		rows++
	}
	for name := range jsonNameCapRows {
		if !seen[name] {
			t.Errorf("no manifest row for %%s", name)
		}
	}
	if rows == 0 {
		t.Fatal("no name-cap rows read; the gate would pass over an empty set")
	}
}
`

func TestGoJsonNameCap(t *testing.T) {
	t.Parallel()
	vectors, err := filepath.Abs("../../../testdata/wire/json-read-closed")
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(jsonNameCapProbe, vectors, filepath.Join(vectors, "MANIFEST.tsv"))
	out, err := runGeneratedEdited(t, jsonNameCapSchema, probe, nil)
	if err != nil {
		t.Fatalf("the generated Go JSON read does not hold the name cap: %v\n%s", err, out)
	}
}
