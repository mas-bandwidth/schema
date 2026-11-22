package unitytable_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestUnityFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet and the live C++ oracle")
	fixedproof.RunValueSuite(t, unityFixedProofRunner{})
}

func TestUnityFixedProofSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet and the live C++ oracle")
	fixedproof.RunNegativeControlSuite(t, unityFixedProofRunner{})
}

type unityFixedProofRunner struct{}

var _ fixedproof.LegRunner = unityFixedProofRunner{}

func (unityFixedProofRunner) Name() string { return "unity" }

func (unityFixedProofRunner) Save(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) []byte {
	t.Helper()
	out, err := unityProofRun(t, tc, u, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (unityFixedProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	return unityProofRun(t, tc, u, wire, true, false)
}

func (unityFixedProofRunner) SaveMutated(t *testing.T, tc fixedproof.RowCase, u *ir.Unit) ([]byte, error) {
	t.Helper()
	return unityProofRun(t, tc, u, nil, false, true)
}

func unityProofRun(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte, load, mutate bool) ([]byte, error) {
	t.Helper()
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		return nil, err
	}
	if mutate {
		anchor := fmt.Sprintf("%sFixedStore(body, in values[k]);", tc.Root)
		replacement := anchor + " body[0] ^= 1;"
		hits := 0
		for name, data := range files {
			hits += strings.Count(string(data), anchor)
			if strings.Contains(string(data), anchor) {
				files[name] = []byte(strings.ReplaceAll(string(data), anchor, replacement))
			}
		}
		if hits != 1 {
			t.Fatalf("%s: writer mutation anchors: %d", tc.ID, hits)
		}
	}

	ns := unityNamespace(u.Package)
	setup, check := unityProofFixture(tc.ID)
	body := fmt.Sprintf(`        %s[] r = new %s[%d];
        for (int i = 0; i < r.Length; i++) { r[i] = Schema.New%s(); }
%s`, tc.Root, tc.Root, tc.Count, tc.Root, setup)
	if load {
		body = fmt.Sprintf(`        byte[] input = File.ReadAllBytes("in.bin");
        %s[] r = new %s[%d];
        for (int i = 0; i < r.Length; i++) { r[i] = Schema.New%s(); }
        TableReport report = new TableReport { Unknown = 9, Widened = 9, Reason = 99, LayoutHash = 99 };
        long n = Schema.%sFixedLoad(r.AsSpan(), input, ref report);
        if (n != %d || report.Refused || report.Reason != 0 || report.Unknown != 0 || report.Widened != 0 || report.LayoutHash != 0)
        {
            Console.Error.WriteLine("load failed: n=" + n + " reason=" + TableReport.ReasonName(report.Reason));
            return 2;
        }
%s`, tc.Root, tc.Root, tc.Count, tc.Root, tc.Root, tc.Count, check)
	}

	probe := fmt.Sprintf(`using System;
using System.IO;
using Serialize;
using %[1]s;
using static %[1]s.Schema;

static class Program
{
    static void Req(bool cond, string msg = "check failed")
    {
        if (!cond) { throw new InvalidOperationException(msg); }
    }

    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        try
        {
%[2]s
            long need = Schema.%[3]sFixedMeasure(%[4]d);
            Req(need > 0, "measure");
            byte[] outBuf = new byte[need];
            long written = Schema.%[3]sFixedSave(r, outBuf);
            Req(written == need, "written");
            ProbeCommon.WriteHex(outBuf);
            return 0;
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine(ex.GetType().Name + ": " + ex.Message);
            return 3;
        }
    }
}
`, ns, body, tc.Root, tc.Count)

	var inputs map[string][]byte
	if load {
		inputs = map[string][]byte{"in.bin": wire}
	}
	out, code, _, runErr := runUnityProbe(t, files, probe, inputs)
	if runErr != nil {
		return nil, runErr
	}
	if code != 0 {
		return nil, fmt.Errorf("%s: exit %d: %s", tc.ID, code, strings.TrimSpace(out))
	}
	return hex.DecodeString(strings.TrimSpace(out))
}

func unityProofFixture(id string) (setup, check string) {
	switch id {
	case "bool-values":
		return `        r[1].A = true;
        r[1].B = false;`, `        Req(r[0].A == false && r[0].B == true);
        Req(r[1].A == true && r[1].B == false);`
	case "signed-integers":
		return `        r[1].A = 127;
        r[1].B = 32767;
        r[1].C = 2147483647;
        r[1].D = 9223372036854775807L;
        r[2].A = -1;
        r[2].B = 0;
        r[2].C = 1;
        r[2].D = -2L;`, `        Req(r[1].A == 127 && r[1].B == 32767 && r[1].C == 2147483647 && r[1].D == 9223372036854775807L);
        Req(r[2].A == -1 && r[2].B == 0 && r[2].C == 1 && r[2].D == -2L);`
	case "unsigned-integers":
		return `        r[1].A = 0;
        r[1].B = 0;
        r[1].C = 0;
        r[1].D = 0UL;
        r[2].A = 1;
        r[2].B = 256;
        r[2].C = 65536U;
        r[2].D = 4294967296UL;`, `        Req(r[1].A == 0 && r[1].B == 0 && r[1].C == 0 && r[1].D == 0UL);
        Req(r[2].A == 1 && r[2].B == 256 && r[2].C == 65536U && r[2].D == 4294967296UL);`
	case "signed-128":
		return `        r[1].A = new Int128Value(0x7fffffffffffffffUL, 0xffffffffffffffffUL);
        r[2].A = new Int128Value(0UL, 1UL);`, `        Req(r[1].A.Hi == 0x7fffffffffffffffUL && r[1].A.Lo == 0xffffffffffffffffUL);
        Req(r[2].A.Hi == 0UL && r[2].A.Lo == 1UL);`
	case "unsigned-128":
		return `        r[1].A = new UInt128Value(0UL, 0UL);
        r[2].A = new UInt128Value(0UL, 1UL);`, `        Req(r[1].A.Hi == 0UL && r[1].A.Lo == 0UL);
        Req(r[2].A.Hi == 0UL && r[2].A.Lo == 1UL);`
	case "ranged-integers":
		return `        r[0].A = -5;
        r[0].B = -1000000000000L;
        r[0].C = 0;
        r[1].A = 5;
        r[1].B = 1000000000000L;
        r[1].C = 1000;`, `        Req(r[0].A == -5 && r[0].B == -1000000000000L && r[0].C == 0);
        Req(r[1].A == 5 && r[1].B == 1000000000000L && r[1].C == 1000);`
	case "bits-values":
		return `        r[0].A = 7U;
        r[0].B = 4095U;
        r[0].C = 1048575U;
        r[0].D = 1099511627775UL;
        r[1].A = 0U;
        r[1].B = 0U;
        r[1].C = 0U;
        r[1].D = 0UL;`, `        Req(r[0].A == 7U && r[0].B == 4095U && r[0].C == 1048575U && r[0].D == 1099511627775UL);
        Req(r[1].A == 0U && r[1].B == 0U && r[1].C == 0U && r[1].D == 0UL);`
	case "float32-values":
		return `        r[1].A = -1024.25f;
        r[1].B = 2048.75f;`, `        Req(r[1].A == -1024.25f && r[1].B == 2048.75f);`
	case "float64-values":
		return `        r[1].A = -987654321.125;
        r[1].B = 123456789.875;`, `        Req(r[1].A == -987654321.125 && r[1].B == 123456789.875);`
	case "compressed-floats":
		return `        r[0].A = 0.25f;
        r[0].B = -0.5f;
        r[1].A = 0.0f;
        r[1].B = -1.0f;
        r[2].A = 1.0f;
        r[2].B = 1.0f;`, `        Req(r[0].A == 0.25f && r[0].B == -0.5f);
        Req(r[1].A == 0.0f && r[1].B == -1.0f);
        Req(r[2].A == 1.0f && r[2].B == 1.0f);`
	case "signed-fixed-point":
		return `        r[0].A = -(8 << 16);
        r[0].B = -(100L << 16);
        r[0].C = new Int128Value(0xffffffffffffffffUL, 0xffffffffff000000UL);
        r[1].A = (8 << 16);
        r[1].B = (100L << 16);
        r[1].C = new Int128Value(0UL, 0x0000000000fa0000UL);`, `        Req(r[0].A == -(8 << 16) && r[0].B == -(100L << 16) && r[0].C.Hi == 0xffffffffffffffffUL && r[0].C.Lo == 0xffffffffff000000UL);
        Req(r[1].A == (8 << 16) && r[1].B == (100L << 16) && r[1].C.Hi == 0UL && r[1].C.Lo == 0x0000000000fa0000UL);`
	case "unsigned-fixed-point":
		return `        r[0].A = 0U;
        r[0].B = 0UL;
        r[0].C = new UInt128Value(0UL, 0UL);
        r[1].A = (16U << 16);
        r[1].B = (100UL << 16);
        r[1].C = new UInt128Value(0UL, 1000UL << 16);`, `        Req(r[0].A == 0U && r[0].B == 0UL && r[0].C.Hi == 0UL && r[0].C.Lo == 0UL);
        Req(r[1].A == (16U << 16) && r[1].B == (100UL << 16) && r[1].C.Hi == 0UL && r[1].C.Lo == (1000UL << 16));`
	case "flags-values":
		return `        r[0].Caps = (Caps)0;
        r[1].Caps = (Caps)5;
        r[2].Caps = (Caps)7;`, `        Req((ulong)r[0].Caps == 0UL && (ulong)r[1].Caps == 5UL && (ulong)r[2].Caps == 7UL);`
	case "enum-values":
		return `        r[0].G = Grade.None;
        r[1].G = Grade.Gold;
        r[2].G = Grade.Bronze;`, `        Req(r[0].G == Grade.None && r[1].G == Grade.Gold && r[2].G == Grade.Bronze);`
	case "utf8-values":
		return `        ProbeCommon.SetUtf8(r[1].S.AsSpan(), out r[1].SLength, "hello");`, `        Req(r[1].SLength == 5 && ProbeCommon.GetUtf8(r[1].S.AsSpan(), r[1].SLength) == "hello");`
	case "byte-buffers":
		return `        ProbeCommon.SetBytes(r[1].Blob.AsSpan(), out r[1].BlobLength, 0xff, 0x00, 0x5a, 1, 2, 3);`, `        Req(ProbeCommon.BytesEq(r[1].Blob.AsSpan(), r[1].BlobLength, 0xff, 0x00, 0x5a, 1, 2, 3));`
	case "nested-types":
		return `        ProbeCommon.SetUtf8(r[0].Badge.Title.AsSpan(), out r[0].Badge.TitleLength, "hi");`, `        Req(r[0].Badge.TitleLength == 2 && ProbeCommon.GetUtf8(r[0].Badge.Title.AsSpan(), r[0].Badge.TitleLength) == "hi");`
	case "nested-fixed-tables":
		return `        r[0].Child.N = int.MinValue;
        r[1].Child.N = int.MaxValue;`, `        Req(r[0].Child.N == int.MinValue && r[1].Child.N == int.MaxValue);`
	case "fixed-arrays":
		return `        r[0].Tag[0] = int.MinValue;
        r[0].Tag[1] = 0;
        r[0].Tag[2] = int.MaxValue;`, `        Req(r[0].Tag[0] == int.MinValue && r[0].Tag[1] == 0 && r[0].Tag[2] == int.MaxValue);`
	case "counted-arrays":
		return `        r[1].Nums[0] = 10;
        r[1].Nums[1] = 65535;
        r[1].Nums[2] = 32768;
        r[1].NumsCount = 3;`, `        Req(r[1].NumsCount == 3 && r[1].Nums[0] == 10 && r[1].Nums[1] == 65535 && r[1].Nums[2] == 32768);`
	case "keyed-arrays":
		return `        r[0].Slots[(int)Key.A - 1] = int.MinValue;
        r[0].Slots[(int)Key.B - 1] = int.MaxValue;`, `        Req(r[0].Slots[(int)Key.A - 1] == int.MinValue && r[0].Slots[(int)Key.B - 1] == int.MaxValue);`
	case "nested-keyed-arrays":
		return `        r[0].Slots.E0.N = 11;
        r[0].Slots.E1.N = -22;`, `        Req(r[0].Slots.E0.N == 11 && r[0].Slots.E1.N == -22);`
	case "union-values":
		return `        r[0].Body.Type = BodyType.V;
        r[0].Body.V.X = 11;
        r[0].Body.V.Y = -22;
        r[1].Body.Type = BodyType.T;
        r[1].Body.T.N = 41;
        r[2].Body.Type = BodyType.N;
        r[2].Body.N = 1073741821;`, `        Req(r[0].Body.Type == BodyType.V && r[0].Body.V.X == 11 && r[0].Body.V.Y == -22);
        Req(r[1].Body.Type == BodyType.T && r[1].Body.T.N == 41);
        Req(r[2].Body.Type == BodyType.N && r[2].Body.N == 1073741821);`
	case "union-field-arms":
		return `        r[0].Body.Type = BodyType.Sa;
        r[0].Body.Sa = -5;
        r[1].Body.Type = BodyType.Ta;
        ProbeCommon.SetUtf8(r[1].Body.Ta.AsSpan(), out r[1].Body.TaLength, "abc");
        r[2].Body.Type = BodyType.Aa;
        r[2].Body.Aa[0] = -1;
        r[2].Body.Aa[1] = 0;
        r[2].Body.Aa[2] = 1;
        r[2].Body.AaCount = 3;`, `        Req(r[0].Body.Type == BodyType.Sa && r[0].Body.Sa == -5);
        Req(r[1].Body.Type == BodyType.Ta && r[1].Body.TaLength == 3 && ProbeCommon.GetUtf8(r[1].Body.Ta.AsSpan(), r[1].Body.TaLength) == "abc");
        Req(r[2].Body.Type == BodyType.Aa && r[2].Body.AaCount == 3 && r[2].Body.Aa[0] == -1 && r[2].Body.Aa[1] == 0 && r[2].Body.Aa[2] == 1);`
	case "payload-free-arms":
		return `        r[0].Lead = 10;
        r[0].Pick.Type = PickType.Alpha;
        r[0].Pick.Alpha = 99;
        r[0].Trail = 20;
        r[1].Lead = 30;
        r[1].Pick.Alpha = 777;
        r[1].Pick.Type = PickType.Quiet;
        r[1].Trail = 40;
        r[2].Lead = 50;
        r[2].Pick.Alpha = 888;
        r[2].Pick.Type = PickType.Signal;
        r[2].Trail = 60;`, `        Req(r[0].Lead == 10 && r[0].Pick.Type == PickType.Alpha && r[0].Pick.Alpha == 99 && r[0].Trail == 20);
        Req(r[1].Lead == 30 && r[1].Pick.Type == PickType.Quiet && r[1].Trail == 40);
        Req(r[2].Lead == 50 && r[2].Pick.Type == PickType.Signal && r[2].Trail == 60);`
	case "union-arrays":
		return `        r[0].PicksCount = 3;
        r[0].Picks.E0.Type = PickType.A;
        r[0].Picks.E0.A = 3;
        r[0].Picks.E1.Type = PickType.B;
        r[0].Picks.E1.B = -1099511627776L;
        r[0].Picks.E2.Type = PickType.A;
        r[0].Picks.E2.A = 0;`, `        Req(r[0].PicksCount == 3 && r[0].Picks.E0.Type == PickType.A && r[0].Picks.E0.A == 3);
        Req(r[0].Picks.E1.Type == PickType.B && r[0].Picks.E1.B == -1099511627776L);
        Req(r[0].Picks.E2.Type == PickType.A && r[0].Picks.E2.A == 0);`
	case "optional-scalars":
		return `        r[1].N = -123456;
        r[1].NPresent = true;
        r[1].G = Grade.Silver;
        r[1].GPresent = true;`, `        Req(!r[0].NPresent && !r[0].GPresent);
        Req(r[1].NPresent && r[1].N == -123456 && r[1].GPresent && r[1].G == Grade.Silver);`
	case "optional-nesting":
		return `        r[1].P.X = 99;
        r[1].P.Y = -99;
        r[1].PPresent = true;`, `        Req(!r[0].PPresent && r[1].PPresent && r[1].P.X == 99 && r[1].P.Y == -99);`
	case "optional-arrays":
		return `        r[1].Xs[0] = 7;
        r[1].Xs[1] = -8;
        r[1].Xs[2] = 9;
        r[1].XsPresent = true;
        r[1].XsCount = 3;`, `        Req(!r[0].XsPresent && r[1].XsPresent && r[1].XsCount == 3 && r[1].Xs[0] == 7 && r[1].Xs[1] == -8 && r[1].Xs[2] == 9);`
	case "scalar-defaults":
		return `        r[1].X = -9;
        r[1].G = Grade.Bronze;
        r[1].F = -2.25f;`, `        Req(r[0].X == 7 && r[0].G == Grade.Silver && r[0].F == 1.5f);
        Req(r[1].X == -9 && r[1].G == Grade.Bronze && r[1].F == -2.25f);`
	case "text-bytes-flags-defaults":
		return `        ProbeCommon.SetUtf8(r[1].S.AsSpan(), out r[1].SLength, "xyz");
        ProbeCommon.SetBytes(r[1].B.AsSpan(), out r[1].BLength, 0x00, 0xff, 0x5a, 0x01);
        r[1].C = (Caps)4;`, `        Req(r[0].SLength == 2 && ProbeCommon.GetUtf8(r[0].S.AsSpan(), r[0].SLength) == "hi");
        Req(r[1].SLength == 3 && ProbeCommon.GetUtf8(r[1].S.AsSpan(), r[1].SLength) == "xyz");
        Req(ProbeCommon.BytesEq(r[1].B.AsSpan(), r[1].BLength, 0x00, 0xff, 0x5a, 0x01) && (ulong)r[1].C == 4UL);`
	case "record-measurement":
		return `        r[0].A = 10U;
        r[0].B = 20;
        r[1].A = 30U;
        r[1].B = 40;`, `        Req(r[0].A == 10U && r[0].B == 20 && r[1].A == 30U && r[1].B == 40);`
	case "fixed-file-roundtrip":
		return `        r[1].A = 22;
        ProbeCommon.SetUtf8(r[1].B.AsSpan(), out r[1].BLength, "test");`, `        Req(r[0].A == 11 && r[1].A == 22 && ProbeCommon.GetUtf8(r[1].B.AsSpan(), r[1].BLength) == "test");`
	case "file-envelope":
		return `        r[1].X = 10;
        r[1].Y = 20;`, `        Req(r[1].X == 10 && r[1].Y == 20);`
	case "batch-capacity":
		return `        r[1].X = 3;
        r[1].Y = 4;
        r[2].X = 5;
        r[2].Y = 6;`, `        Req(r[1].X == 3 && r[1].Y == 4 && r[2].X == 5 && r[2].Y == 6);`
	case "plan-selection":
		return `        r[1].X = 33;
        r[1].Y = 44;`, `        Req(r[1].X == 33 && r[1].Y == 44);`
	case "compiled-plans":
		return `        r[1].A = 100U;
        r[1].B = -20L;
        r[1].C = 55;`, `        Req(r[1].A == 100U && r[1].B == -20L && r[1].C == 55);`
	case "definition-hash":
		return `        r[1].X = 9;
        ProbeCommon.SetUtf8(r[1].B.AsSpan(), out r[1].BLength, "abc");`, `        Req(r[1].X == 9 && r[1].BLength == 3);`
	case "fixed-closure":
		return `        r[1].N.A = 30;
        r[1].N.B = 40;
        r[1].Z = 70;`, `        Req(r[1].N.A == 30 && r[1].N.B == 40 && r[1].Z == 70);`
	case "retirement":
		return `        r[1].A = 10;
        r[1].B = 20;
        r[1].C = 30;`, `        Req(r[1].A == 10 && r[1].B == 20 && r[1].C == 30);`
	case "retired-runtime":
		return `        r[1].X = 44;
        r[1].Y = 55;
        r[1].Z = 66;`, `        Req(r[1].X == 44 && r[1].Y == 55 && r[1].Z == 66);`
	case "optional-values":
		return `        r[1].Plain.A = 11;
        r[1].Plain.B = 22;
        r[1].OptPresent = true;
        r[1].Opt.A = 33;
        r[1].Opt.B = 44;`, `        Req(r[1].Plain.A == 11 && r[1].Plain.B == 22 && r[1].OptPresent && r[1].Opt.A == 33 && r[1].Opt.B == 44);`
	case "field-evolution":
		return `        r[1].B = 100;
        r[1].C = 200;`, `        Req(r[1].B == 100 && r[1].C == 200);`
	case "live-extents":
		return `        r[1].ArrCount = 1;
        r[1].Arr[0] = Small.One;
        r[1].OptPresent = false;
        r[1].Tail = 88;`, `        Req(r[1].ArrCount == 1 && r[1].Arr[0] == Small.One && !r[1].OptPresent && r[1].Tail == 88);`
	case "writing":
		return `        r[1].ArrCount = 1;
        r[1].Arr[0] = 0x1122;
        ProbeCommon.SetUtf8(r[1].S.AsSpan(), out r[1].SLength, "A");
        ProbeCommon.SetBytes(r[1].B.AsSpan(), out r[1].BLength, 0x42);
        r[1].Pick.Type = PickType.Narrow;
        r[1].Pick.Narrow = 7;`, `        Req(r[1].ArrCount == 1 && r[1].Arr[0] == 0x1122 && r[1].SLength == 1 && r[1].BLength == 1 && r[1].Pick.Type == PickType.Narrow && r[1].Pick.Narrow == 7);`
	case "union-guards":
		return `        r[1].U.Type = OuterType.Nested;
        r[1].U.Nested.X = 12;
        r[1].U.Nested.Y = 0x12345678;
        r[1].Tail = 77;`, `        Req(r[1].U.Type == OuterType.Nested && r[1].U.Nested.X == 12 && r[1].U.Nested.Y == 0x12345678 && r[1].Tail == 77);`
	case "plan-execution":
		return `        r[1].A = 10;
        r[1].Sub.P = 30;
        r[1].Sub.Q = 40;
        r[1].XsCount = 2;
        r[1].Xs[0] = 50;
        r[1].Xs[1] = 60;`, `        Req(r[1].A == 10 && r[1].Sub.P == 30 && r[1].Sub.Q == 40 && r[1].XsCount == 2 && r[1].Xs[0] == 50 && r[1].Xs[1] == 60);`
	case "interoperability":
		return `        r[1].A = 42;
        ProbeCommon.SetUtf8(r[1].S.AsSpan(), out r[1].SLength, "test");`, `        Req(r[1].A == 42 && r[1].SLength == 4 && ProbeCommon.GetUtf8(r[1].S.AsSpan(), r[1].SLength) == "test");`
	default:
		panic("missing Unity fixed proof fixture: " + id)
	}
}
