// TestEnginePointerTargetTypeChecked holds every row of
// testdata/wire/pointer-target-type/MANIFEST.tsv through the engine's strict
// and retaining reads (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// pointer-target-type-checked): a pointer naming a record of a type this
// reader declares that is not the slot's target type refuses the whole load,
// while a record of a type the reader does not declare stays unknown and the
// pointer reads null.
//
// TestEnginePointerTargetBlobKind holds the blob-kind vectors
// (testdata/wire/pointer-target-type/blobkind; docs/rulings.json
// blob-target-outside-closure-refused) the same way, and holds the engine's
// writer to never emitting a blob record no pointer reaches.
//
// TestEnginePointerTargetWitnesses holds every row of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json through
// the engine's reads: the Go oracle the leg witnesses are checked against.
package tablewire_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestEnginePointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	manifest, dir := readPointerTargetManifest(t, "../../testdata/wire/pointer-target-type/MANIFEST.tsv")
	for _, p := range pointerTargetManifestProblems(t, manifest, dir, newPointerTargetModels(t)) {
		t.Error(p)
	}
}

func TestEnginePointerTargetBlobKind(t *testing.T) {
	t.Parallel()
	models := newPointerTargetModels(t)
	manifest, dir := readPointerTargetManifest(t, "../../testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv")
	// compiled here, on this test's goroutine, so the subtests only read it
	m := models.get("test/pointer-target-type/blobkind/reader")

	t.Run("every row answers its verdict", func(t *testing.T) {
		for _, p := range pointerTargetManifestProblems(t, manifest, dir, models) {
			t.Error(p)
		}
	})
	t.Run("negative control: a flipped verdict", func(t *testing.T) {
		planted := strings.Replace(manifest, "text-names-bytes.bin\trefused", "text-names-bytes.bin\tread", 1)
		if planted == manifest {
			t.Fatal("the planted fault did not apply: the manifest has no text-names-bytes refused row")
		}
		if len(pointerTargetManifestProblems(t, planted, dir, models)) == 0 {
			t.Error("the gate passed a manifest that reads a *string slot naming a *bytes record")
		}
	})

	// A CONFORMING WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: the
	// engine's save numbers only the records its written slots name, so every
	// save of the reader's roots, the blob set or null, reads back accepted
	// with nothing unknown.
	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		for _, c := range []struct{ root, field string }{{"Text", "body"}, {"Bin", "data"}} {
			for _, data := range [][]byte{nil, {}, []byte("hello")} {
				inst := m.New(m.Lookup(c.root))
				if data != nil {
					fv, ok := inst.FieldByKey(c.field)
					if !ok {
						t.Fatalf("%s has no field %s", c.root, c.field)
					}
					fv.Cell.Blob = &tabletext.Blob{Data: data}
				}
				wire, err := tablewire.Encode(m, inst)
				if err != nil {
					t.Fatalf("%s.%s=%q: Encode: %v", c.root, c.field, data, err)
				}
				for _, p := range cleanReadBackProblems(m, c.root, wire) {
					t.Errorf("%s.%s=%q: the engine's own save does not read back clean: %s", c.root, c.field, data, p)
				}
			}
		}
	})
	// NEGATIVE CONTROL: the criterion above has a blade. A wire carrying a
	// blob record no pointer reaches fails it (refused), and so does a wire
	// that reads with a record counted unknown.
	t.Run("negative control: an orphan blob and an unknown record fail the writer criterion", func(t *testing.T) {
		for _, file := range []string{"text-orphan-bytes.bin", "text-unknown-field-bytes.bin"} {
			wire, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				t.Fatal(err)
			}
			if len(cleanReadBackProblems(m, "Text", wire)) != len(pointerTargetDecoders) {
				t.Errorf("%s: the writer criterion passed a wire it must refuse", file)
			}
		}
	})
}

// cleanReadBackProblems names each read (strict, retaining) of `wire` at
// `root` that is not accepted with nothing unknown: what a conforming
// writer's own save must always answer.
func cleanReadBackProblems(m *tabletext.Model, root string, wire []byte) []string {
	var problems []string
	for _, d := range pointerTargetDecoders {
		var report tabletext.Report
		ok, err := d.read(m, m.New(m.Lookup(root)), wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			problems = append(problems, fmt.Sprintf("(%s) %v %+v", d.name, err, report))
		}
	}
	return problems
}

func TestEnginePointerTargetWitnesses(t *testing.T) {
	t.Parallel()
	const dir = "../../testdata/conformance/feature-witnesses/pointer-targets"
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []pointerWitness
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 {
		t.Fatalf("cases.json holds %d rows, want 10", len(rows))
	}

	t.Run("every row answers its outcome", func(t *testing.T) {
		for _, p := range pointerWitnessProblems(t, m, rows) {
			t.Error(p)
		}
	})
	t.Run("negative control: a blob outside the closure read as unknown", func(t *testing.T) {
		planted := append([]pointerWitness(nil), rows...)
		i := slices.IndexFunc(planted, func(r pointerWitness) bool { return r.Name == "bytes outside root closure" })
		if i < 0 {
			t.Fatal("cases.json has no row \"bytes outside root closure\"")
		}
		planted[i].KindMismatch, planted[i].Unknown = 0, 1
		if len(pointerWitnessProblems(t, m, planted)) == 0 {
			t.Error("the gate passed a row that reads a *bytes record under a *string slot")
		}
	})
}

// pointerWitness is one row of the pointer-target feature witnesses: a value
// saved at the writer root and read at the reader root. kind_mismatch > 0
// means the read refuses; otherwise it is accepted with `unknown` records and
// the reader's value is `expected`.
type pointerWitness struct {
	Name, Writer, Reader string
	Input, Expected      json.RawMessage
	KindMismatch         int `json:"kind_mismatch"`
	Unknown              int
}

func pointerWitnessProblems(t *testing.T, m *tabletext.Model, rows []pointerWitness) []string {
	t.Helper()
	var problems []string
	for _, row := range rows {
		writer, reader := m.Lookup(row.Writer), m.Lookup(row.Reader)
		if writer == nil || reader == nil {
			t.Fatalf("%s: no table %q or %q", row.Name, row.Writer, row.Reader)
		}
		v := m.New(writer)
		var r tabletext.Report
		if !m.Read(v, row.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", row.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", row.Name, err)
		}
		for _, d := range pointerTargetDecoders {
			dest := m.New(reader)
			var report tabletext.Report
			ok, err := d.read(m, dest, wire, &report)
			if row.KindMismatch > 0 {
				if ok || !tablewire.ReadRefusal(err) || !report.Refused || report.Reason == "" {
					problems = append(problems, fmt.Sprintf("%s (%s): the read accepted a row that refuses: %v %+v", row.Name, d.name, err, report))
				}
				continue
			}
			if !ok || err != nil || report.Refused || report.Unknown != row.Unknown {
				problems = append(problems, fmt.Sprintf("%s (%s): want accepted with unknown %d: %v %+v", row.Name, d.name, row.Unknown, err, report))
				continue
			}
			text, err := m.Write(dest)
			if err != nil {
				t.Fatalf("%s: Write: %v", row.Name, err)
			}
			var got, want any
			if err := json.Unmarshal(text, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(row.Expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				problems = append(problems, fmt.Sprintf("%s (%s): read %s, want %s", row.Name, d.name, text, row.Expected))
			}
		}
	}
	return problems
}

type pointerTargetDecoder struct {
	name string
	read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
}

var pointerTargetDecoders = []pointerTargetDecoder{
	{"strict", tablewire.Decode},
	{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		return tablewire.DecodeRetain(m, inst, wire, &retain, report)
	}},
}

// pointerTargetModels compiles each unit a manifest names once.
type pointerTargetModels struct {
	t      *testing.T
	models map[string]*tabletext.Model
}

func newPointerTargetModels(t *testing.T) *pointerTargetModels {
	t.Helper()
	return &pointerTargetModels{t: t, models: map[string]*tabletext.Model{}}
}

func (p *pointerTargetModels) get(unit string) *tabletext.Model {
	p.t.Helper()
	if m := p.models[unit]; m != nil {
		return m
	}
	paths, err := compiler.GatherPaths([]string{filepath.Join("..", "..", unit)})
	if err != nil {
		p.t.Fatalf("%s: %v", unit, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		p.t.Fatalf("%s: %v", unit, err)
	}
	m := tabletext.NewModel(u)
	p.models[unit] = m
	return m
}

func readPointerTargetManifest(t *testing.T, path string) (manifest, dir string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), filepath.Dir(path)
}

// pointerTargetManifestProblems reads every manifest row through the strict
// and retaining reads and names each answer that is not the row's verdict. The
// models are compiled before any subtest runs in parallel, so the map is read
// only.
func pointerTargetManifestProblems(t *testing.T, manifest, dir string, models *pointerTargetModels) []string {
	t.Helper()
	var problems []string
	rows := 0
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s:%d: want name, unit, root, file, verdict, unknown; got %q", dir, i+1, line)
		}
		name, unit, root, file, verdict := f[0], f[1], f[2], f[3], f[4]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", dir, i+1, verdict)
		}
		wantUnknown := -1
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s:%d: unknown %q: %v", dir, i+1, f[5], err)
			}
			wantUnknown = n
		}
		m := models.get(unit)
		st := m.Lookup(root)
		if st == nil {
			t.Fatalf("%s: no root %q in %s", name, root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, d := range pointerTargetDecoders {
			var report tabletext.Report
			ok, err := d.read(m, m.New(st), wire, &report)
			switch verdict {
			case "read":
				if !ok || err != nil || report.Refused {
					problems = append(problems, fmt.Sprintf("%s (%s): the load refused a wire the manifest reads: %v %+v", name, d.name, err, report))
					continue
				}
				if wantUnknown >= 0 && report.Unknown != wantUnknown {
					problems = append(problems, fmt.Sprintf("%s (%s): unknown %d, want %d", name, d.name, report.Unknown, wantUnknown))
				}
			case "refused":
				if ok || err == nil || !report.Refused {
					problems = append(problems, fmt.Sprintf("%s (%s): the load accepted a wire the manifest refuses", name, d.name))
				}
			}
		}
		rows++
	}
	if rows == 0 {
		problems = append(problems, "no manifest rows read; the gate would pass over an empty set")
	}
	return problems
}
