// A blob record's kind in the variable form (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader can name, whatever its root reaches: a slot
// this reader declares that names a blob record of the other kind refuses the
// whole read, and so does a blob record no pointer reaches. A blob record
// named only from a field this reader does not declare is a newer writer's
// evolution: it reads, counted unknown with the field that names it.
//
// The engine (internal/tablewire) is the oracle. Every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv and the three
// closure rows of testdata/conformance/feature-witnesses/pointer-targets are
// read by the engine and by every C++ read of the variable form (the region
// Load, the retaining Load, the builder Load, the strict read and the
// measure), and the C++ answer must be the engine's: the verdict, the unknown
// count and whether the declared pointer reads set. The verdict is the
// contract and the refusal's name is per-language detail (docs/rulings.json
// refusal-verdict-only), so the reasons are logged beside it, not compared.
// The C++ writer's saves (the builder save, the region save and the retaining
// save) of each root with its blob null, empty and set are decoded by the
// engine and must read back accepted with nothing unknown: a writer never
// emits a blob record no pointer reaches.
package cpptable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cppBlobKindCase is one wire read at one root, with the engine's answer.
type cppBlobKindCase struct {
	name, root, field string // field: the root's declared pointer
	wire              []byte
	verdict, reason   string // the engine's verdict and, for a refusal, its reason
	unknown           int
	set               bool // a read: the declared pointer reads set
}

// cppBlobKindPaths are the C++ reads of the variable form each case runs
// through. The strict read reports no counters, so only its verdict and the
// pointer are compared; the measure answers a verdict alone.
var cppBlobKindPaths = []string{"load", "retain", "builder", "strict", "measure"}

// cppBlobKindDecoders are the engine's two reads; they must agree on every case.
var cppBlobKindDecoders = []struct {
	name string
	read func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
}{
	{"strict", tablewire.Decode},
	{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		return tablewire.DecodeRetain(m, inst, wire, &retain, report)
	}},
}

// cppBlobKindOracle is the engine's answer for one wire at one root.
func cppBlobKindOracle(t *testing.T, m *tabletext.Model, c *cppBlobKindCase) {
	t.Helper()
	st := m.Lookup(c.root)
	if st == nil {
		t.Fatalf("%s: no root %s", c.name, c.root)
	}
	for i, d := range cppBlobKindDecoders {
		inst := m.New(st)
		var report tabletext.Report
		ok, err := d.read(m, inst, c.wire, &report)
		verdict, reason, unknown, set := "refused", tablewire.RefusalReason(err), 0, false
		if ok && err == nil && !report.Refused {
			verdict, reason, unknown = "read", "", report.Unknown
			fv, found := inst.FieldByKey(c.field)
			if !found {
				t.Fatalf("%s: %s has no field %s", c.name, c.root, c.field)
			}
			set = fv.Cell.Blob != nil || fv.Cell.Node != nil
		} else if reason == "" {
			t.Fatalf("%s (%s): the engine failed without a refusal: %v %+v", c.name, d.name, err, report)
		}
		if i == 0 {
			c.verdict, c.reason, c.unknown, c.set = verdict, reason, unknown, set
		} else if verdict != c.verdict || reason != c.reason || unknown != c.unknown || set != c.set {
			t.Fatalf("%s: the engine's reads disagree: %s %s %d %v against %s %s %d %v", c.name, verdict, reason, unknown, set, c.verdict, c.reason, c.unknown, c.set)
		}
	}
}

// cppBlobKindReads is the C++ (statements inside main) that reads every case,
// its wire a file in dir named <case>.bin, through every read path and prints
// one ROW line per path:
//
//	ROW <case> <path> read <unknown|-> <set|null>
//	ROW <case> <path> refused <reason>
func cppBlobKindReads(dir string, cases []cppBlobKindCase) string {
	var b strings.Builder
	b.WriteString(`
    alignas( 64 ) static uint8_t region[ 1 << 16 ];
    alignas( 64 ) static uint8_t retain_region[ 1 << 16 ];
    static uint8_t retain_bytes[ 1 << 12 ];
    static TableRetain::Id retain_ids[ 64 ];
    auto reason_name = []( int reason ) -> const char *
    {
        switch ( reason )
        {
            case wire_damaged: return "wire_damaged";
            case kind_mismatch: return "kind_mismatch";
            case ok: return "ok";
            default: return "other";
        }
    };
    auto row = [&]( const char * name, const char * path, bool read, const TableReport & report, bool set )
    {
        if ( read ) { printf( "ROW %s %s read %d %s\n", name, path, (int) report.unknown, set ? "set" : "null" ); }
        else { printf( "ROW %s %s refused %s\n", name, path, reason_name( (int) report.refuse_reason ) ); }
    };
`)
	for _, c := range cases {
		fmt.Fprintf(&b, `
    {
        const char * name = %[1]q;
        std::vector<uint8_t> wire;
        if ( !slurp( %[4]q, wire ) ) { printf( "FAIL %%s: cannot read the vector\n", name ); return 1; }
        const int64_t bytes = (int64_t) wire.size();
        {
            memset( region, 0, sizeof( region ) );
            TableReport report;
            const %[2]s * root = %[2]sLoad( region, (int64_t) sizeof( region ), wire.data(), bytes, &report );
            row( name, "load", root != NULL, report, root != NULL && root->%[3]s.value != 0 );
        }
        {
            memset( retain_region, 0, sizeof( retain_region ) );
            TableRetain retain;
            retain.bytes = retain_bytes; retain.capacity = (int64_t) sizeof( retain_bytes );
            retain.ids = retain_ids; retain.id_capacity = 64;
            TableReport report;
            const %[2]s * root = %[2]sLoadRetain( retain_region, (int64_t) sizeof( retain_region ), wire.data(), bytes, &retain, &report );
            row( name, "retain", root != NULL, report, root != NULL && root->%[3]s.value != 0 );
        }
        {
            %[2]sBuilder builder;
            TableReport report;
            const bool built = %[2]sLoadBuilder( builder, wire.data(), bytes, &report );
            row( name, "builder", built, report, built && builder.GetRoot()->%[3]s.value != 0 );
        }
        {
            TableRefuseReason why = ok;
            const %[2]s * root = %[2]sReadStrict( wire.data(), bytes, &why );
            if ( root != NULL ) { printf( "ROW %%s strict read - %%s\n", name, root->%[3]s.value != 0 ? "set" : "null" ); }
            else { printf( "ROW %%s strict refused %%s\n", name, reason_name( (int) why ) ); }
            if ( root != NULL ) { table_release( TableDefaultAllocator(), (void *) root ); }
        }
        {
            int64_t attribution = 0;
            TableRefuseReason why = ok;
            const int64_t need = %[2]sLoadMeasure( wire.data(), bytes, &attribution, &why );
            printf( "ROW %%s measure %%s\n", name, need >= 0 ? "read" : "refused" );
        }
    }
`, c.name, c.root, c.field, filepath.Join(dir, c.name+".bin"))
	}
	return b.String()
}

// cppBlobKindSaves is the C++ writer half (statements inside main, after
// cppBlobKindReads): for each root, its blob null (0), empty (1) and set to
// "hello" (2), the builder save, the region save of that wire read back, and
// the retaining save of it read back retained, each into
// <dir>/save-<root>-<which>-<path>.bin.
func cppBlobKindSaves(dir string, roots map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
    auto spill = []( const char * root, int which, const char * path, const uint8_t * data, int64_t size ) -> bool
    {
        char file[ 4096 ];
        snprintf( file, sizeof( file ), "%%s/save-%%s-%%d-%%s.bin", %q, root, which, path );
        FILE * f = fopen( file, "wb" );
        if ( f == NULL ) { return false; }
        const bool good = size == 0 || fwrite( data, 1, (size_t) size, f ) == (size_t) size;
        return fclose( f ) == 0 && good;
    };
`, dir)
	for _, root := range slices.Sorted(maps.Keys(roots)) {
		field := roots[root]
		fmt.Fprintf(&b, `
    for ( int which = 0; which <= 2; which++ )
    {
        static uint8_t out[ 4096 ], again[ 4096 ];
        const int64_t length = which == 2 ? 5 : 0;
        int64_t written = 0;
        {
            %[1]sBuilder builder;
            %[1]s * root = builder.GetRoot();
            if ( root == NULL ) { printf( "FAIL %[1]s: builder root\n" ); return 1; }
            if ( which > 0 )
            {
                %[3]s
            }
            written = %[1]sSave( builder, out, (int64_t) sizeof( out ) );
        }
        if ( written < 0 || !spill( %[1]q, which, "builder", out, written ) ) { printf( "FAIL %[1]s %%d: builder save\n", which ); return 1; }
        {
            memset( region, 0, sizeof( region ) );
            TableReport report;
            const %[1]s * loaded = %[1]sLoad( region, (int64_t) sizeof( region ), out, written, &report );
            if ( loaded == NULL ) { printf( "FAIL %[1]s %%d: region read of the builder save\n", which ); return 1; }
            const int64_t rewritten = %[1]sSave( loaded, again, (int64_t) sizeof( again ) );
            if ( rewritten < 0 || !spill( %[1]q, which, "region", again, rewritten ) ) { printf( "FAIL %[1]s %%d: region save\n", which ); return 1; }
        }
        {
            memset( retain_region, 0, sizeof( retain_region ) );
            TableRetain retain;
            retain.bytes = retain_bytes; retain.capacity = (int64_t) sizeof( retain_bytes );
            retain.ids = retain_ids; retain.id_capacity = 64;
            TableReport report;
            const %[1]s * loaded = %[1]sLoadRetain( retain_region, (int64_t) sizeof( retain_region ), out, written, &retain, &report );
            if ( loaded == NULL ) { printf( "FAIL %[1]s %%d: retaining read of the builder save\n", which ); return 1; }
            TableReport saving;
            const int64_t rewritten = %[1]sSaveRetain( loaded, &retain, again, (int64_t) sizeof( again ), &saving );
            if ( rewritten < 0 || !spill( %[1]q, which, "retain", again, rewritten ) ) { printf( "FAIL %[1]s %%d: retaining save\n", which ); return 1; }
        }
    }
`, root, field, cppBlobKindEmplace(field))
	}
	return b.String()
}

// cppBlobKindEmplace places a blob of `length` bytes ("hello" or empty) in the
// root's field: a *string through the string emplace, a *bytes through the
// bytes emplace. The reader unit's Text.body is the one, Bin.data the other.
func cppBlobKindEmplace(field string) string {
	if field == "body" {
		return `if ( TableStringEmplace( builder.main, root->body, "hello", length ) == NULL && length > 0 ) { printf( "FAIL string emplace\n" ); return 1; }`
	}
	return `uint8_t * data = TableBytesEmplace( builder.main, root->` + field + `, length );
                if ( length > 0 ) { if ( data == NULL ) { printf( "FAIL bytes emplace\n" ); return 1; } memcpy( data, "hello", (size_t) length ); }`
}

// cppBlobKindProblems names every C++ answer that is not the engine's.
func cppBlobKindProblems(cases []cppBlobKindCase, output string) []string {
	got := map[string]string{}
	for line := range strings.Lines(output) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ROW "); ok {
			name, tail, _ := strings.Cut(rest, " ")
			path, answer, _ := strings.Cut(tail, " ")
			got[name+" "+path] = answer
		}
	}
	var problems []string
	for _, c := range cases {
		for _, path := range cppBlobKindPaths {
			answer, ok := got[c.name+" "+path]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s (%s): no answer", c.name, path))
				continue
			}
			pointer := map[bool]string{true: "set", false: "null"}[c.set]
			var bad bool
			switch {
			case path == "measure":
				bad = answer != c.verdict
			case c.verdict == "refused":
				bad = !strings.HasPrefix(answer, "refused ")
			case path == "strict":
				bad = answer != "read - "+pointer
			default:
				bad = answer != "read "+strconv.Itoa(c.unknown)+" "+pointer
			}
			if bad {
				problems = append(problems, fmt.Sprintf("%s (%s): C++ answers %q, the engine %s %s unknown %d %s", c.name, path, answer, c.verdict, c.reason, c.unknown, pointer))
			}
		}
	}
	return problems
}

// cppBlobKindWriterProblems decodes a saved wire with the engine at `root` and
// names each read that is not accepted with nothing unknown and the field's
// blob as saved (nil for null).
func cppBlobKindWriterProblems(m *tabletext.Model, root, field string, wire, want []byte) []string {
	var problems []string
	for _, d := range cppBlobKindDecoders {
		inst := m.New(m.Lookup(root))
		var report tabletext.Report
		ok, err := d.read(m, inst, wire, &report)
		if !ok || err != nil || report.Refused || report.Unknown != 0 {
			problems = append(problems, fmt.Sprintf("(%s) %v %+v", d.name, err, report))
			continue
		}
		fv, _ := inst.FieldByKey(field)
		if blob := fv.Cell.Blob; (blob == nil) != (want == nil) || (blob != nil && !bytes.Equal(blob.Data, want)) {
			problems = append(problems, fmt.Sprintf("(%s) the field reads %v, want %q", d.name, blob, want))
		}
	}
	return problems
}

// cppBlobKindPlantRootClosure plants the old rule in a copy of the generated
// reader: a blob kind this root's pointer closure lacks is a type the reader
// cannot name, so Text's NodeDeclared forgets the bytes id and Bin's the
// string id.
func cppBlobKindPlantRootClosure(t *testing.T) func(map[string][]byte) {
	return func(files map[string][]byte) {
		both := "        case kTableBytesTypeId:\n        case kTableStringTypeId:\n"
		for root, keep := range map[string]string{"Text": "        case kTableStringTypeId:\n", "Bin": "        case kTableBytesTypeId:\n"} {
			header := "inline bool " + root + "NodeDeclared( uint64_t type_id )\n{\n"
			planted := 0
			for name, data := range files {
				src := string(data)
				at := strings.Index(src, header)
				if at < 0 {
					continue
				}
				end := at + strings.Index(src[at:], "return true;")
				body := strings.Replace(src[at:end], both, keep, 1)
				if body == src[at:end] {
					t.Fatalf("plant: %sNodeDeclared in %s does not name both blob ids", root, name)
				}
				files[name] = []byte(src[:at] + body + src[end:])
				planted++
			}
			if planted != 1 {
				t.Fatalf("plant: %sNodeDeclared found in %d files, want 1", root, planted)
			}
		}
	}
}

// cppBlobKindWrite puts each case's wire in dir as <case>.bin.
func cppBlobKindWrite(t *testing.T, dir string, cases []cppBlobKindCase) {
	t.Helper()
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, c.name+".bin"), c.wire, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCppPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated reader over the shared vectors)")
	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(vectors, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	type row struct {
		name, unit, root, file, verdict string
		unknown                         int
	}
	var rows []row
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("manifest line %q: %d columns, want 6", line, len(f))
		}
		r := row{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if r.verdict != "read" && r.verdict != "refused" {
			t.Fatalf("manifest row %s: verdict %q", r.name, r.verdict)
		}
		if f[5] != "-" {
			if r.unknown, err = strconv.Atoi(f[5]); err != nil {
				t.Fatalf("manifest row %s: unknown %q", r.name, f[5])
			}
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	unit := rows[0].unit
	schemas, err := filepath.Glob(filepath.Join(repo, unit, "*.schema"))
	if err != nil || len(schemas) != 1 {
		t.Fatalf("%s: want one schema file, got %v (%v)", unit, schemas, err)
	}
	src, err := os.ReadFile(schemas[0])
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, string(src))
	m := tabletext.NewModel(u)
	fields := map[string]string{"Text": "body", "Bin": "data"}

	// THE MANIFEST, generically: the engine must answer each row's verdict and
	// unknown count, and the C++ reads must answer the engine.
	var cases []cppBlobKindCase
	for _, r := range rows {
		if r.unit != unit || fields[r.root] == "" {
			t.Fatalf("row %s reads %s at %s; this probe reads %s at Text or Bin", r.name, r.unit, r.root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(vectors, r.file))
		if err != nil {
			t.Fatalf("row %s: %v", r.name, err)
		}
		c := cppBlobKindCase{name: r.name, root: r.root, field: fields[r.root], wire: wire}
		cppBlobKindOracle(t, m, &c)
		if c.verdict != r.verdict || (r.verdict == "read" && r.unknown >= 0 && c.unknown != r.unknown) {
			t.Fatalf("row %s: the engine answers %s unknown %d; MANIFEST.tsv %s unknown %d", r.name, c.verdict, c.unknown, r.verdict, r.unknown)
		}
		cases = append(cases, c)
	}

	t.Run("every row answers the engine", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cppBlobKindWrite(t, dir, cases)
		out, err := cppHeldRun(t, u, cppBlobKindReads(dir, cases)+cppBlobKindSaves(dir, fields))
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, out)
		}
		t.Logf("%s", out)
		for _, p := range cppBlobKindProblems(cases, string(out)) {
			t.Error(p)
		}
		// THE WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: every save
		// reads back in the engine accepted, nothing unknown, the blob as saved.
		for root, field := range fields {
			for which, want := range [][]byte{nil, {}, []byte("hello")} {
				for _, path := range []string{"builder", "region", "retain"} {
					file := fmt.Sprintf("save-%s-%d-%s.bin", root, which, path)
					wire, err := os.ReadFile(filepath.Join(dir, file))
					if err != nil {
						t.Fatalf("%s: %v", file, err)
					}
					for _, p := range cppBlobKindWriterProblems(m, root, field, wire, want) {
						t.Errorf("%s: the C++ save does not read back clean: %s", file, p)
					}
				}
			}
		}
	})
	t.Run("negative control: the writer criterion refuses an orphan and an unknown record", func(t *testing.T) {
		t.Parallel()
		for _, c := range cases {
			if c.name == "text-orphan-bytes" || c.name == "text-unknown-field-bytes" {
				if len(cppBlobKindWriterProblems(m, "Text", "body", c.wire, nil)) != len(cppBlobKindDecoders) {
					t.Errorf("%s: the writer criterion passed a wire a save must never produce", c.name)
				}
			}
		}
	})
	t.Run("negative control: the root's closure decides which blob ids it can name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cppBlobKindWrite(t, dir, cases)
		out, err := cppHeldRunEdited(t, u, cppBlobKindReads(dir, cases), cppBlobKindPlantRootClosure(t))
		if err != nil {
			t.Fatalf("probe: %v\n%s", err, out)
		}
		problems := cppBlobKindProblems(cases, string(out))
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "text-names-bytes (load)") }) {
			t.Errorf("the planted root-closure rule did not go red on text-names-bytes: %q", problems)
		}
	})
	t.Run("feature witnesses: blobs and tables outside the root closure", func(t *testing.T) {
		t.Parallel()
		testCppPointerTargetWitnessesOutsideClosure(t, repo)
	})
}

// testCppPointerTargetWitnessesOutsideClosure holds rows 8, 9 and 10 of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json: a value
// the engine saves at Old and the C++ reads at OnlyB, OnlyString and
// OnlyBytes, where the slot's target kind is outside the root's closure. Each
// refuses.
func testCppPointerTargetWitnessesOutsideClosure(t *testing.T, repo string) {
	dir := filepath.Join(repo, "testdata", "conformance", "feature-witnesses", "pointer-targets")
	src, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
		Unknown              int
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	u := loadUnitFromSource(t, string(src))
	m := tabletext.NewModel(u)
	fields := map[string]string{"OnlyB": "left", "OnlyString": "payload", "OnlyBytes": "text"}
	want := []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}
	if len(rows) < 10 {
		t.Fatalf("cases.json holds %d rows, want rows 8 to 10", len(rows))
	}
	var cases []cppBlobKindCase
	for i, r := range rows[7:10] {
		if r.Name != want[i] || fields[r.Reader] == "" {
			t.Fatalf("cases.json row %d is %q read at %s, want %q", i+8, r.Name, r.Reader, want[i])
		}
		v := m.New(m.Lookup(r.Writer))
		var report tabletext.Report
		if !m.Read(v, r.Input, &report) || !report.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", r.Name, report)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", r.Name, err)
		}
		c := cppBlobKindCase{name: strings.ReplaceAll(r.Name, " ", "-"), root: r.Reader, field: fields[r.Reader], wire: wire}
		cppBlobKindOracle(t, m, &c)
		if (c.verdict == "refused") != (r.KindMismatch > 0) || (c.verdict == "read" && c.unknown != r.Unknown) {
			t.Fatalf("%s: the engine answers %s unknown %d; cases.json kind_mismatch %d unknown %d", r.Name, c.verdict, c.unknown, r.KindMismatch, r.Unknown)
		}
		cases = append(cases, c)
	}
	wires := t.TempDir()
	cppBlobKindWrite(t, wires, cases)
	out, err := cppHeldRun(t, u, cppBlobKindReads(wires, cases))
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	t.Logf("%s", out)
	for _, p := range cppBlobKindProblems(cases, string(out)) {
		t.Error(p)
	}
}
