package pythontable

// THE BYTE BUFFERS on the Python leg (ROADMAP.md, Variable tables: blobs and
// string-identity; docs/SPEC-TABLES.md §2.5, §3.1; docs/PORT-PYTHON.md, THE
// TABLE FRAME). A *bytes or *string field is a pointer to a blob record. On
// this leg a buffer is a Python value, so the save numbers ONE record per
// non-null slot BY VALUE: a caller's equal strings in two slots are two
// records, never one shared node. The read keeps the wire's node indices, so a
// wire that names one blob record from two slots is refused by name
// (blob_shared), and a *string record's bytes must be well-formed UTF-8 with
// no zero byte.
//
// TestPythonVtBlobs holds every manifest blob instance to the one read on
// read-then-write under both pinned interpreters, then holds the string
// identity cases: decoded equal values, built equal values, empty values, one
// node named from two slots, and one table object reached twice (in two
// by-value slots, and by value and by pointer), which saves as two equal copies
// do. TestPythonVtBlobsNegativeControl plants the identity faults in a COPY of
// the runtime and requires its rows red.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type blobManifestRow struct {
	caseName string
	unit     string
	root     string
	wire     string
	refusal  bool
}

// blobManifestRows reads the tables manifest and answers the instance rows of
// the blob unit, in manifest order, with the wire-refused marker.
func blobManifestRows(t *testing.T, path string) ([]blobManifestRow, map[string][]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	var rows []blobManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
			continue
		}
		if f[0] != "instance" || len(f) < 5 || f[2] != "blobdemo" {
			continue
		}
		refusal := false
		for _, x := range f[5:] {
			if x == "wire-refused" || strings.HasPrefix(x, "go-refusal=") {
				refusal = true
			}
		}
		rows = append(rows, blobManifestRow{caseName: f[1], unit: f[2], root: f[3], wire: f[4], refusal: refusal})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest names no blobdemo instance")
	}
	return rows, units
}

// loadUnitPaths builds one unit from schema paths, as the manifest names them.
func loadUnitPaths(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path: name, Name: filepath.Base(name), Base: strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: ast,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

func blobWriteFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const blobManifestProbe = `data = open("wire", "rb").read()
from blobdemo import Catalog, InvalidValue, Refused
try:
    value = Catalog.load(data)
except Refused as err:
    print("refused", err.reason)
else:
    try:
        saved = value.save()
    except InvalidValue as err:
        print("write_refused", err.rule)
    else:
        print("saved", saved.hex(), "same" if saved == data else "diff", value.measure() == len(saved))
`

// blobManifestFailures runs every manifest blob row under both pins and
// returns the named failures and how many rows the run held. mutate, when set,
// edits the generated files first.
func blobManifestFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows, units := blobManifestRows(t, manifest)
	unitCache := map[string]*ir.Unit{}
	var fails []string
	held := 0
	for _, row := range rows {
		u, ok := unitCache[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u = loadUnitPaths(t, paths...)
			unitCache[row.unit] = u
		}
		files, err := Generate(u)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s: the backend refused the unit: %v", row.caseName, err))
			continue
		}
		if mutate != nil {
			edited := make(map[string][]byte, len(files))
			for name, data := range files {
				edited[name] = data
			}
			mutate(t, edited)
			files = edited
		}
		dir := t.TempDir()
		blobWriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(pyrun.ImportPrelude+blobManifestProbe), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
		}
		inputs := map[string][]byte{"wire": wire}
		current := strings.TrimSpace(string(pyrun.Run(t, dir, "main.py", inputs)))
		floor := strings.TrimSpace(string(pyrun.RunFloor(t, dir, "main.py", inputs)))
		held++
		if current != floor {
			fails = append(fails, fmt.Sprintf("%s: the pinned interpreters disagree: %q vs %q", row.caseName, current, floor))
			continue
		}
		if row.refusal {
			want := "refused blob_shared"
			if current != want {
				fails = append(fails, fmt.Sprintf("%s: the read gave %q, want %q", row.caseName, current, want))
			}
			continue
		}
		parts := strings.Fields(current)
		if len(parts) != 4 || parts[0] != "saved" || parts[2] != "same" || parts[3] != "True" {
			fails = append(fails, fmt.Sprintf("%s: the read-then-write is %q", row.caseName, current))
		}
	}
	return fails, held
}

const stringIdentitySchema = `package probe

table Child
{
    caption *string
}

table Root
{
    a      *string
    b      *string
    child  *Child
    data_a *bytes
    data_b *bytes
}

table Inner
{
    s *string
}

table Pair
{
    a Inner
    b Inner
}

table Mixed
{
    a Inner
    p *Inner
}
`

const stringIdentityPrelude = `from probe import Inner, InvalidValue, Mixed, Pair, Refused, Root
`

const stringIdentityProbe = `def read_file(name):
    with open(name, "rb") as handle:
        return handle.read()

def roundtrip(name):
    data = read_file(name)
    try:
        value = Root.load(data)
    except Refused as err:
        print(name, "refused", err.reason)
        return
    try:
        saved = value.save()
    except InvalidValue as err:
        print(name, "write_refused", err.rule)
        return
    print(name, "saved", saved.hex(), "same" if saved == data else "diff", value.measure() == len(saved))

def built_string(text, name):
    value = Root()
    value.a = text
    value.b = text
    a, b = value.a, value.b
    saved = value.save()
    reference = read_file(name)
    print(name, "saved", saved.hex(), "same" if saved == reference else "diff", value.measure() == len(saved), value.a == a and value.b == b)

def built_bytes(value_bytes, name):
    value = Root()
    value.data_a = value_bytes
    value.data_b = value_bytes
    a, b = value.data_a, value.data_b
    saved = value.save()
    reference = read_file(name)
    print(name, "saved", saved.hex(), "same" if saved == reference else "diff", value.measure() == len(saved), value.data_a == a and value.data_b == b)

def built_twice(cls, other, name):
    inner = Inner()
    inner.s = "hello"
    value = cls()
    value.a = inner
    setattr(value, other, inner)
    saved = value.save()
    reference = read_file(name)
    try:
        again = "True" if cls.load(saved).save() == saved else "False"
    except Refused as err:
        again = "refused_" + err.reason
    untouched = inner.s == "hello" and value.a is inner and getattr(value, other) is inner
    print(name, "saved", saved.hex(), "same" if saved == reference else "diff", value.measure() == len(saved), untouched, again)

for name in ("decoded_string", "decoded_all", "decoded_empty", "shared_string"):
    roundtrip(name)
built_string("same", "built_same")
built_string("", "built_empty")
built_bytes(bytes.fromhex("10203040"), "built_bytes")
built_twice(Pair, "b", "twice_by_value")
built_twice(Mixed, "p", "twice_value_and_pointer")
print("OK")
`

// stringIdentityReference is the Go engine's form-1 wire of text at Root.
func stringIdentityReference(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	return stringIdentityReferenceAt(t, u, "Root", text)
}

// stringIdentityReferenceAt is the Go engine's form-1 wire of text at root.
func stringIdentityReferenceAt(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables[root])
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read %s cleanly: %+v", text, rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write %s: %v", text, err)
	}
	return wire
}

func blobVarint(dst []byte, value uint64) []byte {
	for {
		b := byte(value & 0x7F)
		value >>= 7
		if value != 0 {
			dst = append(dst, b|0x80)
			continue
		}
		return append(dst, b)
	}
}

// sharedStringWire spells a root whose a and b slots name ONE *string node,
// the wire every writer refuses as blob_shared (§2.5).
func sharedStringWire(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	st := u.Tables["Root"]
	var a, b *ir.Field
	for _, f := range st.Fields {
		switch f.Name {
		case "a":
			a = f
		case "b":
			b = f
		}
	}
	if a == nil || b == nil {
		t.Fatal("the probe unit has no Root.a and Root.b")
	}
	ids := []uint64{ir.TableFieldWireId(a), ir.TableFieldWireId(b), ir.TableNodeWireId, ir.StringWireTypeId}
	body := []byte{}
	body = blobVarint(body, 1)
	body = append(body, 17)
	body = blobVarint(body, 2)
	body = blobVarint(body, 2)
	body = append(body, 17)
	body = blobVarint(body, 2)
	body = blobVarint(body, 3)
	body = append(body, 12)
	payload := []byte{}
	payload = blobVarint(payload, 1)
	payload = blobVarint(payload, 4)
	payload = blobVarint(payload, uint64(len(text)))
	payload = append(payload, text...)
	body = blobVarint(body, uint64(len(payload)))
	body = append(body, payload...)
	body = append(body, 0)
	out := append([]byte{1}, body...)
	for _, id := range ids {
		var word [8]byte
		binary.LittleEndian.PutUint64(word[:], id)
		out = append(out, word[:]...)
	}
	var count [8]byte
	binary.LittleEndian.PutUint64(count[:], uint64(len(ids)))
	return append(out, count[:]...)
}

// stringIdentityFailures runs the string identity cases under both pins.
func stringIdentityFailures(t *testing.T, mutate func(*testing.T, map[string][]byte)) ([]string, int) {
	t.Helper()
	u := loadUnit(t, stringIdentitySchema)
	files, err := Generate(u)
	if err != nil {
		return []string{fmt.Sprintf("string-identity: the backend refused the unit: %v", err)}, 0
	}
	if mutate != nil {
		edited := make(map[string][]byte, len(files))
		for name, data := range files {
			edited[name] = data
		}
		mutate(t, edited)
		files = edited
	}
	dir := t.TempDir()
	blobWriteFiles(t, dir, files)
	probe := pyrun.ImportPrelude + stringIdentityPrelude + stringIdentityProbe
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	references := map[string][]byte{
		"decoded_string": stringIdentityReference(t, u, `{"a":"same","b":"same"}`),
		"decoded_all":    stringIdentityReference(t, u, `{"a":"same","b":"same","child":{"caption":"same"},"data_a":"ECAwQA==","data_b":"ECAwQA=="}`),
		"decoded_empty":  stringIdentityReference(t, u, `{"a":"","b":"","data_a":"","data_b":""}`),
		"built_same":     stringIdentityReference(t, u, `{"a":"same","b":"same"}`),
		"built_empty":    stringIdentityReference(t, u, `{"a":"","b":""}`),
		"built_bytes":    stringIdentityReference(t, u, `{"data_a":"ECAwQA==","data_b":"ECAwQA=="}`),
		"shared_string":  sharedStringWire(t, u, "same"),
		// One Inner object in both slots saves as two equal copies do.
		"twice_by_value":          stringIdentityReferenceAt(t, u, "Pair", `{"a":{"s":"hello"},"b":{"s":"hello"}}`),
		"twice_value_and_pointer": stringIdentityReferenceAt(t, u, "Mixed", `{"a":{"s":"hello"},"p":{"s":"hello"}}`),
	}
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for name, data := range references {
		inputs[name] = data
	}
	current := strings.TrimSpace(string(pyrun.Run(t, dir, "main.py", inputs)))
	floor := strings.TrimSpace(string(pyrun.RunFloor(t, dir, "main.py", inputs)))
	var fails []string
	if current != floor {
		fails = append(fails, fmt.Sprintf("string-identity: the pinned interpreters disagree:\n%s\n%s", current, floor))
		return fails, 1
	}
	lines := strings.Split(current, "\n")
	if len(lines) == 0 || lines[len(lines)-1] != "OK" {
		fails = append(fails, "string-identity: the probe did not finish")
	}
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 0 || parts[0] == "OK" {
			continue
		}
		name := parts[0]
		if name == "shared_string" {
			if len(parts) != 3 || parts[1] != "refused" || parts[2] != "blob_shared" {
				fails = append(fails, fmt.Sprintf("%s: the read gave %q, want refused blob_shared", name, line))
			}
			continue
		}
		if strings.HasPrefix(name, "twice_") {
			if len(parts) != 7 || parts[1] != "saved" || parts[3] != "same" || parts[4] != "True" || parts[6] != "True" {
				fails = append(fails, fmt.Sprintf("%s: the save of one table object in two slots is %q", name, line))
				continue
			}
			if parts[5] != "True" {
				fails = append(fails, fmt.Sprintf("%s: the caller's objects moved", name))
			}
			continue
		}
		if strings.HasPrefix(name, "built_") {
			if len(parts) != 6 || parts[1] != "saved" || parts[3] != "same" || parts[4] != "True" {
				fails = append(fails, fmt.Sprintf("%s: the read-then-write is %q", name, line))
				continue
			}
			if parts[5] != "True" {
				fails = append(fails, fmt.Sprintf("%s: the caller's objects moved", name))
			}
			continue
		}
		if len(parts) != 5 || parts[1] != "saved" || parts[3] != "same" || parts[4] != "True" {
			fails = append(fails, fmt.Sprintf("%s: the read-then-write is %q", name, line))
		}
	}
	return fails, len(lines) - 1
}

// TestPythonVtBlobs is the blob row and the string-identity row: every manifest
// blob instance and the equal-value cases, under both pinned interpreters.
//
// Functional (python pins): run with SCHEMA_SLOW=1.
func TestPythonVtBlobs(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	fails, held := blobManifestFailures(t, nil)
	for _, f := range fails {
		t.Error(f)
	}
	if held == 0 {
		t.Fatal("the manifest blob rows held nothing")
	}
	identityFails, identityHeld := stringIdentityFailures(t, nil)
	for _, f := range identityFails {
		t.Error(f)
	}
	if identityHeld == 0 {
		t.Fatal("the string identity cases held nothing")
	}
	t.Logf("manifest rows held %d; string identity answers held %d", held, identityHeld)
}

// TestPythonVtBlobsNegativeControl plants, one at a time, the two identity
// faults in a COPY of the runtime:
//
//   - BY CONTENT: two slots with equal bytes share one node, so a caller's
//     equal strings are no longer written by value; blob_equal_bytes and the
//     built equal-value cases must go red;
//   - BY OWNER: the write keys a slot's record by the owning table object's
//     identity, so one object in two slots names one record and orphans the
//     other; twice_by_value and twice_value_and_pointer must go red;
//   - NO SHARED REFUSAL: the read accepts a wire that names one blob record
//     from two slots, so blob_shared and shared_string no longer refuse.
func TestPythonVtBlobsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "python (it runs the emitted table runtime)")
	plant := func(file, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src, ok := files[file]
			if !ok {
				t.Fatalf("%s is not in the generated tree", file)
			}
			if strings.Count(string(src), from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(string(src), from))
			}
			files[file] = []byte(strings.Replace(string(src), from, to, 1))
		}
	}
	const saveByContentFrom = `            index = len(ctx.order) + 2
            ctx.order.append(kind_blob._BlobNode(field.codec == "string_blob", body))
            blobs.append(index)
`
	const saveByContentTo = `            index = ctx.indices.get(body)
            if index is None:
                index = len(ctx.order) + 2
                ctx.order.append(kind_blob._BlobNode(field.codec == "string_blob", body))
                ctx.indices[body] = index
            blobs.append(index)
`
	// BY OWNER: the write reuses the first record it gave an owner's slot,
	// keyed by the owning table's identity (the shape that orphaned a record).
	const byOwnerFrom = `        index = ctx.blob_slots[ctx.blob_next]
        ctx.blob_next += 1
`
	const byOwnerTo = `        index = ctx.blob_slots[ctx.blob_next]
        ctx.blob_next += 1
        index = ctx.refs.setdefault((id(ctx.current), fd.field_id), index)
`
	const sharedFrom = `        if blob and names[k + 1] > 1:
            raise Refused("blob_shared", "one blob record named from two slots")
`
	const sharedTo = `        if blob and names[k + 1] > 1:
            continue
`
	for _, ctl := range []struct {
		name           string
		manifestMutate func(*testing.T, map[string][]byte)
		identityMutate func(*testing.T, map[string][]byte)
		rows           []string
		answers        []string
	}{
		{
			"by-content",
			plant("blobdemo/_schema/save.py", saveByContentFrom, saveByContentTo),
			plant("probe/_schema/save.py", saveByContentFrom, saveByContentTo),
			[]string{"blob_equal_bytes"},
			[]string{"built_same", "built_empty", "built_bytes", "decoded_string", "decoded_all"},
		},
		{
			"by-owner",
			nil,
			plant("probe/_schema/kind_blob.py", byOwnerFrom, byOwnerTo),
			nil,
			[]string{"twice_by_value", "twice_value_and_pointer"},
		},
		{
			"no-shared-refusal",
			plant("blobdemo/_schema/load.py", sharedFrom, sharedTo),
			plant("probe/_schema/load.py", sharedFrom, sharedTo),
			[]string{"blob_shared"},
			[]string{"shared_string"},
		},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			var manifestFails []string
			if ctl.manifestMutate != nil {
				manifestFails, _ = blobManifestFailures(t, ctl.manifestMutate)
			}
			identityFails, _ := stringIdentityFailures(t, ctl.identityMutate)
			red := map[string]bool{}
			for _, f := range append(manifestFails, identityFails...) {
				name, _, _ := strings.Cut(f, ":")
				red[name] = true
			}
			for _, row := range ctl.rows {
				if !red[row] {
					t.Errorf("%s: %s stayed green (manifest findings %q, identity findings %q)", ctl.name, row, manifestFails, identityFails)
				}
			}
			for _, answer := range ctl.answers {
				if !red[answer] {
					t.Errorf("%s: %s stayed green (manifest findings %q, identity findings %q)", ctl.name, answer, manifestFails, identityFails)
				}
			}
		})
	}
}
