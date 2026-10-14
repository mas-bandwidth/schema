package cstable

// TestFixedRoadmapCSharpT14CompressedFloats is cs/R30. The fixed write stores
// the float32 itself (docs/SPEC.md Floating-point discipline,
// docs/FIXED-FORM-ALGORITHM.md §5.1, docs/SPEC-TABLES.md §3.4). The message
// dequantize is a separate multiply then add:
//
//	float scaled = normalized * shape.Delta;
//	bits = TableFloatToBits(scaled + shape.QMin);
//
// The fixture where math.FMA differs is index 2 under min=-1, max=2,
// resolution=0.1 (count=30, delta=3): unfused 0xbf4ccccc, fused 0xbf4ccccd.
// Coarser is cited from the lockfile tests rather than re-proved.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const t14CSOld = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`

const t14CSFine = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.01
    trail uint32 = 2
}
`

const t14CSDropped = `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32
    trail uint32 = 2
}
`

func TestFixedRoadmapCSharpT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("cs/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the C# toolchain (it compiles and runs the generated unit)")
		t14CSCite(t)
		files, err := Generate(unitFrom(t, t14CSOld))
		if err != nil {
			t.Fatal(err)
		}
		var src string
		for name, data := range files {
			if strings.HasSuffix(name, ".cs") {
				src += string(data)
			}
		}
		write := csFn(src, "public static void ReadingFixedWriteBody(")
		if !strings.Contains(write, "BinaryPrimitives.WriteSingleLittleEndian(") {
			t.Fatal("the fixed write does not store the float32")
		}
		if strings.Contains(write, "shape.Steps") || strings.Contains(src, "FusedMultiplyAdd") {
			t.Fatal("the fixed write quantizes, or the dequantize contracts the multiply-add")
		}
		if !strings.Contains(src, "float scaled = normalized * shape.Delta;") || !strings.Contains(src, "TableFloatToBits(scaled + shape.QMin)") {
			t.Fatal("the emitted dequantize does not round the product before the add")
		}
		dir := t.TempDir()
		file := filepath.Join(dir, "old.bin")
		t14CSRun(t, t14CSOld, nil, "write", file)
		t14CSRun(t, t14CSFine, []string{t14CSOld}, "read", file)
		t14CSRun(t, t14CSDropped, []string{t14CSOld}, "read", file)
	})
}

func t14CSCite(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		"func TestLockCFloatResolutionCoarsenedRefuses",
		"func TestLockCFloatResolutionRefinedAllowed",
		"func TestLockCFloatRangeDroppedAllowed",
		"resolution coarsened",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("the lockfile citation is gone: %s", needle)
		}
	}
}

// t14CSDriver is compiled into the generated assembly. ProbeDequantize calls
// the emitted message reader (MessageReadElement, packing 2) so the bits are
// that reader's, not a second copy of the formula. write saves an off-grid
// float32 and reads it back; read is the finer or dropped reader.
const t14CSDriver = `using System;

namespace Cfold
{
    public static partial class Schema
    {
    public static partial class TableWire
    {
        public static uint ProbeDequantize()
        {
            byte[] raw = new byte[] { 2, 0, 0, 0, 0, 0, 0, 0 };
            var reader = new BitReader(raw);
            var shape = new TableMessageShape();
            shape.Packing = 2;
            shape.Bits = 5;
            shape.Steps = 30;
            shape.Delta = 3f;
            shape.QMin = -1f;
            ulong got = 0;
            var field = new TableFieldInfo();
            field.Kind = 10;
            field.SetRaw = (o, i, bits) => { got = bits; };
            var decode = new MessageRead();
            decode.Report = new TableReport();
            if (!MessageReadElement(ref reader, decode, new object(), field, 0, 10, shape, true))
            {
                throw new Exception("dequantize read failed");
            }
            return (uint)got;
        }
    }
    }
}

static class ProbeMain
{
    static int Main(string[] args)
    {
        string mode = args[0];
        string path = args[1];
        if (mode == "write")
        {
            Cfold.Reading value = new Cfold.Reading();
            Cfold.Schema.TableReset(value);
            value.Lead = 1u;
            value.Aim = BitConverter.Int32BitsToSingle(unchecked((int)0x3E99999A));
            value.Trail = 2u;
            byte[] bytes = new byte[(int)Cfold.Schema.ReadingFixedMeasure(1)];
            if (Cfold.Schema.ReadingFixedSave(value, bytes) != bytes.Length)
            {
                Console.WriteLine("FAIL save");
                return 1;
            }
            bool found = false;
            for (int i = 0; i + 4 <= bytes.Length; i++)
            {
                if (bytes[i] == 0x9a && bytes[i + 1] == 0x99 && bytes[i + 2] == 0x99 && bytes[i + 3] == 0x3e) { found = true; break; }
            }
            if (!found) { Console.WriteLine("FAIL float bits missing"); return 1; }
            System.IO.File.WriteAllBytes(path, bytes);
            if (!Round(bytes)) { return 1; }
            uint bits = Cfold.Schema.TableWire.ProbeDequantize();
            if (bits != 0xbf4cccccu)
            {
                Console.WriteLine("FAIL dequantize " + bits.ToString("x8"));
                return 1;
            }
            Console.WriteLine("PROBE OK");
            return 0;
        }
        if (!Round(System.IO.File.ReadAllBytes(path))) { return 1; }
        Console.WriteLine("PROBE OK");
        return 0;
    }

    static bool Round(byte[] bytes)
    {
        Cfold.Reading back = new Cfold.Reading();
        Cfold.Schema.TableReset(back);
        Cfold.TableReport report = new Cfold.TableReport();
        long n = Cfold.Schema.ReadingFixedLoad(back, bytes, new Cfold.TableFixedEntry[8192], report);
        if (n != 1)
        {
            Console.WriteLine("FAIL load " + n + " " + report.Reason);
            return false;
        }
        if (BitConverter.SingleToInt32Bits(back.Aim) != unchecked((int)0x3E99999A) || back.Lead != 1u || back.Trail != 2u)
        {
            Console.WriteLine("FAIL moved " + BitConverter.SingleToInt32Bits(back.Aim).ToString("X8"));
            return false;
        }
        if (report.Clamped != 0 || report.Malformed || report.Refused || (report.Reason != null && report.Reason.Length != 0))
        {
            Console.WriteLine("FAIL report clamped=" + report.Clamped + " reason=" + report.Reason);
            return false;
        }
        return true;
    }
}
`

func t14CSRun(t *testing.T, schema string, older []string, mode, file string) {
	t.Helper()
	u := unitFrom(t, schema)
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitFrom(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatal("no lineage")
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	var files map[string][]byte
	var err error
	if len(older) == 0 {
		files, err = Generate(u)
	} else {
		files, err = GenerateLineage(u, lineage)
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeDriver.cs"), []byte(t14CSDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	sources, err := filepath.Glob(filepath.Join(dir, "*.cs"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("sources: %v", err)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc (%s): %v\n%s", mode, err, out)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app, mode, file)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PROBE OK") || strings.Contains(string(out), "FAIL") {
		t.Fatalf("csharp %s: %v\n%s", mode, err, out)
	}
}
