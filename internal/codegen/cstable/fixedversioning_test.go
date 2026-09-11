package cstable

// THE VERSIONING FIXTURES ON THE C# LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// one definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every old
//	                 value lands exactly, the reader's tail is its declared
//	                 default, and the counters are §5.4's — for an APPEND all of
//	                 them zero, because an append the reader knows is not an
//	                 event.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 `layout_newer` BEFORE any record, reporting THE FILE'S HASH
//	                 and nothing else, with no counter moved and `Malformed`
//	                 false (§5.3's joint answer), and the destination still every
//	                 field of a fresh value.
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`, plus the names
// only the dump states — `old_/mid_/new_floor.bin` and
// `old_/a_/b_/new_lineage_merge.bin` (§5.9 #28). The two schemas of a row are
// `test/tables/VOLD_<row>.schema` and `VNEW_<row>.schema`: ONE table name in two
// packages, because the two layouts are ONE LINEAGE.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)): here
// the test plays the lock, handing the newer unit the older unit's locked entry
// — the wire hash, the layout bytes verbatim and the record size. A reader NEVER
// parses the layout a file carries; it matches the header's hash against the
// lineage and compares the bytes it already holds.
//
// ONE ASSEMBLY, ONE BUILD, ONE RUN — and this is where the C# leg departs from
// the C and Rust ones on purpose. §5.9 #18 makes the COLUMN the unit because
// "two generations of ONE table name have no spelling inside one translation
// unit in any language". C# has namespaces, and the generator already derives
// the namespace from the schema's package, which every VOLD_/VNEW_ pair already
// differs in — so both generations DO have a spelling here, every probe is one
// class in its own namespace, and the whole suite is a single `dotnet build` and
// a single run. Fifty-seven `dotnet run`s would cost minutes where this costs
// one build. What §5.9 #18 actually owes is the SHAPE — one generated probe per
// row per COLUMN, never hand-written — and that is what this keeps. The one case
// the namespace does not solve is a reader unit generated TWICE with different
// lineages (the three floor rows): there the schema's own `package` line is
// rewritten per probe, which is the unit's name and not the generated code.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type csVersionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan and the file READS in
	// BOTH directions — which is the test that the hash, and only the hash, is
	// the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes every counter at zero (§5.9 #10).
	widens bool
	// widened is THE EXACT NUMBER the row owes, where the reference states one
	// and the fold could hide it. §5.4's "once per entry" is once per ELEMENT
	// across a widen: the C++ reference's EMIT has no fold there and emits one
	// entry per element, so array_elem_widen's four widened elements are four.
	// This leg DOES fold the run into one entry for the copy loop, so `> 0`
	// would pass whether it reported 1 or 4 and the fold would be free to move
	// the number. The figure is asserted instead, and it is the reference's.
	widened int
	// check is C# source asserting the landed values, over `back`.
	check string
}

var csVersionRows = []csVersionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true, widened: 4},
	{row: "array_fixed_grow"},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
            if (back[0].X != 11 || back[0].Y != 22 || back[0].Z != 33)
            {
                bad += ProbeLog.Fail(@NAME@, "the old writer's values did not land: " + back[0].X + " " + back[0].Y + " " + back[0].Z);
            }
            if (back[0].W != 77)
            {
                bad += ProbeLog.Fail(@NAME@, "the appended field is not its declared default: " + back[0].W);
            }`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- the one test -----------------------------------------------------------
//
// Every probe is generated, built and run ONCE, and the Go subtests read that
// one run's output. A probe that does not COMPILE reddens the whole suite with
// the compiler's own words, which is the cost this shape pays for the build it
// saves — it is named here rather than discovered.

func TestFixedVersioning(t *testing.T) {
	out, err := csVersionRun(t)
	for _, r := range csVersionRows {
		t.Run(r.row+"/new_reads_old", func(t *testing.T) {
			csVersionAssert(t, out, err, r.row+"/new_reads_old")
		})
		t.Run(r.row+"/old_refuses_new", func(t *testing.T) {
			csVersionAssert(t, out, err, r.row+"/old_refuses_new")
		})
	}
	for _, name := range []string{
		"floor_at", "floor_below", "floor_raise_live",
		"hash_unknown", "hash_known_bytes_differ", "hash_identity",
		"lineage_merge",
	} {
		t.Run(name, func(t *testing.T) { csVersionAssert(t, out, err, name) })
	}
}

// csVersionAssert reads ONE probe's verdict out of the single run's output. A
// probe that did not print at all DID NOT RUN — the project did not build, or
// the name moved — and that is a failure and never a pass.
func csVersionAssert(t *testing.T, out []byte, runErr error, probe string) {
	t.Helper()
	text := string(out)
	if strings.Contains(text, "ROW "+probe+": ok") {
		return
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, probe) {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s DID NOT RUN: the probe assembly did not build, or the name moved\n%s",
			probe, csVersionTail(text, runErr))
	}
	t.Fatalf("%s:\n%s", probe, strings.Join(lines, "\n"))
}

func csVersionTail(text string, runErr error) string {
	const cap = 4000
	if len(text) > cap {
		text = text[len(text)-cap:]
	}
	if runErr != nil {
		return text + "\n" + runErr.Error()
	}
	return text
}

// ---- the probe project ------------------------------------------------------

var (
	csVersionOnce sync.Once
	csVersionOut  []byte
	csVersionErr  error
)

func csVersionRun(t *testing.T) ([]byte, error) {
	t.Helper()
	corpus := csFixedCorpus(t)
	csVersionOnce.Do(func() { csVersionOut, csVersionErr = csVersionBuildAndRun(t, corpus) })
	return csVersionOut, csVersionErr
}

// csVersionProbe is one generated probe: the READER's unit, the OLDER units
// whose locked entries are its lineage, how many of those are retired, and the
// C# body that reads one corpus file.
type csVersionProbe struct {
	name   string   // the probe's own name, what ROW <name> prints
	reader string   // the reader schema's basename under test/tables
	older  []string // the lineage, OLDEST FIRST
	retire int      // the first `retire` entries are marked retired, which moves the floor
	suffix string   // a package suffix, for a reader unit generated more than once
	body   string   // the C# body, with NAME/TABLE/FILE substituted
	file   string   // the corpus file this probe reads
}

func csVersionBuildAndRun(t *testing.T, corpus string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	var probes []csVersionProbe
	for _, r := range csVersionRows {
		probes = append(probes,
			csVersionProbe{
				name:   r.row + "/new_reads_old",
				reader: "VNEW_" + r.row,
				older:  []string{"VOLD_" + r.row},
				file:   "old_" + r.row + ".bin",
				body:   csVersionNewReadsOld(r),
			},
			csVersionProbe{
				name:   r.row + "/old_refuses_new",
				reader: "VOLD_" + r.row,
				file:   "new_" + r.row + ".bin",
				body:   csVersionOldRefusesNew(r),
			})
	}
	// THE FLOOR owes three (§5.7): a file AT the floor reads, a file ONE BELOW
	// refuses `layout_unsupported`, and the floor raised by one makes
	// yesterday's file refuse today. One reader, three retire levels — so the
	// same unit is generated three times and the package is what separates them.
	floor := []string{"VOLD_floor", "VMID_floor"}
	probes = append(probes,
		csVersionProbe{name: "floor_at", reader: "VNEW_floor", older: floor, suffix: "r0",
			file: "old_floor.bin", body: csVersionFloorReads("floor_at")},
		csVersionProbe{name: "floor_below", reader: "VNEW_floor", older: floor, retire: 1, suffix: "r1",
			file: "old_floor.bin", body: csVersionFloorRefuses("floor_below")},
		csVersionProbe{name: "floor_raise_live", reader: "VNEW_floor", older: floor, retire: 2, suffix: "r2",
			file: "mid_floor.bin", body: csVersionFloorRefuses("floor_raise_live")},
		csVersionProbe{name: "hash_unknown", reader: "VNEW_field_append", older: []string{"VOLD_field_append"},
			suffix: "hu", file: "new_field_append.bin", body: csVersionHashUnknown()},
		csVersionProbe{name: "hash_known_bytes_differ", reader: "VNEW_field_append", older: []string{"VOLD_field_append"},
			suffix: "hb", file: "new_field_append.bin", body: csVersionHashBytesDiffer()},
		csVersionProbe{name: "hash_identity", reader: "VNEW_field_append", older: []string{"VOLD_field_append"},
			suffix: "hi", file: "new_field_append.bin", body: csVersionHashIdentity()},
		csVersionProbe{name: "lineage_merge", reader: "VNEW_lineage_merge",
			older: []string{"VOLD_lineage_merge", "VBRA_lineage_merge", "VBRB_lineage_merge"},
			file:  "a_lineage_merge.bin", body: csVersionLineageMerge()},
	)

	var calls []string
	for i, p := range probes {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		joined := ""
		for _, data := range files {
			joined += string(data)
		}
		probeClass := fmt.Sprintf("Probe%03d", i)
		// TWO PASSES, and the order matters: the value-comparison block is itself
		// written in the probe's placeholders, so it is spliced in FIRST and then
		// substituted with everything else.
		body := strings.ReplaceAll(p.body, "@JSON@", csVersionJSON(joined, table))
		body = strings.NewReplacer(
			"@NAME@", fmt.Sprintf("%q", p.name),
			"@TABLE@", table,
			"@FILE@", fmt.Sprintf("%q", p.file),
		).Replace(body)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's versioning harness: one probe per row per COLUMN
using System;

namespace %s
{
    static class %s
    {
        public static int Run(string corpus)
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, ns, probeClass, body, fmt.Sprintf("%q", p.name))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run(corpus);", ns, probeClass))
	}

	main := fmt.Sprintf(`using System;

// ProbeLog is the one thing every namespace shares: the failure line, and the
// count. It lives in the global namespace so a probe in any unit's namespace can
// reach it unqualified by its type name.
static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        string corpus = Environment.GetEnvironmentVariable("SCHEMA_FIXEDFORM_CORPUS");
        int bad = 0;
        try
        {
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs versioning: every row green" : "cs versioning: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	proj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csversioning</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime, runtime)
	if err := os.WriteFile(filepath.Join(dir, "csversioning.csproj"), []byte(proj), 0o600); err != nil {
		t.Fatal(err)
	}

	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	return cmd.CombinedOutput()
}

// csVersionGenerate generates the READER's unit with the OLDER units' locked
// entries handed to it as its lineage — oldest first, the reader's own layout
// last — and marks the first `retire` entries retired, which is what moves the
// floor. It returns the namespace, the root table's name and the files.
func csVersionGenerate(t *testing.T, p csVersionProbe) (string, string, map[string][]byte) {
	t.Helper()
	reader := csVersionSuffix(csReadSchema(t, p.reader), p.suffix)
	u := csUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, base := range p.older {
		o := csUnitOf(t, csReadSchema(t, base))
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < p.retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return capitalize(u.Package), csFixedRootName(t, u), files
}

var csVersionPackage = regexp.MustCompile(`(?m)^package\s+(\w+)`)

// csVersionSuffix renames a unit's PACKAGE, which is the only thing that makes
// one schema generated twice two C# namespaces. It edits the schema's own text
// and never the generated code.
func csVersionSuffix(src, suffix string) string {
	if suffix == "" {
		return src
	}
	return csVersionPackage.ReplaceAllString(src, "package ${1}_"+suffix)
}

func csVersionWrite(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// csVersionJSON is the OLD-REFUSES-NEW column's value comparison, and it is the
// leg's answer to §5.9 #17. That ruling says the column compares VALUES and
// never a struct's slack, and offers the byte view where a generated value
// derives no equality — which is the one option a C# backend does not have: its
// storage is a sealed CLASS with no byte view and no derived equality. The
// conforming third answer is the unit's own value surface, the JSON the same
// emitter writes, which is field by field and carries no slack at all. Where a
// unit has no such surface the clause is omitted and the row still asserts the
// refusal, the hash, the counters and the verdict.
func csVersionJSON(generated, table string) string {
	if !strings.Contains(generated, table+"ToJsonMeasure(") {
		return `            // no value surface on this unit: the destination comparison is omitted (§5.9 #17)`
	}
	return fmt.Sprintf(`            {
                byte[] a = new byte[Schema.%[1]sToJsonMeasure(back[0])];
                byte[] b = new byte[Schema.%[1]sToJsonMeasure(fresh)];
                long na = Schema.%[1]sToJson(back[0], a);
                long nb = Schema.%[1]sToJson(fresh, b);
                if (na != nb || !a.AsSpan(0, (int)na).SequenceEqual(b.AsSpan(0, (int)nb)))
                {
                    bad += ProbeLog.Fail(@NAME@, "REFUSE wrote the destination: " + System.Text.Encoding.UTF8.GetString(a, 0, (int)na));
                }
            }`, table)
}

// ---- the probe bodies, one per column ---------------------------------------

const csVersionClean = `            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0
                || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "counters moved on a clean backward read: unknown=" + r.Unknown
                    + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate
                    + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`

const csVersionHead = `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            ulong want = System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(data.AsSpan(8));
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
`

func csVersionNewReadsOld(r csVersionRow) string {
	counters := `            if (r.Widened != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "widened " + r.Widened + ", and an append the reader knows is not an event");
            }`
	if r.widens {
		counters = `            if (r.Widened == 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a widening row moved no widened counter");
            }`
	}
	if r.widened != 0 {
		counters = fmt.Sprintf(`            if (r.Widened != %d)
            {
                bad += ProbeLog.Fail(@NAME@, "widened " + r.Widened + ", and the reference counts one per ELEMENT widened: %d");
            }`, r.widened, r.widened)
	}
	return csVersionHead + `            if (n < 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the newer reader refused the older writer's file: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean NEW-READS-OLD is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            _ = want;
` + csVersionClean + "\n" + counters + "\n" + r.check
}

func csVersionOldRefusesNew(r csVersionRow) string {
	if r.sameHash {
		// A row whose edit moves no layout byte and no digest byte has NO second
		// column: the hashes are equal and the file READS in both directions
		// (§5.7) — which is the test that the hash, and only the hash, is the
		// version.
		return csVersionHead + `            if (n < 1)
            {
                bad += ProbeLog.Fail(@NAME@, "a row that moved no layout byte must READ in both directions: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "an equal hash is not a version: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            _ = want;
` + csVersionClean
	}
	return `            @TABLE@ fresh = new @TABLE@();
` + csVersionHead + `            if (n != -1)
            {
                bad += ProbeLog.Fail(@NAME@, "the older reader did not refuse the newer writer's file: n=" + n);
            }
            if (r.Reason != "layout_newer")
            {
                bad += ProbeLog.Fail(@NAME@, "a hash in no lineage entry owes layout_newer, not " + r.Reason);
            }
            if (r.LayoutHash != want)
            {
                bad += ProbeLog.Fail(@NAME@, "layout_newer reports THE FILE'S hash: 0x" + want.ToString("x16") + ", not 0x" + r.LayoutHash.ToString("x16"));
            }
            if (r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "a refusal by name never sets malformed too (§5.3, the joint answer)");
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "REFUSE is total: no counter moves");
            }
@JSON@`
}

func csVersionFloorReads(name string) string {
	return csVersionHead + `            if (n < 1)
            {
                bad += ProbeLog.Fail(@NAME@, "a file AT the floor reads: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "a file at the floor is not a refusal: reason=" + r.Reason);
            }
            _ = want;`
}

func csVersionFloorRefuses(name string) string {
	return csVersionHead + `            if (n != -1)
            {
                bad += ProbeLog.Fail(@NAME@, "a file below the floor must refuse: n=" + n);
            }
            if (r.Reason != "layout_unsupported")
            {
                bad += ProbeLog.Fail(@NAME@, "a hash below the floor owes layout_unsupported, not " + r.Reason);
            }
            if (r.LayoutHash != want)
            {
                bad += ProbeLog.Fail(@NAME@, "layout_unsupported reports the file's hash too (§5.9 #7)");
            }
            if (r.Malformed || r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "REFUSE is total");
            }`
}

func csVersionHashUnknown() string {
	return `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            ulong want = 0xDEADBEEFCAFEF00DUL;
            System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(data.AsSpan(8), want);
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != -1 || r.Reason != "layout_newer")
            {
                bad += ProbeLog.Fail(@NAME@, "a hash in no lineage entry owes layout_newer: n=" + n + " reason=" + r.Reason);
            }
            if (r.LayoutHash != want)
            {
                bad += ProbeLog.Fail(@NAME@, "layout_newer reports THE FILE'S hash and nothing else");
            }
            if (r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "a refusal by name never sets malformed too");
            }`
}

func csVersionHashBytesDiffer() string {
	return `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            data[Schema.TableFixedWire.HeaderBytes + 4] ^= 0xFF;
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != -1 || r.Reason != "layout_malformed")
            {
                bad += ProbeLog.Fail(@NAME@, "a KNOWN hash whose layout bytes differ is one name, layout_malformed: n=" + n + " reason=" + r.Reason);
            }
            if (r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "a refusal by name never sets malformed too");
            }`
}

func csVersionHashIdentity() string {
	return csVersionHead + `            if (n < 1 || r.Refused || r.Malformed)
            {
                bad += ProbeLog.Fail(@NAME@, "the reader's OWN hash selects the identity plan: n=" + n + " reason=" + r.Reason);
            }
            if (r.LayoutHash != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "layout_hash is zero on every path but the two layout refusals (§5.9 #15)");
            }
            _ = want;
` + csVersionClean
}

func csVersionLineageMerge() string {
	return `            foreach (string one in new string[] { "a_lineage_merge.bin", "b_lineage_merge.bin" })
            {
                byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, one));
                @TABLE@[] back = new @TABLE@[8];
                for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                if (n < 1 || r.Refused || r.Malformed)
                {
                    bad += ProbeLog.Fail(@NAME@, one + " does not read on the merged build: n=" + n + " reason=" + r.Reason);
                }
            }`
}

// ---- the corpus, the schemas, the root --------------------------------------

func csFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; `make tables-cs-versioning` builds the corpus first
		// and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a missing file
		// means the build did not do what the target says it did — and a suite
		// that skips itself there would report green over §5 having never run.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

func csReadSchema(t *testing.T, base string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", base+".schema"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func csUnitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// csFixedRootName is §5.9 #9's answer: EVERY declared fixed table is a root, and
// the file's root is the one no other fixed table names BY VALUE — the outer
// table, which is the root the dump wrote every corpus row from.
func csFixedRootName(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	return roots[len(roots)-1].Name
}

var _ = binary.LittleEndian
