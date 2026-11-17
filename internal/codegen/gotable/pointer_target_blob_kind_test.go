// TestGoPointerTargetBlobKindRefused holds the generated Go read of the
// variable form to docs/SPEC-TABLES.md §3.1 and §6.5 for blob targets
// (docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader names, whatever its root reaches: a slot the
// reader declares naming a blob record of the other kind refuses the whole
// read, and so does a blob record no pointer reaches; a blob record named only
// from a field the reader does not declare is a newer writer's, read and
// counted unknown. The rows are testdata/wire/pointer-target-type/blobkind/
// and its manifest, and the three closure rows of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json, each
// held first to the engine (the oracle) and then to every read entry of the
// generated reader. The generated writer is held to never emitting a blob
// record no pointer reaches.
package gotable

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	blobKindUnit    = "test/pointer-target-type/blobkind/reader"
	blobKindDir     = "testdata/wire/pointer-target-type/blobkind"
	blobWitnessDir  = "testdata/conformance/feature-witnesses/pointer-targets"
	blobWitnessRows = "table outside root closure|bytes outside root closure|string outside root closure"
)

// blobKindRow is one read the generated reader must answer: the wire, the
// root it is read at, the verdict and the unknown count (-1 when unpinned),
// and the root's one pointer field when the row holds it null.
type blobKindRow struct {
	name, root string
	wire       []byte
	read       bool
	unknown    int
	nullField  string
}

func TestGoPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it builds and runs the generated unit)")
	root := filepath.Join("..", "..", "..")

	readerUnit := loadUnitPaths(t, root, []string{blobKindUnit})
	readerModel := tabletext.NewModel(readerUnit)
	rows := blobKindManifestRows(t, root, readerUnit, readerModel)
	witnessUnit := loadUnitPaths(t, root, []string{blobWitnessDir})
	witnesses := blobKindWitnessRows(t, root, tabletext.NewModel(witnessUnit))

	t.Run("every manifest row through every read entry", func(t *testing.T) {
		t.Parallel()
		if out, err := runGeneratedUnit(t, readerUnit, blobKindProbe(readerUnit.Package, rows), nil, "-v"); err != nil {
			t.Fatalf("generated unit: %v\n%s", err, out)
		}
	})
	t.Run("the three closure witness rows through every read entry", func(t *testing.T) {
		t.Parallel()
		if out, err := runGeneratedUnit(t, witnessUnit, blobKindProbe(witnessUnit.Package, witnesses), nil, "-v"); err != nil {
			t.Fatalf("generated unit: %v\n%s", err, out)
		}
	})
	// NEGATIVE CONTROL: the old rule, where a blob the root's closure lacks
	// is one this reader cannot name, planted in a copy of the generated
	// reader. The gate must go red by name on text-names-bytes.
	t.Run("negative control: the root-closure rule goes red on text-names-bytes", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, readerUnit, blobKindProbe(readerUnit.Package, rows), plantRootClosureBlobRule(t), "-v")
		if err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: the root-closure rule left the gate green\n%s", out)
		}
		if !bytes.Contains(out, []byte("--- FAIL: TestBlobKind/text-names-bytes")) {
			t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on text-names-bytes\n%s", out)
		}
	})
	// A CONFORMING WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: the
	// generated save and retaining save of each root, the blob null, empty
	// and set, read back through the engine accepted with nothing unknown.
	t.Run("the generated writer emits no unreachable blob record", func(t *testing.T) {
		t.Parallel()
		out, err := runGeneratedUnit(t, readerUnit, blobKindWriterProbe(readerUnit), nil, "-v")
		if err != nil {
			t.Fatalf("generated unit: %v\n%s", err, out)
		}
		saved := blobKindSavedWires(out)
		if want := 2 * 3 * 2; len(saved) != want {
			t.Fatalf("the writer probe printed %d wires, want %d\n%s", len(saved), want, out)
		}
		for _, s := range saved {
			for _, p := range blobKindCleanReadBack(readerModel, s.root, s.wire) {
				t.Errorf("%s: the generated save does not read back clean through the engine: %s", s.name, p)
			}
		}
		// the criterion has a blade: the orphan and the unknown-field wires fail it
		for _, file := range []string{"text-orphan-bytes.bin", "text-unknown-field-bytes.bin"} {
			wire, err := os.ReadFile(filepath.Join(root, blobKindDir, file))
			if err != nil {
				t.Fatal(err)
			}
			if len(blobKindCleanReadBack(readerModel, "Text", wire)) == 0 {
				t.Errorf("%s: the writer criterion passed a wire no conforming writer emits", file)
			}
		}
	})
}

// blobKindManifestRows reads the blobkind manifest generically and holds each
// row to the engine before the generated reader is asked: the manifest's
// verdict and unknown count must be the engine's own.
func blobKindManifestRows(t *testing.T, root string, u *ir.Unit, m *tabletext.Model) []blobKindRow {
	t.Helper()
	dir := filepath.Join(root, blobKindDir)
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	var rows []blobKindRow
	for _, mr := range pointerTargetManifestRows(t, string(manifest)) {
		if mr.unit != blobKindUnit {
			t.Fatalf("%s: unit %s, want %s", mr.name, mr.unit, blobKindUnit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, mr.file))
		if err != nil {
			t.Fatalf("%s: %v", mr.file, err)
		}
		read, unknown := blobKindEngineVerdict(t, m, mr.root, wire)
		if read != mr.read || read && mr.unknown >= 0 && unknown != mr.unknown {
			t.Fatalf("%s: the engine answers read=%t unknown=%d, the manifest read=%t unknown=%d", mr.name, read, unknown, mr.read, mr.unknown)
		}
		row := blobKindRow{name: mr.name, root: mr.root, wire: wire, read: mr.read, unknown: mr.unknown}
		if mr.read && mr.unknown > 0 {
			row.nullField = blobKindPointerField(t, u, mr.root)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

func blobKindPointerField(t *testing.T, u *ir.Unit, root string) string {
	t.Helper()
	st := u.Tables[root]
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("%s must declare %s with one pointer field", blobKindUnit, root)
	}
	return goname.Member(st.Fields[0].Name)
}

// blobKindEngineVerdict is the oracle: the engine's strict and retaining
// reads must agree, and their answer is the verdict.
func blobKindEngineVerdict(t *testing.T, m *tabletext.Model, root string, wire []byte) (bool, int) {
	t.Helper()
	st := m.Lookup(root)
	if st == nil {
		t.Fatalf("no root %q", root)
	}
	var verdicts []string
	read, unknown := false, 0
	for i, d := range blobKindEngineReads {
		var report tabletext.Report
		ok, err := d(m, m.New(st), wire, &report)
		r := ok && err == nil && !report.Refused
		verdicts = append(verdicts, fmt.Sprintf("%t/%d", r, report.Unknown))
		if i == 0 {
			read, unknown = r, report.Unknown
		} else if r != read || r && report.Unknown != unknown {
			t.Fatalf("the engine's reads disagree at %s: %v", root, verdicts)
		}
	}
	return read, unknown
}

var blobKindEngineReads = []func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error){
	tablewire.Decode,
	func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		return tablewire.DecodeRetain(m, inst, wire, &retain, report)
	},
}

// blobKindWitnessRows are the pointer-target witness rows whose target lies
// outside the reader root's closure: the engine writes each row's input at
// its writer root, and its own read at the reader root is the verdict, which
// must be the row's (kind_mismatch above zero refuses).
func blobKindWitnessRows(t *testing.T, root string, m *tabletext.Model) []blobKindRow {
	t.Helper()
	dir := filepath.Join(root, blobWitnessDir)
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var rows []blobKindRow
	for _, c := range cases {
		if !slices.Contains(strings.Split(blobWitnessRows, "|"), c.Name) {
			continue
		}
		v := m.New(m.Lookup(c.Writer))
		var r tabletext.Report
		if !m.Read(v, c.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", c.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", c.Name, err)
		}
		read, unknown := blobKindEngineVerdict(t, m, c.Reader, wire)
		if read != (c.KindMismatch == 0) || read && unknown != c.Unknown {
			t.Fatalf("%s: the engine answers read=%t unknown=%d, the row kind_mismatch=%d unknown=%d", c.Name, read, unknown, c.KindMismatch, c.Unknown)
		}
		rows = append(rows, blobKindRow{name: strings.ReplaceAll(c.Name, " ", "-"), root: c.Reader, wire: wire, read: read, unknown: unknown})
	}
	if len(rows) != 3 {
		t.Fatalf("cases.json holds %d of the three closure rows", len(rows))
	}
	return rows
}

// blobKindProbe is a generated unit test: each row, as a subtest named after
// it, through every read entry: the two measures, the plain load, the
// retaining load, the builder load and the strict read.
func blobKindProbe(pkg string, rows []blobKindRow) string {
	var s strings.Builder
	fmt.Fprintf(&s, "package %s\nimport(\"testing\";\"unsafe\")\n", pkg)
	s.WriteString("func blobKindAligned(n int64)[]byte{raw:=make([]byte,n+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}\n")
	s.WriteString("func TestBlobKind(t *testing.T){\n")
	for _, row := range rows {
		fmt.Fprintf(&s, " t.Run(%q,func(t *testing.T){blobKind%s(t,[]byte{%s},%t,%d)})\n", row.name, row.root, byteLiteral(row.wire), row.read, row.unknown)
	}
	s.WriteString("}\n")
	done := map[string]bool{}
	for _, row := range rows {
		if done[row.root] {
			continue
		}
		done[row.root] = true
		null := "false"
		for _, r := range rows {
			if r.root == row.root && r.nullField != "" {
				null = "unknown>0&&v." + r.nullField + "!=0"
			}
		}
		fmt.Fprintf(&s, `func blobKind%[1]s(t *testing.T,wire []byte,read bool,unknown int){
 check:=func(where string,v *%[1]s,r *TableReport){
  if read {
   if v==nil||r.Refused(){t.Fatalf("%%s: read refused: %%+v",where,r)}
   if unknown>=0&&r.Unknown!=int32(unknown){t.Fatalf("%%s: unknown %%d want %%d",where,r.Unknown,unknown)}
   if %[2]s {t.Fatalf("%%s: the declared pointer reads non-null, want null",where)}
   return
  }
  if v!=nil||!r.Refused(){t.Fatalf("%%s: a refused row read: %%+v",where,r)}
  if r.Err()!=TableRefuseWireDamaged{t.Fatalf("%%s: refused %%v, want wire_damaged",where,r.Err())}
 }
 if n,err:=%[1]sLoadMeasureReason(wire);read {
  if err!=nil||n<0{t.Fatalf("measure reason refused a read row: %%d %%v",n,err)}
 } else if err==nil||n>=0{t.Fatalf("measure reason read a refused row: %%d %%v",n,err)}
 if n:=%[1]sLoadMeasure(wire);read&&n<0{t.Fatalf("measure refused a read row")}else if !read&&n>=0{t.Fatalf("measure read a refused row: %%d",n)}
 var r TableReport
 check("load",%[1]sLoad(blobKindAligned(1<<20),wire,&r),&r)
 store:=TableRetain{Bytes:make([]byte,1<<20),Ids:make([]TableRetainID,1024)}
 r=TableReport{}
 check("retain",%[1]sLoadRetain(blobKindAligned(1<<20),wire,&store,&r),&r)
 var b %[1]sBuilder
 if !b.Init(){t.Fatal("builder init")}
 defer b.Shutdown()
 r=TableReport{}
 var bv *%[1]s
 if %[1]sLoadBuilder(&b,wire,&r){bv=b.GetRoot()}
 check("builder",bv,&r)
 sv,serr:=%[1]sReadStrict(wire)
 if read {
  if serr!=nil||sv==nil{t.Fatalf("strict refused a read row: %%v",serr)}
 } else if serr!=TableRefuseWireDamaged||sv!=nil{t.Fatalf("strict: %%v, want wire_damaged",serr)}
}
`, row.root, null)
	}
	return s.String()
}

// blobKindWriterProbe saves Text and Bin with the blob null, empty and set,
// through the plain save and through the retaining save of the value the
// retaining load read back, and prints each wire for the engine to read.
func blobKindWriterProbe(u *ir.Unit) string {
	var s strings.Builder
	fmt.Fprintf(&s, "package %s\nimport(\"encoding/hex\";\"fmt\";\"testing\";\"unsafe\")\n", u.Package)
	s.WriteString("func blobKindAligned(n int64)[]byte{raw:=make([]byte,n+64);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;return raw[off:off+uintptr(n)]}\n")
	s.WriteString("func TestBlobKindWriter(t *testing.T){\n")
	for _, c := range []struct{ root, emplace string }{{"Text", "TableStringEmplace"}, {"Bin", "TableBytesEmplace"}} {
		field := blobKindFieldName(u, c.root)
		set := map[string]string{
			"null":  "",
			"empty": fmt.Sprintf("if %s(&b.Main,&b.GetRoot().%s,[]byte{})==nil{t.Fatal(\"emplace\")}", c.emplace, field),
			"set":   fmt.Sprintf("if %s(&b.Main,&b.GetRoot().%s,[]byte(\"hello\"))==nil{t.Fatal(\"emplace\")}", c.emplace, field),
		}
		if c.emplace == "TableBytesEmplace" {
			set["empty"] = fmt.Sprintf("if %s(&b.Main,&b.GetRoot().%s,0)==nil{t.Fatal(\"emplace\")}", c.emplace, field)
			set["set"] = fmt.Sprintf("if p:=%s(&b.Main,&b.GetRoot().%s,5);p==nil{t.Fatal(\"emplace\")}else{copy(p,\"hello\")}", c.emplace, field)
		}
		for _, k := range []string{"null", "empty", "set"} {
			fmt.Fprintf(&s, ` {var b %[1]sBuilder;if !b.Init(){t.Fatal("init")};%[3]s;if !b.Lock(){t.Fatal("lock")}
  v:=b.AsConst();n:=%[1]sMeasure(v);if n<0{t.Fatal("measure")};wire:=make([]byte,n);if %[1]sSave(v,wire)!=n{t.Fatal("save")}
  fmt.Printf("WIRE %[1]s %[1]s-%[2]s-save %%s\n",hex.EncodeToString(wire))
  store:=TableRetain{Bytes:make([]byte,1<<16),Ids:make([]TableRetainID,64)};var r TableReport
  rv:=%[1]sLoadRetain(blobKindAligned(1<<16),wire,&store,&r);if rv==nil{t.Fatalf("retaining load: %%+v",r)}
  m:=%[1]sMeasureRetain(rv,&store);if m<0{t.Fatal("measure retain")};out:=make([]byte,m);r=TableReport{};if %[1]sSaveRetain(rv,&store,out,&r)!=m{t.Fatalf("save retain: %%+v",r)}
  fmt.Printf("WIRE %[1]s %[1]s-%[2]s-save-retain %%s\n",hex.EncodeToString(out))
  b.Shutdown()}
`, c.root, k, set[k])
		}
	}
	s.WriteString("}\n")
	return s.String()
}

func blobKindFieldName(u *ir.Unit, root string) string {
	return goname.Member(u.Tables[root].Fields[0].Name)
}

type blobKindSaved struct {
	name, root string
	wire       []byte
}

func blobKindSavedWires(out []byte) []blobKindSaved {
	var saved []blobKindSaved
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 4 || f[0] != "WIRE" {
			continue
		}
		wire, err := hex.DecodeString(f[3])
		if err != nil {
			continue
		}
		saved = append(saved, blobKindSaved{name: f[2], root: f[1], wire: wire})
	}
	return saved
}

// blobKindCleanReadBack names each engine read of wire at root that is not
// accepted with nothing unknown.
func blobKindCleanReadBack(m *tabletext.Model, root string, wire []byte) []string {
	var problems []string
	for i, d := range blobKindEngineReads {
		var report tabletext.Report
		ok, err := d(m, m.New(m.Lookup(root)), wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			problems = append(problems, fmt.Sprintf("(engine read %d) %v %+v", i, err, report))
		}
	}
	return problems
}

// plantRootClosureBlobRule edits a copy of the generated reader so a blob
// record the root's closure lacks is one the reader cannot name.
func plantRootClosureBlobRule(t *testing.T) func(map[string][]byte) {
	return func(files map[string][]byte) {
		t.Fatal("negative control: no plant written yet")
	}
}
