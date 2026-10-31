package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestGoMessageFormGoldens asserts the Go leg answers every `connection` and
// `message` row of the cross-language manifest without the sibling runtime
// (docs/SPEC-TABLES.md §3.3). One subtest per manifest row, and the manifest is
// read here rather than copied: a new row is a new subtest with no edit.
//
// The rows are the reference's own bytes:
//
//   - "The ANNOUNCEMENT IS AN ORDINARY FORM-`1` FILE ... the generated Announce
//     bytes equal the announcement file and AnnounceRead accepts it." (§3.3)
//   - "LoadMessages of the batch against that connection's announcement gives
//     the values the form-1 instance decodes to." (§3.3)
//   - "SaveMessages of them gives the batch bytes back exactly." (§3.3)
func TestGoMessageFormGoldens(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated goldens)")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units, connections, rows := parseManifest(t, manifest)
	// Every unit a connection names is parsed once, here, before the parallel
	// subtests: loadUnitPaths is not concurrency-safe and the subtests must not
	// share a written cache.
	loaded := map[string]*ir.Unit{}
	for _, conn := range connections {
		if _, ok := loaded[conn.unit]; ok {
			continue
		}
		paths, ok := units[conn.unit]
		if !ok {
			t.Fatalf("manifest names unit %q with no unit line", conn.unit)
		}
		loaded[conn.unit] = loadUnitPaths(t, root, paths)
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			conn, ok := connections[row.connection]
			if !ok {
				t.Fatalf("manifest message %q names connection %q with no connection line", row.name, row.connection)
			}
			u, ok := loaded[conn.unit]
			if !ok {
				t.Fatalf("manifest message %q names unit %q with no unit line", row.name, conn.unit)
			}
			announcement := filepath.Join(root, conn.path)
			var source string
			switch {
			case row.file == "":
				source = connectionProbe(u.Package, announcement)
			case ir.VariableTables(u)[row.root]:
				// a variable table has no message form (§3.3), so the Go leg
				// answers this message row ABSENT and asserts nothing
				return
			default:
				source = messageProbe(u.Package, u, row.root, announcement, filepath.Join(root, row.file), filepath.Join(root, row.batch))
			}
			runGoGoldens(t, u, source)
		})
	}
}

type messageManifestConnection struct {
	unit, path string
}

type messageManifestRow struct {
	name, connection string
	root             string
	file, batch      string
}

// parseManifest reads the manifest itself, never a copied list. Only the two
// line kinds this card names are returned; every other kind is another
// surface's.
func parseManifest(t *testing.T, path string) (map[string][]string, map[string]messageManifestConnection, []messageManifestRow) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	connections := map[string]messageManifestConnection{}
	var rows []messageManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch fields[0] {
		case "unit":
			if len(fields) >= 3 {
				units[fields[1]] = fields[2:]
			}
		case "connection":
			if len(fields) >= 5 {
				connections[fields[1]] = messageManifestConnection{unit: fields[2], path: fields[4]}
				rows = append(rows, messageManifestRow{name: "connection_" + fields[1], connection: fields[1]})
			}
		case "message":
			if len(fields) >= 6 && fields[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
				rows = append(rows, messageManifestRow{name: fields[1], connection: fields[2], root: fields[3], file: fields[4], batch: fields[5]})
			}
		}
	}
	if len(connections) == 0 || len(rows) == 0 {
		t.Fatalf("%s carries no connection or message rows", path)
	}
	return units, connections, rows
}

// loadUnitPaths parses one unit's source files. A directory contributes its
// *.schema files; a named file contributes itself (compiler.GatherPaths' rule).
func loadUnitPaths(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		abs := filepath.Join(root, p)
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(abs)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(abs, e.Name()))
				}
			}
		} else {
			names = append(names, abs)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			files = append(files, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   f,
			})
		}
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// runGoGoldens emits a unit and runs one assertion program against it. Both
// emitters' files are generated, because the table codec's shared declarations
// (enums, flags, unions) are the packet emitter's. A generated file that names
// the sibling runtime is dropped rather than built: the unit test must answer
// without it, and a dropped file that carried a declaration the table codec
// needs fails the compile by name rather than silently.
func runGoGoldens(t *testing.T, u *ir.Unit, testSource string) {
	t.Helper()
	if out, err := runGoGoldensEdited(t, u, testSource, nil); err != nil {
		t.Fatalf("generated Go goldens: %v\n%s", err, out)
	}
}

// runGoGoldensEdited is runGoGoldens with an edit applied to the generated
// files before they are built, and the run's output and error returned rather
// than judged: a negative control plants a fault through it and requires red.
func runGoGoldensEdited(t *testing.T, u *ir.Unit, testSource string, edit func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for name, data := range files {
		// A file that names the sibling runtime is dropped rather than built:
		// the unit test answers without it. The reflection view (§8) is dropped
		// with it, because a closure's view names every packet type including
		// the cpp_native ones the sibling runtime carries — while the wire the
		// message surface owes names none of them.
		if bytes.Contains(data, []byte("github.com/mas-bandwidth/serialize.go/v2")) || strings.HasSuffix(name, "View.go") {
			delete(files, name)
		}
	}
	if edit != nil {
		edit(files)
	}
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	files["goldens_test.go"] = []byte(testSource)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func goGoldensHeader(pkg string) string {
	return fmt.Sprintf(`package %s

import (
	"bytes"
	"os"
	"testing"
)

func readGolden(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
`, pkg)
}

// connectionProbe asserts a `connection` row: the generated announcement is the
// reference's file byte for byte and the announcement reader accepts it.
func connectionProbe(pkg, announcement string) string {
	return goGoldensHeader(pkg) + fmt.Sprintf(`
func TestGolden(t *testing.T) {
	want := readGolden(t, %q)
	got := make([]byte, AnnounceMeasure())
	if n := Announce(got); n != int64(len(got)) {
		t.Fatalf("Announce: %%d != %%d", n, len(got))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the announcement bytes differ from the reference")
	}
	var vocabulary TableVocabulary
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var report TableReport
	if !AnnounceRead(&vocabulary, want, &report) {
		t.Fatalf("AnnounceRead refused the reference announcement: %%+v", report)
	}
	if vocabulary.Count != TableMessageEntriesHere {
		t.Fatalf("announced %%d entries, want %%d", vocabulary.Count, TableMessageEntriesHere)
	}
}
`, announcement)
}

// messageProbe asserts a `message` row for a fixed root: the file-form instance
// decodes to the value the batch gives, and saving that batch reproduces the
// batch wire exactly.
func messageProbe(pkg string, u *ir.Unit, root, announcement, fileWire, batchWire string) string {
	if hasPointers(u, root) {
		return regionMessageProbe(pkg, root, announcement, fileWire, batchWire)
	}
	const tmpl = `
func TestGolden(t *testing.T) {
	announcement := readGolden(t, @ANNOUNCE@)
	fileWire := readGolden(t, @FILE@)
	batchWire := readGolden(t, @BATCH@)
	var vocabulary TableVocabulary
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var announced TableReport
	if !AnnounceRead(&vocabulary, announcement, &announced) {
		t.Fatalf("AnnounceRead: %+v", announced)
	}
	var want @ROOT@
	var fileReport TableReport
	if !@ROOT@Load(&want, fileWire, &fileReport) {
		t.Fatalf("@ROOT@Load: %+v", fileReport)
	}
	storage := make([]@ROOT@, 256)
	var loadReport TableReport
	count, ok := @ROOT@LoadMessages(storage, &vocabulary, batchWire, &loadReport)
	if !ok || count != 1 {
		t.Fatalf("@ROOT@LoadMessages: count=%d ok=%v %+v", count, ok, loadReport)
	}
	if storage[0] != want {
		t.Fatalf("the batch's value is not the file-form instance's value")
	}
	values := []*@ROOT@{&storage[0]}
	n := @ROOT@MeasureMessages(values, nil)
	if n < 0 {
		t.Fatalf("@ROOT@MeasureMessages: %d", n)
	}
	out := make([]byte, n)
	if got := @ROOT@SaveMessages(values, out, nil); got != n {
		t.Fatalf("@ROOT@SaveMessages: %d != %d", got, n)
	}
	if !bytes.Equal(out, batchWire) {
		t.Fatalf("the batch bytes differ from the reference")
	}
}
`
	return goGoldensHeader(pkg) + goldenReplace(tmpl, root, announcement, fileWire, batchWire)
}

// goldenReplace folds a row's root name and three golden paths into a probe
// template. Tokens are used rather than positional verbs so a template edit
// cannot silently shift a path into an identifier.
func goldenReplace(tmpl, root, announcement, fileWire, batchWire string) string {
	return strings.NewReplacer(
		"@ROOT@", root,
		"@ANNOUNCE@", fmt.Sprintf("%q", announcement),
		"@FILE@", fmt.Sprintf("%q", fileWire),
		"@BATCH@", fmt.Sprintf("%q", batchWire),
	).Replace(tmpl)
}

// regionMessageProbe asserts a `message` row for a pointered root, whose form-1
// reader and message reader both take a region (§3.3). The two values are
// compared by their canonical form-1 saves, because a pointered value's
// self-relative deltas are a fact of its region rather than of the value.
func regionMessageProbe(pkg, root, announcement, fileWire, batchWire string) string {
	const tmpl = `package @PKG@

import (
	"bytes"
	"os"
	"testing"
	"unsafe"
)

func readGolden(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func align8(n int64) []byte {
	raw := make([]byte, n+7)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 7
	return raw[off : off+uintptr(n)]
}

func TestGolden(t *testing.T) {
	announcement := readGolden(t, @ANNOUNCE@)
	fileWire := readGolden(t, @FILE@)
	batchWire := readGolden(t, @BATCH@)
	var vocabulary TableVocabulary
	vocabulary.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var announced TableReport
	if !AnnounceRead(&vocabulary, announcement, &announced) {
		t.Fatalf("AnnounceRead: %+v", announced)
	}
	var fileReport TableReport
	want := @ROOT@Load(align8(@ROOT@LoadMeasure(fileWire)), fileWire, &fileReport)
	if want == nil {
		t.Fatalf("@ROOT@Load: %+v", fileReport)
	}
	size := @ROOT@LoadMessagesMeasure(&vocabulary, batchWire)
	if size <= 0 {
		t.Fatalf("@ROOT@LoadMessagesMeasure: %d", size)
	}
	region := align8(size)
	values := make([]*@ROOT@, 256)
	var loadReport TableReport
	count, ok := @ROOT@LoadMessages(values, region, &vocabulary, batchWire, &loadReport)
	if !ok || count != 1 {
		t.Fatalf("@ROOT@LoadMessages: count=%d ok=%v %+v", count, ok, loadReport)
	}
	fileSaved := make([]byte, @ROOT@Measure(want))
	if @ROOT@Save(want, fileSaved) != int64(len(fileSaved)) {
		t.Fatalf("saving the file-form value")
	}
	messageSaved := make([]byte, @ROOT@Measure(values[0]))
	if @ROOT@Save(values[0], messageSaved) != int64(len(messageSaved)) {
		t.Fatalf("saving the batch's value")
	}
	if !bytes.Equal(fileSaved, messageSaved) {
		t.Fatalf("the batch's value is not the file-form instance's value")
	}
	values = values[:count]
	n := @ROOT@MeasureMessages(values, nil)
	if n < 0 {
		t.Fatalf("@ROOT@MeasureMessages: %d", n)
	}
	out := make([]byte, n)
	if got := @ROOT@SaveMessages(values, out, nil); got != n {
		t.Fatalf("@ROOT@SaveMessages: %d != %d", got, n)
	}
	if !bytes.Equal(out, batchWire) {
		t.Fatalf("the batch bytes differ from the reference")
	}
}
`
	return strings.NewReplacer(
		"@PKG@", pkg,
		"@ROOT@", root,
		"@ANNOUNCE@", fmt.Sprintf("%q", announcement),
		"@FILE@", fmt.Sprintf("%q", fileWire),
		"@BATCH@", fmt.Sprintf("%q", batchWire),
	).Replace(tmpl)
}

// hasPointers reports whether a root of the unit is variable-class and so rides
// the region form on the message wire (docs/SPEC-TABLES.md §3.3).
func hasPointers(u *ir.Unit, root string) bool {
	return ir.VariableTables(u)[root]
}
