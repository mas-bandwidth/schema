package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// csNonCanonicalRow is one row of testdata/wire/non-canonical/MANIFEST.tsv.
type csNonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func csNonCanonicalRows(t *testing.T, vector string) []csNonCanonicalRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []csNonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 4", i+1, len(f))
		}
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[3])
		}
		rows = append(rows, csNonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// csNonCanonicalExpected is the canonical file a read row re-saves to: the
// vector itself, or the canonical rewrite the manifest's generator pins beside
// it for a lawful spelling no writer produces (docs/SPEC-TABLES.md §3).
func csNonCanonicalExpected(t *testing.T, vector string, row csNonCanonicalRow) []byte {
	t.Helper()
	path := filepath.Join(vector, row.file)
	if row.form == "variable" {
		rewrite := filepath.Join(vector, "canonical", filepath.Base(row.file))
		if _, err := os.Stat(rewrite); err == nil {
			path = rewrite
		} else if row.file == "variable-fields-out-of-order.bin" {
			path = filepath.Join(vector, "variable-canonical.bin")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// csNodeTableOutOfPlaceFirstUse respells the manifest's out-of-place node
// table vector with its id table in the order a walk first uses each id: the
// root body's references from the node table's on, then the record's type and
// its body's. Only the node table's place is then non-canonical.
func csNodeTableOutOfPlaceFirstUse(t *testing.T, data []byte) []byte {
	t.Helper()
	// the vector's root body: the node table (one Node record, value 3), then
	// count 5, head -> record 1, and pick = leaf { tag 4 }, under references
	// 1..8 of its own id table
	body := []byte{0x01, 0x0c, 0x07, 0x01, 0x02, 0x04, 0x03, 0x06, 0x03, 0x00, 0x04, 0x06, 0x05, 0x05, 0x11, 0x02, 0x06, 0x0f, 0x07, 0x0d, 0x04, 0x08, 0x06, 0x04, 0x00, 0x00}
	const count = 8
	if len(data) != 1+len(body)+count*8+8 || data[0] != 1 || string(data[1:1+len(body)]) != string(body) || data[len(data)-8] != count {
		t.Fatalf("variable-node-table-out-of-place.bin is not the spelling this respelling reads: %x", data)
	}
	ids := data[1+len(body) : len(data)-8]
	// old reference -> new: 1 4 5 6 7 8 2 3 -> 1 2 3 4 5 6 7 8
	out := []byte{0x01, 0x01, 0x0c, 0x07, 0x01, 0x07, 0x04, 0x08, 0x06, 0x03, 0x00, 0x02, 0x06, 0x05, 0x03, 0x11, 0x02, 0x04, 0x0f, 0x05, 0x0d, 0x04, 0x06, 0x06, 0x04, 0x00, 0x00}
	for _, old := range []int{1, 4, 5, 6, 7, 8, 2, 3} {
		out = append(out, ids[(old-1)*8:old*8]...)
	}
	return append(out, data[len(data)-8:]...)
}

// csNonCanonicalUnit is one schema the manifest's rows are read under, and
// the probe calls for its rows.
type csNonCanonicalUnit struct {
	schema, namespace string
	calls             []string
	rows              []string
}

// TestCSharpReadsRefuseNonCanonical holds the C# variable-form and
// message-form reads to docs/SPEC-TABLES.md §3, §3.1, §3.3, §4 and
// docs/rulings.json reads-refuse-non-canonical over every row of
// testdata/wire/non-canonical/MANIFEST.tsv, through every read entry: the
// object load, the load measure, the region load, the retaining load, the
// builder load and the message load. A read row re-saves through each entry's
// writer to the canonical bytes; a refused row is refused by every entry. A
// message row is also read under another build version, where only the union
// field written with arm reference 0 stays refused (§3.3 (c)).
func TestCSharpReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")

	repo := filepath.Join("..", "..", "..")
	vector := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := csNonCanonicalRows(t, vector)
	units := map[string]*csNonCanonicalUnit{}
	unitOf := func(schema, namespace string) *csNonCanonicalUnit {
		u := units[schema]
		if u == nil {
			u = &csNonCanonicalUnit{schema: schema, namespace: namespace}
			units[schema] = u
		}
		return u
	}
	for _, row := range rows {
		data, err := os.ReadFile(filepath.Join(vector, row.file))
		if err != nil {
			t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
		}
		schema := filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema")
		namespace := "Noncanonical"
		announcement := filepath.Join(vector, "announcement.bin")
		table := "Message"
		if strings.HasPrefix(row.file, "../message-one-encoding/") {
			schema = filepath.Join(repo, "test", "message-one-encoding", "One.schema")
			namespace = "Messageone"
			announcement = filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")
			table = "One"
		}
		read := row.verdict == "read"
		var call string
		switch row.form {
		case "variable":
			if namespace != "Noncanonical" {
				t.Fatalf("%s: a variable row outside NonCanonical.schema", row.file)
			}
			call = fmt.Sprintf("VariableRow(%q, %q, %q, %t);", row.file, fmt.Sprintf("%x", data), fmt.Sprintf("%x", csNonCanonicalExpected(t, vector, row)), read)
		case "message":
			ann, err := os.ReadFile(announcement)
			if err != nil {
				t.Fatal(err)
			}
			// §3.3 (c): a union field written with arm reference 0 is refused
			// at every build; the other spellings are judged at the reader's
			// own build version only.
			otherRead := row.spelling != "union-arm-zero"
			call = csNonCanonicalMessageCall(row.file, fmt.Sprintf("%q", fmt.Sprintf("%x", ann)), data, read, otherRead, table)
		case "announcement":
			if !read {
				t.Fatalf("%s: an announcement row must read", row.file)
			}
			call = fmt.Sprintf("AnnouncementRow(%q, %q);", row.file, fmt.Sprintf("%x", data))
		default:
			t.Fatalf("MANIFEST.tsv: %s: unknown form %q", row.file, row.form)
		}
		u := unitOf(schema, namespace)
		u.calls = append(u.calls, call)
		u.rows = append(u.rows, row.file)
	}
	// The manifest's out-of-place node table also breaks first-use order, so
	// one respelling with its ids in the walk's first-use order holds the
	// placement rule apart (§3.1).
	placed, err := os.ReadFile(filepath.Join(vector, "variable-node-table-out-of-place.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitOf(filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema"), "Noncanonical")
	u.calls = append(u.calls, fmt.Sprintf("VariableRow(%q, %q, %q, false);", "out-of-place-first-use", fmt.Sprintf("%x", csNodeTableOutOfPlaceFirstUse(t, placed)), ""))
	u.rows = append(u.rows, "out-of-place-first-use")
	for _, u := range units {
		t.Run(filepath.Base(u.schema), func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(u.schema)
			if err != nil {
				t.Fatal(err)
			}
			variable := ""
			if u.namespace == "Noncanonical" {
				variable = csNonCanonicalVariableSource
			}
			program := strings.NewReplacer("NAMESPACE", u.namespace, "VARIABLE", variable, "CALLS", strings.Join(u.calls, "\n        ")).Replace(csNonCanonicalProbeSource)
			out := csNonCanonicalRun(t, string(src), program)
			var missed []string
			for _, file := range u.rows {
				if !strings.Contains(out, "PASS "+file+"\n") {
					missed = append(missed, file)
				}
			}
			if len(missed) != 0 {
				t.Errorf("%d rows not held (%s):\n%s", len(missed), strings.Join(missed, ", "), out)
			}
		})
	}
	t.Run("union None element", testCSharpUnionNoneElementReads)
	t.Run("undeclared field", testCSharpUndeclaredFieldSameBuild)
}

// testCSharpUndeclaredFieldSameBuild holds §3.3's rule for a field the
// record does not declare: from a newer writer (another build version) it is
// skipped and counted (§4), but under this reader's own build version every
// field the sender's record declares is one this record declares, so the
// batch is refused.
func testCSharpUndeclaredFieldSameBuild(t *testing.T) {
	t.Parallel()
	const reader = `package undeclared

fixed table Root
{
    tick uint8
}
`
	const writer = `package undeclared

fixed table Root
{
    tick  uint8
    extra uint8
}
`
	wu := unitFrom(t, writer)
	m := tabletext.NewModel(wu)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "extra": 5}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	call := fmt.Sprintf("UndeclaredRow(\"undeclared\", %q, %q);", fmt.Sprintf("%x", tablewire.Announce(wu)), fmt.Sprintf("%x", batch))
	program := strings.NewReplacer("NAMESPACE", "Undeclared", "VARIABLE", csNonCanonicalUndeclaredSource, "CALLS", call).Replace(csNonCanonicalProbeSource)
	out := csNonCanonicalRun(t, reader, program)
	if !strings.Contains(out, "PASS undeclared\n") {
		t.Fatalf("a field the record does not declare is not held to the build version:\n%s", out)
	}
}

// csNonCanonicalUndeclaredSource reads a newer writer's batch under the
// writer's announcement as written, where its extra field is skipped and
// counted, and then as if the writer's build version were this reader's own.
const csNonCanonicalUndeclaredSource = `
    static void UndeclaredRow(string name, string announcementHex, string hex)
    {
        Row(name, () =>
        {
            byte[] wire = Convert.FromHexString(hex);
            var v = new TableVocabulary();
            if (Schema.AnnounceRead(v, Convert.FromHexString(announcementHex), new TableReport()) != Schema.TableWire.Verdict.Ok) { return "announcement refused"; }
            var values = new Root[] { new Root() };
            var report = new TableReport();
            if (Schema.RootLoadMessages(values, wire, v, report, out int count) != Schema.TableWire.Verdict.Ok || count != 1 || report.Unknown != 1 || values[0].Tick != 1) { return "other build: load refused"; }
            v.BuildVersion = Schema.TableWire.OwnVocabulary(Schema.Announce()).BuildVersion;
            if (Schema.RootLoadMessages(values, wire, v, new TableReport(), out count) == Schema.TableWire.Verdict.Ok || count != 0) { return "own build: load read"; }
            return null;
        });
    }
`

// testCSharpUnionNoneElementReads is the control for §3.3 (c): the refusal is
// of a union FIELD written with arm reference 0, never of a None element of
// an array of unions, which is positional. The writer's batch carrying one
// reads under this reader's build version and under another, and re-saves to
// the same bytes.
func testCSharpUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	const schema = `package messagenone

union Choice
{
    item uint8
}

fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`
	m := tabletext.NewModel(unitFrom(t, schema))
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	call := csNonCanonicalMessageCall("none-element", "Convert.ToHexString(Schema.Announce())", batch, true, true, "Root")
	program := strings.NewReplacer("NAMESPACE", "Messagenone", "VARIABLE", "", "CALLS", call).Replace(csNonCanonicalProbeSource)
	out := csNonCanonicalRun(t, schema, program)
	if !strings.Contains(out, "PASS none-element\n") {
		t.Fatalf("a None element of an array of unions is not read:\n%s", out)
	}
}

// csNonCanonicalMessageCall is the probe's call for one batch of the fixed
// table `table`, read under the announcement the C# expression `announcement`
// spells.
func csNonCanonicalMessageCall(name, announcement string, batch []byte, read, otherRead bool, table string) string {
	return fmt.Sprintf("MessageRow(%q, %s, %q, %t, %t, (v, b, r) => { var vs = new %s[] { new %s() }; var verdict = Schema.%sLoadMessages(vs, b, v, r, out int n); return (verdict, n, vs); }, vs => { var typed = (%s[])vs; byte[] o = new byte[Schema.%sMeasureMessages(typed)]; return Schema.%sSaveMessages(typed, o) == o.Length ? o : null; });",
		name, announcement, fmt.Sprintf("%x", batch), read, otherRead, table, table, table, table, table, table)
}

// csNonCanonicalRun generates the unit's table and packet sources, compiles
// them with the probe and the packet runtime, runs it and returns its output.
func csNonCanonicalRun(t *testing.T, schema, program string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	if len(u.Enums) != 0 || len(u.Structs) != 0 || len(u.Unions) != 0 {
		packet, err := csharp.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, packet)
		runtimeDir, err := filepath.Abs("../../../../serialize.cs/src")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
			path := filepath.Join(runtimeDir, name)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("C# packet runtime prerequisite: %v", err)
			}
			sources = append(sources, path)
		}
	}
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	main := filepath.Join(dir, "NonCanonicalProbe.cs")
	if err := os.WriteFile(main, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	app := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe", "-nowarn:CS8500", "-langversion:latest", "-out:" + app}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc: %v\n%s", err, out)
	}
	rc := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.CommandContext(ctx, csDotnet(), "exec", app)
	run.Env = csDotnetEnv(dir, dir)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	return string(out)
}

// csNonCanonicalProbeSource runs each row and prints PASS or FAIL with the
// first entry whose verdict or re-save does not hold.
const csNonCanonicalProbeSource = `using System;
using NAMESPACE;

public static unsafe class NonCanonicalProbe
{
    delegate (Schema.TableWire.Verdict, int, object[]) LoadBatch(TableVocabulary v, byte[] bytes, TableReport report);
    delegate byte[] SaveBatch(object[] values);

    static void Row(string name, Func<string> run)
    {
        string why;
        try { why = run(); } catch (Exception e) { why = "threw " + e.GetType().Name + ": " + e.Message; }
        Console.Write(why == null ? "PASS " + name + "\n" : "FAIL " + name + ": " + why + "\n");
    }

    static void AnnouncementRow(string name, string hex)
    {
        Row(name, () =>
        {
            byte[] announcement = Convert.FromHexString(hex);
            if (!Schema.Announce().SequenceEqual(announcement)) { return "announce bytes"; }
            var v = new TableVocabulary();
            if (Schema.AnnounceRead(v, announcement, new TableReport()) != Schema.TableWire.Verdict.Ok || !v.Announced) { return "announce read"; }
            return null;
        });
    }

    // Message runs one batch through the message load twice: under the
    // announcement as written, where the manifest's verdict holds and a read
    // re-saves to the same bytes, and under another build version
    // (docs/SPEC-TABLES.md §3.3), where a field at a default and fields out
    // of order are lawful evolution and read, re-saving to a batch that reads
    // back to the same bytes; a union field written with arm reference 0
    // (§3.3 (c)) is refused at every build.
    static void MessageRow(string name, string announcementHex, string hex, bool read, bool otherRead, LoadBatch load, SaveBatch save)
    {
        Row(name, () =>
        {
            byte[] announcement = Convert.FromHexString(announcementHex);
            byte[] wire = Convert.FromHexString(hex);
            var v = new TableVocabulary();
            if (Schema.AnnounceRead(v, announcement, new TableReport()) != Schema.TableWire.Verdict.Ok) { return "announcement refused"; }
            var (verdict, count, values) = load(v, wire, new TableReport());
            if (read)
            {
                if (verdict != Schema.TableWire.Verdict.Ok || count != 1) { return "load refused: " + verdict; }
                byte[] again = save(values);
                if (again == null || !again.AsSpan().SequenceEqual(wire)) { return "save differs"; }
            }
            else if (verdict == Schema.TableWire.Verdict.Ok || count != 0) { return "load read"; }
            v.BuildVersion ^= 1;
            (verdict, count, values) = load(v, wire, new TableReport());
            if (!otherRead)
            {
                if (verdict == Schema.TableWire.Verdict.Ok || count != 0) { return "other build: load read"; }
                return null;
            }
            if (verdict != Schema.TableWire.Verdict.Ok || count != 1) { return "other build: load refused: " + verdict; }
            byte[] rewrite = save(values);
            if (rewrite == null) { return "other build: save refused"; }
            v.BuildVersion ^= 1;
            (verdict, count, values) = load(v, rewrite, new TableReport());
            if (verdict != Schema.TableWire.Verdict.Ok || count != 1) { return "other build: the rewrite does not read"; }
            byte[] twice = save(values);
            if (twice == null || !twice.AsSpan().SequenceEqual(rewrite)) { return "other build: the rewrite is not canonical"; }
            return null;
        });
    }
VARIABLE
    public static int Main()
    {
        CALLS
        return 0;
    }
}
`

// csNonCanonicalVariableSource runs one file through every variable-form read
// entry of Root: the object load, the load measure, the region load, the
// retaining load and the builder load. A read re-saves through each entry's
// own writer to the canonical bytes; a refusal is every entry's.
const csNonCanonicalVariableSource = `
    static void VariableRow(string name, string hex, string expectedHex, bool read)
    {
        Row(name, () => VariableRun(Convert.FromHexString(hex), Convert.FromHexString(expectedHex), read));
    }

    // VariableRun names every entry that does not hold, not only the first.
    static string VariableRun(byte[] wire, byte[] expected, bool read)
    {
        string bad = null;
        void Verdict(string entry, bool loaded) { if (loaded != read) { bad += (bad == null ? "" : "; ") + entry + (read ? " refused" : " read"); } }
        void Saved(string entry, long n, byte[] o) { if (n != o.Length || !o.AsSpan().SequenceEqual(expected)) { bad += (bad == null ? "" : "; ") + entry + " save differs"; } }
        var value = new Root();
        bool loaded = Schema.RootLoad(value, wire, new TableReport());
        Verdict("object load", loaded);
        if (read && loaded)
        {
            byte[] o = new byte[Schema.RootMeasure(value)];
            Saved("object", Schema.RootSave(value, o), o);
        }
        long total = Schema.RootLoadMeasure(wire);
        Verdict("measure", total >= 0);
        long capacity = total >= 0 ? total : 1 << 20;
        void* memory = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((capacity + 15) & ~15L), 16);
        void* memory2 = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)((capacity + 15) & ~15L), 16);
        try
        {
            RootRow* region = Schema.RootLoad((IntPtr)memory, capacity, wire, new TableReport());
            Verdict("region load", region != null);
            if (read && region != null)
            {
                byte[] o = new byte[Schema.RootMeasure((IntPtr)region)];
                Saved("region", Schema.RootSave((IntPtr)region, o), o);
            }
            TableRetain retain = default;
            RootRow* kept = Schema.RootLoadRetain((IntPtr)memory2, capacity, wire, ref retain, new TableReport());
            Verdict("retain load", kept != null);
            if (read && kept != null)
            {
                byte[] o = new byte[Schema.RootMeasureRetain((IntPtr)kept, ref retain)];
                Saved("retain", Schema.RootSaveRetain((IntPtr)kept, ref retain, o, new TableReport()), o);
            }
        }
        finally
        {
            System.Runtime.InteropServices.NativeMemory.AlignedFree(memory2);
            System.Runtime.InteropServices.NativeMemory.AlignedFree(memory);
        }
        using (var builder = new RootBuilder())
        {
            bool built = Schema.RootLoadBuilder(builder, wire, new TableReport());
            Verdict("builder load", built);
            if (read && built)
            {
                byte[] o = new byte[builder.Measure()];
                Saved("builder", builder.Save(o), o);
            }
        }
        return bad;
    }
`
