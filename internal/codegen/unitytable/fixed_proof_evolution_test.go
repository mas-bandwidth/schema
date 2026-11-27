package unitytable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestUnityFixedProofEvolution(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet and the C++ fixed-form corpus")
	repo := filepath.Join("..", "..", "..")
	fixedproof.RunEvolutionRows(t, repo, unityFixedProofRunner{},
		"fixed-file-roundtrip", "file-envelope", "batch-capacity", "plan-selection",
		"compiled-plans", "definition-hash", "fixed-closure", "retirement",
		"retired-runtime", "optional-values", "field-evolution", "writing",
		"union-guards", "plan-execution", "interoperability")

	t.Run("live-extents", func(t *testing.T) {
		t.Parallel()
		u := fixedproof.ParseCheckUnit(t, "package probe\nenum Small { One, Two }\nfixed table T {\n    arr [..3]Small\n    opt ?Small\n    tail int32 = 77\n}\n")
		runner := unityFixedProofRunner{}
		file, why := runner.SaveCustom(t, u, "T", "live_extents_base")
		if why != "" {
			t.Fatalf("save live_extents_base failed: %s", why)
		}
		eOwn, _ := fixedproof.LineageEntryOf(u, "T")
		lineage := map[string][]fixedproof.LineageEntry{"T": {eOwn}}
		n, r, f, _, counters := runner.LoadLineage(t, u, "T", lineage, file, -1)
		if counters != (fixedproof.LoadCounters{}) {
			t.Errorf("load of T moved counters %s, want u0 k0 c0 w0 d0", counters)
		}
		if n != 1 || f["r0.arr_count"] != "1" || f["r0.arr[0]"] != "1" || f["r0.tail"] != "77" || (f["r0.opt_present"] != "false" && f["r0.opt_present"] != "0") {
			t.Errorf("C14/W4: the clean file: n=%d r=%q fields=%v", n, r, f)
		}
		body := len(file) - 13
		file[body+5] = 99
		file[body+6] = 99
		file[body+8] = 99
		if n, r, _, _, _ := runner.LoadLineage(t, u, "T", lineage, file, -1); n != -1 {
			t.Errorf("C14/W4: a non-zero dead byte read: n=%d r=%q, want a refusal", n, r)
		}
		corpus := filepath.Join(repo, "build", "fixedform-corpus")
		older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_field_append.schema")
		newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_field_append.schema")
		rootName := fixedproof.RootOf(t, older)
		eOld, _ := fixedproof.LineageEntryOf(older, rootName)
		data, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, f, _, _ := runner.LoadLineage(t, newer, rootName, map[string][]fixedproof.LineageEntry{rootName: {eOld}}, data, -1); f["r0.w"] != "77" {
			t.Errorf("W9: the unwritten field: %v", f)
		}
	})
}

var _ fixedproof.EvoRunner = unityFixedProofRunner{}

func (unityFixedProofRunner) CheckLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry) string {
	t.Helper()
	files, err := unityGenerateFiles(u, lineage)
	if err != nil {
		return err.Error()
	}
	ns := unityNamespace(u.Package)
	probe := fmt.Sprintf(`using System;
using %[1]s;

static class Program
{
    static int Main()
    {
        string why = ProbeCommon.CheckLineage(typeof(Schema), %[2]q);
        if (!string.IsNullOrEmpty(why))
        {
            Console.WriteLine("REFUSED " + why);
            return 0;
        }
        Console.WriteLine("OK");
        return 0;
    }
}
`, ns, root)
	out, code, _, runErr := runUnityProbe(t, files, probe, nil)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 0 {
		t.Fatalf("CheckLineage probe exited %d: %s", code, out)
	}
	out = strings.TrimSpace(out)
	if after, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return strings.TrimSpace(after)
	}
	if out == "OK" {
		return ""
	}
	return out
}

func (unityFixedProofRunner) LoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	return unityLoadLineage(t, u, root, lineage, wire, capacity, nil)
}

func unityLoadLineage(t *testing.T, u *ir.Unit, root string, lineage map[string][]fixedproof.LineageEntry, wire []byte, capacity int, mutate func(map[string][]byte)) (int, string, map[string]string, []byte, fixedproof.LoadCounters) {
	t.Helper()
	files, err := unityGenerateFiles(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(files)
	}
	if capacity < 0 {
		capacity = 4096
	}
	ns := unityNamespace(u.Package)
	st := fixedproof.RootStruct(t, u, root)
	var dump strings.Builder
	unityProofDumpFields(&dump, u.Package, st, "rec", "", "                ", 0)

	probe := fmt.Sprintf(`using System;
using System.Globalization;
using System.IO;
using Serialize;
using %[1]s;
using static %[1]s.Schema;

static class Program
{
    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        byte[] data = File.ReadAllBytes("in.bin");
        %[2]s[] r = new %[2]s[%[3]d];
        for (int idx = 0; idx < r.Length; idx++) { r[idx] = Schema.New%[2]s(); }
        TableReport report = new TableReport { Unknown = 99, Widened = 99, Reason = 99, LayoutHash = 99 };
        long count = Schema.%[2]sFixedLoad(r.AsSpan(), data, ref report, 4096);
        Console.WriteLine("COUNTERS " + report.Unknown + " 0 0 " + report.Widened + " 0");
        if (count < 0)
        {
            Console.WriteLine("REFUSED " + TableReport.ReasonName(report.Reason));
            return 0;
        }
        Console.WriteLine("OK " + count);
        for (int i = 0; i < (int)count; i++)
        {
            var rec = r[i];
%[4]s        }
        try
        {
            long need = Schema.%[2]sFixedMeasure(count);
            if (need < 0)
            {
                Console.WriteLine("RESAVE refused");
                return 0;
            }
            byte[] outBuf = new byte[need];
            long written = Schema.%[2]sFixedSave(r.AsSpan(0, (int)count), outBuf);
            if (written != need)
            {
                Console.WriteLine("RESAVE refused");
                return 0;
            }
            File.WriteAllBytes("out.bin", outBuf);
        }
        catch (AssertTrapException)
        {
            Console.WriteLine("RESAVE refused");
        }
        return 0;
    }
}
`, ns, root, capacity, dump.String())

	out, code, resaved, runErr := runUnityProbe(t, files, probe, map[string][]byte{"in.bin": wire})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 0 {
		t.Fatalf("LoadLineage probe exited %d: %s", code, out)
	}
	fields := map[string]string{}
	count, reason := -1, ""
	var counters fixedproof.LoadCounters
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if after, ok := strings.CutPrefix(line, "COUNTERS "); ok {
			parts := strings.Fields(after)
			if len(parts) == 5 {
				counters.Unknown, _ = strconv.Atoi(parts[0])
				counters.KindMismatch, _ = strconv.Atoi(parts[1])
				counters.Clamped, _ = strconv.Atoi(parts[2])
				counters.Widened, _ = strconv.Atoi(parts[3])
				counters.Duplicate, _ = strconv.Atoi(parts[4])
			}
		}
		if after, ok := strings.CutPrefix(line, "REFUSED "); ok {
			return -1, strings.TrimSpace(after), fields, nil, counters
		}
		if after, ok := strings.CutPrefix(line, "OK "); ok {
			var cerr error
			count, cerr = strconv.Atoi(strings.TrimSpace(after))
			if cerr != nil {
				t.Fatalf("native count %q: %v", after, cerr)
			}
			reason = "ok"
		}
		if after, ok := strings.CutPrefix(line, "FIELD "); ok {
			key, val, ok := strings.Cut(after, "=")
			if !ok {
				t.Fatalf("invalid native field %q", line)
			}
			fields[key] = val
		}
		if line == "RESAVE refused" && resaved == nil {
			resaved = []byte{}
		}
	}
	if count >= 0 && resaved != nil {
		return count, reason, fields, resaved, counters
	}
	t.Fatalf("native evolution probe omitted its result: %s", out)
	return -1, "", nil, nil, counters
}

func unityCompanionExpr(expr string, f *ir.Field, suffix string) string {
	name := ir.GoExportName(f.Name)
	slot := ir.GoExportName(f.Name) + suffix
	if i := strings.LastIndex(expr, "."+name); i >= 0 {
		return expr[:i+1] + slot + expr[i+1+len(name):]
	}
	return expr + suffix
}

func unityProofDumpFields(out *strings.Builder, pkg string, st *ir.Struct, expr, path, ind string, depth int) {
	for _, f := range st.Fields {
		unityProofDumpField(out, pkg, f, expr+"."+ir.GoExportName(f.Name), path+"."+f.Name, ind, depth)
	}
}

func unityProofDumpField(out *strings.Builder, pkg string, f *ir.Field, expr, path, ind string, depth int) {
	trueLit, falseLit := "1", "0"
	if pkg == "probe" {
		trueLit, falseLit = "true", "false"
	}
	closeBrace := false
	if f.Type.Optional {
		pres := unityCompanionExpr(expr, f, "Present")
		fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + %q + \"=\" + (%s ? %q : %q));\n", ind, path+"_present", pres, trueLit, falseLit)
		fmt.Fprintf(out, "%sif (%s)\n%s{\n", ind, pres, ind)
		ind += "    "
		closeBrace = true
	}
	if f.Array != ir.ArrayNone || f.KeyEnumRef != nil {
		loopV := fmt.Sprintf("idx_%d", depth)
		elemV := fmt.Sprintf("el_%d", depth)
		boundExpr := strconv.FormatInt(f.ArrayBound, 10)
		if f.KeyEnumRef != nil {
			boundExpr = strconv.FormatInt(f.KeyEnumRef.Max, 10)
		} else if f.Array == ir.ArrayCounted {
			cnt := unityCompanionExpr(expr, f, "Count")
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + %q + \"=\" + %s.ToString(CultureInfo.InvariantCulture));\n", ind, path+"_count", cnt)
			boundExpr = cnt
		}
		fmt.Fprintf(out, "%sfor (int %s = 0; %s < %s; %s++)\n%s{\n", ind, loopV, loopV, boundExpr, loopV, ind)
		fmt.Fprintf(out, "%s    var %s = %s[%s];\n", ind, elemV, expr, loopV)
		elemF := *f
		elemF.Array = ir.ArrayNone
		elemF.KeyEnumRef = nil
		elemF.Type.Optional = false
		elemPath := fmt.Sprintf("%s[\" + %s + \"]", path, loopV)
		unityProofDumpScalarOrComposite(out, pkg, &elemF, elemV, elemPath, "", ind+"    ", depth+1)
		fmt.Fprintf(out, "%s}\n", ind)
		if closeBrace {
			fmt.Fprintf(out, "%s}\n", ind[:len(ind)-4])
		}
		return
	}
	lenExpr := unityCompanionExpr(expr, f, "Length")
	unityProofDumpScalarOrComposite(out, pkg, f, expr, path, lenExpr, ind, depth+1)
	if closeBrace {
		fmt.Fprintf(out, "%s}\n", ind[:len(ind)-4])
	}
}

func unityProofDumpScalarOrComposite(out *strings.Builder, pkg string, f *ir.Field, expr, path, lenExpr, ind string, depth int) {
	trueLit, falseLit := "1", "0"
	if pkg == "probe" {
		trueLit, falseLit = "true", "false"
	}
	switch f.Type.Kind {
	case ir.TInt, ir.TBits:
		if f.Type.Width <= 64 {
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=\" + %s.ToString(CultureInfo.InvariantCulture));\n", ind, path, expr)
		}
	case ir.TFixed:
		if f.Type.Width <= 64 {
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=raw=\" + %s.ToString(CultureInfo.InvariantCulture));\n", ind, path, expr)
		}
	case ir.TBool:
		fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=\" + (%s ? %q : %q));\n", ind, path, expr, trueLit, falseLit)
	case ir.TString:
		if lenExpr != "" {
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=\\\"\" + ProbeCommon.GetUtf8(%s.AsSpan(), %s) + \"\\\"\");\n", ind, path, expr, lenExpr)
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s_length=\" + %s.ToString(CultureInfo.InvariantCulture));\n", ind, path, lenExpr)
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			unityProofDumpFields(out, pkg, ref, expr, path, ind, depth+1)
		case *ir.Union:
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s.type=\" + ((ulong)%s.Type).ToString(CultureInfo.InvariantCulture));\n", ind, path, expr)
			for idx, arm := range ref.Variants {
				if arm.F != nil {
					fmt.Fprintf(out, "%sif ((ulong)%s.Type == %dUL)\n%s{\n", ind, expr, idx+1, ind)
					af := *arm.F
					af.Name = arm.Name
					unityProofDumpField(out, pkg, &af, expr+"."+ir.GoExportName(arm.Name), path+"."+arm.Name, ind+"    ", depth+1)
					fmt.Fprintf(out, "%s}\n", ind)
				}
			}
		case *ir.Enum:
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=\" + ((ulong)%s).ToString(CultureInfo.InvariantCulture));\n", ind, path, expr)
		case *ir.Flags:
			fmt.Fprintf(out, "%sConsole.WriteLine(\"FIELD r\" + i + \"%s=\" + ((ulong)%s).ToString(CultureInfo.InvariantCulture));\n", ind, path, expr)
		}
	}
}

func (unityFixedProofRunner) SaveCustom(t *testing.T, u *ir.Unit, root, scenario string) ([]byte, string) {
	t.Helper()
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 1
	setup := ""
	switch scenario {
	case "optional_absent_zeros":
		setup = `            r[0].Plain.A = 11;
            r[0].Plain.B = 22;
            r[0].OptPresent = false;
            r[0].Opt.A = 0x12345678;
            r[0].Opt.B = 0x23456789;`
	case "live_extents_base":
		setup = `            r[0].ArrCount = 1;
            r[0].Arr[0] = Small.One;
            r[0].OptPresent = false;
            r[0].Tail = 77;`
	case "writing_slack_batch", "writing_slack_unzeroed":
		count = 2
		setup = `            r[0].ArrCount = 3;
            r[0].Arr[0] = 0x9999;
            r[0].Arr[1] = 0x9999;
            r[0].Arr[2] = 0x8888;
            ProbeCommon.SetUtf8(r[0].S.AsSpan(), out r[0].SLength, "ZZZZ");
            ProbeCommon.SetBytes(r[0].B.AsSpan(), out r[0].BLength, 0xAA, 0xAA, 0xAA, 0xAA);
            r[0].Pick.Type = PickType.Wide;
            r[0].Pick.Wide = ulong.MaxValue;
            r[1].ArrCount = 1;
            r[1].Arr[0] = 0x1122;
            r[1].Arr[1] = 0x9999;
            r[1].Arr[2] = 0x8888;
            ProbeCommon.SetUtf8(r[1].S.AsSpan(), out r[1].SLength, "A");
            r[1].S.E1 = (byte)'Z';
            r[1].S.E2 = (byte)'Z';
            r[1].S.E3 = (byte)'Z';
            ProbeCommon.SetBytes(r[1].B.AsSpan(), out r[1].BLength, 0x42);
            r[1].B.E1 = 0xAA;
            r[1].B.E2 = 0xAA;
            r[1].B.E3 = 0xAA;
            r[1].Pick.Type = PickType.Narrow;
            r[1].Pick.Narrow = 7;
            r[1].Pick.Wide = ulong.MaxValue;`
		if scenario == "writing_slack_unzeroed" {
			replaced := 0
			for name, data := range files {
				if strings.Contains(string(data), "body.Clear();") {
					files[name] = []byte(strings.ReplaceAll(string(data), "body.Clear();", "body.Fill(0xAA);"))
					replaced++
				}
			}
			if replaced != 1 {
				t.Fatalf("writing_slack_unzeroed replacement count = %d", replaced)
			}
		}
	case "writing_overcount":
		setup = `            r[0].ArrCount = 4;`
	case "closure_small_42":
		setup = `            r[0].X = 42;`
	case "outer_nested":
		setup = `            r[0].Head = 7U;
            r[0].Inner.A = 11;
            ProbeCommon.SetUtf8(r[0].Inner.B.AsSpan(), out r[0].Inner.BLength, "one");
            ProbeCommon.SetUtf8(r[0].Items.E0.B.AsSpan(), out r[0].Items.E0.BLength, "hi");
            r[0].Items.E1.A = 33;
            ProbeCommon.SetUtf8(r[0].Items.E1.B.AsSpan(), out r[0].Items.E1.BLength, "six");
            r[0].MaybePresent = true;
            r[0].Maybe.A = 44;
            r[0].Tail = 9U;`
	case "w14_writer_offsets":
		setup = `            r[0].A = 0xA1;
            r[0].B = 0x0B0B0B0B;
            r[0].Inner.P = 0x1E1E1E1E;
            r[0].Inner.Q = 0x2E2E;
            r[0].OptPresent = true;
            r[0].Opt = 0x0D0D0D0D;
            r[0].ArrCount = 2;
            r[0].Arr[0] = 0x3E3E;
            r[0].Arr[1] = 0x4E4E;
            ProbeCommon.SetUtf8(r[0].S.AsSpan(), out r[0].SLength, "seven");
            r[0].Tail = 0x7A7A7A7AU;`
	default:
		t.Fatalf("unknown SaveCustom scenario %q", scenario)
	}

	ns := unityNamespace(u.Package)
	probe := fmt.Sprintf(`using System;
using System.IO;
using Serialize;
using %[1]s;

static class Program
{
    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        try
        {
            %[2]s[] r = new %[2]s[%[3]d];
            for (int i = 0; i < r.Length; i++) { r[i] = Schema.New%[2]s(); }
%[4]s
            long need = Schema.%[2]sFixedMeasure(%[3]d);
            if (need < 0)
            {
                Console.WriteLine("REFUSED measure");
                return 0;
            }
            byte[] buf = new byte[need];
            long written = Schema.%[2]sFixedSave(r, buf);
            if (written != need)
            {
                Console.WriteLine("REFUSED save");
                return 0;
            }
            File.WriteAllBytes("out.bin", buf);
            Console.WriteLine("OK");
            return 0;
        }
        catch (AssertTrapException ex)
        {
            Console.WriteLine("REFUSED " + ex.Message);
            return 0;
        }
    }
}
`, ns, root, count, setup)

	out, code, wire, runErr := runUnityProbe(t, files, probe, nil)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if code != 0 {
		t.Fatalf("SaveCustom(%s) exited %d: %s", scenario, code, out)
	}
	out = strings.TrimSpace(out)
	if reason, ok := strings.CutPrefix(out, "REFUSED "); ok {
		return nil, reason
	}
	if out != "OK" || wire == nil {
		t.Fatalf("SaveCustom(%s) omitted wire: %q", scenario, out)
	}
	return wire, ""
}

func (unityFixedProofRunner) HasFixedRoot(t *testing.T, u *ir.Unit, root string) bool {
	t.Helper()
	files, err := unitytable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	needle := "public static long " + root + "FixedSave("
	for _, data := range files {
		if strings.Contains(string(data), needle) {
			return true
		}
	}
	return false
}
