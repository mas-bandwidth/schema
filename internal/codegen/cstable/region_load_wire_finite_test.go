package cstable

// A non-finite float anywhere on a wire refuses every read of it, in a field
// the reader declares and in one it does not (docs/SPEC-TABLES.md §3.3): the
// managed load, the native region load and the builder load of one root answer
// the same verdict. The wire is a writer's, whose appended float the reader
// does not declare; only the wire-alone walk sees it.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const regionFiniteReaderSchema = `package probe

table Root
{
    label string(8)
    ratio float32
}
`

const regionFiniteWriterSchema = `package probe

table Root
{
    label string(8)
    ratio float32
    appended float32
}
`

func TestCSharpRegionLoadWireFinite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	finite, nan := regionFiniteWires(t)
	out, err := regionFiniteRun(t, finite, nan, false)
	t.Logf("probe:\n%s", out)
	for _, row := range regionFiniteRows {
		regionFiniteAssert(t, out, err, row)
	}
}

func TestCSharpRegionLoadWireFiniteNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	finite, nan := regionFiniteWires(t)
	clean, err := regionFiniteRun(t, finite, nan, false)
	if err != nil {
		t.Fatalf("unplanted tree:\n%s\n%v", clean, err)
	}
	for _, row := range regionFiniteRows {
		regionFiniteAssert(t, clean, nil, row)
	}
	planted, err := regionFiniteRun(t, finite, nan, true)
	t.Logf("planted probe:\n%s", planted)
	if err == nil || strings.Contains(planted, "ROW region/nan: ok\n") || !strings.Contains(planted, "ROW region/nan: ") {
		t.Fatalf("planted discard did not go red by name:\n%s\n%v", planted, err)
	}
}

var regionFiniteRows = []string{
	"managed/finite",
	"managed/nan",
	"region/finite",
	"region/nan",
	"builder/finite",
	"builder/nan",
}

func regionFiniteWires(t *testing.T) (finite, nan []byte) {
	t.Helper()
	finite = vtCellsEncode(t, regionFiniteWriterSchema, "Root", `{"label":"x","ratio":1.5,"appended":2.25}`)
	from := make([]byte, 4)
	binary.LittleEndian.PutUint32(from, math.Float32bits(2.25))
	to := make([]byte, 4)
	binary.LittleEndian.PutUint32(to, 0x7fc00000)
	if i := bytes.Index(finite, from); i < 0 || bytes.Contains(finite[i+len(from):], from) {
		t.Fatalf("want exactly one appended float % x in % x", from, finite)
	}
	nan = bytes.Replace(finite, from, to, 1)
	return finite, nan
}

func regionFiniteRun(t *testing.T, finite, nan []byte, plant bool) (string, error) {
	t.Helper()
	files := generateFiles(t, "Probe", regionFiniteReaderSchema)
	if plant {
		if err := regionFinitePlantDiscard(files); err != nil {
			t.Fatal(err)
		}
	}
	return vtCellsCompileRun(t, files, regionFiniteProbe(finite, nan))
}

// regionFinitePlantDiscard restores the region load's discarded walk answer in
// a copy of the generated artifact: the non-finite out parameter is dropped and
// the native load sees no refusal to carry.
func regionFinitePlantDiscard(files map[string][]byte) error {
	plants := map[string]string{
		"long total = LoadMeasurePartsWalk(type, bytes, out long dataBytes, out long attributionBytes, out string reason, out bool wireNonFinite);": "long total = LoadMeasureParts(type, bytes, out long dataBytes, out long attributionBytes, out string reason); bool wireNonFinite = false;",
		"long total = LoadMeasurePartsWalk(type, bytes, out long dataBytes, out _, out string reason, out bool wireNonFinite);":                     "long total = LoadMeasureParts(type, bytes, out long dataBytes, out _, out string reason); bool wireNonFinite = false;",
	}
	planted := 0
	for name, data := range files {
		s := string(data)
		for from, to := range plants {
			if strings.Contains(s, from) {
				s = strings.ReplaceAll(s, from, to)
				planted++
			}
		}
		files[name] = []byte(s)
	}
	if planted != len(plants) {
		return fmt.Errorf("plant touched %d sites, want %d", planted, len(plants))
	}
	return nil
}

func regionFiniteAssert(t *testing.T, out string, runErr error, row string) {
	t.Helper()
	if strings.Contains(out, "ROW "+row+": ok\n") {
		return
	}
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "ROW "+row) || strings.HasPrefix(strings.TrimSpace(line), "error") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s DID NOT RUN: the assembly did not build, or the name moved\n%s\n%v", row, out, runErr)
	}
	t.Fatalf("%s:\n%s", row, strings.Join(lines, "\n"))
}

func regionFiniteProbe(finite, nan []byte) string {
	return fmt.Sprintf(regionFiniteProbeSrc, hex.EncodeToString(finite), hex.EncodeToString(nan))
}

const regionFiniteProbeSrc = `using System;
using System.Runtime.InteropServices;
using Probe;

public static class RegionFiniteProbe
{
    static int bad;

    static void Row(string name, bool ok, string why)
    {
        if (ok) { Console.WriteLine("ROW " + name + ": ok"); return; }
        bad++;
        Console.WriteLine("ROW " + name + ": " + why);
    }

    static bool Managed(byte[] wire, out bool refused)
    {
        var value = new Root();
        var report = new TableReport();
        bool ok = Schema.RootLoad(value, wire, report);
        refused = report.Refused;
        return ok;
    }

    static unsafe bool Region(byte[] wire, out bool refused)
    {
        refused = false;
        long total = Schema.RootLoadMeasure(wire, out _, out _, out TableRefuseReason why);
        if (total < 0) { Row("region/nan: measure", false, "measure answered " + why); return false; }
        IntPtr region = (IntPtr)NativeMemory.AlignedAlloc((nuint)(total + 64), 64);
        try
        {
            var report = new TableReport();
            IntPtr root = (IntPtr)Schema.RootLoad(region, total, wire, report);
            refused = report.Refused;
            return root != IntPtr.Zero;
        }
        finally { NativeMemory.AlignedFree((void*)region); }
    }

    static bool Builder(byte[] wire, out bool refused)
    {
        using var builder = new RootBuilder();
        var report = new TableReport();
        bool ok = Schema.RootLoadBuilder(builder, wire, report);
        refused = report.Refused;
        return ok;
    }

    public static int Main(string[] args)
    {
        byte[] finite = Convert.FromHexString(%q);
        byte[] nan = Convert.FromHexString(%q);

        bool fok = Managed(finite, out bool frefused);
        Row("managed/finite", fok && !frefused, "ok=" + fok + " refused=" + frefused);
        bool nok = Managed(nan, out bool nrefused);
        Row("managed/nan", !nok && nrefused, "ok=" + nok + " refused=" + nrefused);

        fok = Region(finite, out frefused);
        Row("region/finite", fok && !frefused, "ok=" + fok + " refused=" + frefused);
        nok = Region(nan, out nrefused);
        Row("region/nan", !nok && nrefused, "ok=" + nok + " refused=" + nrefused);

        fok = Builder(finite, out frefused);
        Row("builder/finite", fok && !frefused, "ok=" + fok + " refused=" + frefused);
        nok = Builder(nan, out nrefused);
        Row("builder/nan", !nok && nrefused, "ok=" + nok + " refused=" + nrefused);

        return bad == 0 ? 0 : 1;
    }
}
`
