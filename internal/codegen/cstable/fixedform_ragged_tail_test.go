package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.3 `fixed_form_ragged_tail`, schema#876 item F8 — a GAP on go, cpp and cs,
// only partial on c and elixir. A record region that is not a whole number of
// records is MALFORMED, not refused. It is F7's direct sibling, the very next
// guard in the same emitted function; go closed it in #1291. Nothing already
// covers the ragged tail on this leg: fixedform_under_20_bytes stops at the
// short-buffer guard and no other row appends bytes to a whole file. The reader
// owes two answers and they are NEVER both set — `refused` plus a `reason`, or
// `malformed`. A ragged tail is the residue of a bad file, so Reason stays
// untouched. record_bytes is the SELECTED LINEAGE ENTRY's record size, compiled
// into the reader (known.Record), so the guard's `record_bytes <= 8` arm is NOT
// reachable from a file at all; rest is the file's bytes after the header and
// layout, so this row forges only `rest % record_bytes != 0` by appending 1 to
// record_bytes-1 bytes. extra == record_bytes is one more WHOLE record and owes
// batch_too_large, not malformed, and is measured separately. CONTROL 2 deletes
// the ragged arm and must turn the row RED; on go that red was a reported
// SUCCESS, not a crash, so the shape is recorded, not assumed.
func TestFixedFormRaggedTail(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	probes := []csVersionProbe{
		{name: "fixed_form_ragged_tail", reader: "VNEW_array_bounded_grow",
			older: []string{"VOLD_array_bounded_grow"}, file: "old_array_bounded_grow.bin",
			body: `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            // THE FIXTURE READS CLEANLY AT ITS TRUE LENGTH FIRST, so a broken
            // fixture cannot pass this row by accident.
            @TABLE@[] full = new @TABLE@[8];
            for (int i = 0; i < full.Length; ++i) { full[i] = new @TABLE@(); }
            TableReport rf = new TableReport();
            TableFixedEntry[] planf = new TableFixedEntry[8192];
            long nf = Schema.@TABLE@FixedLoad(full, data, planf, rf);
            if (nf != 1 || rf.Refused || rf.Malformed || rf.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "the fixture does not read cleanly at its true length: n=" + nf + " refused=" + rf.Refused + " malformed=" + rf.Malformed + " reason=" + rf.Reason);
            }
            // THE RECORD SIZE IS THE SELECTED LINEAGE ENTRY'S, read off the
            // emitted lineage (Schema.@TABLE@FixedKnown) by the file's hash and
            // never from a literal: the emitter reads known.Record, and this is
            // the same decision reached through the generated table.
            ulong fhash = System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(data.AsSpan(Schema.TableFixedWire.HashAt));
            long recordBytes = -1;
            foreach (TableFixedKnownLayout k in Schema.@TABLE@FixedKnown)
            {
                if (k.Hash == fhash) { recordBytes = k.Record; break; }
            }
            if (recordBytes <= 0)
            {
                bad += ProbeLog.Fail(@NAME@, "the file's hash is not in the lineage, so the record size cannot be read from the emitted code");
            }
            // THE RAGGED LOOP: extra runs 1 .. recordBytes-1. extra == recordBytes
            // is one more WHOLE record, not a ragged tail, and owes a different
            // answer, so the loop stops short of it and the boundary is measured
            // separately below.
            for (int extra = 1; extra < recordBytes; ++extra)
            {
                byte[] ragged = new byte[data.Length + extra];
                data.CopyTo(ragged, 0);
                @TABLE@[] back = new @TABLE@[8];
                for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
                // THE POISON: every field of back[0] carries a recognisable
                // sentinel, so a read that returns -1 can be told apart from one
                // that wrote the destination. 0x5A5A5A5A is neither the lead,
                // the trail, nor any declared default.
                back[0].Lead = 0x5A5A5A5Au;
                back[0].ValsCount = 0x5A5A5A5A;
                for (int i = 0; i < 8; ++i) { back[0].Vals[i] = 0x5A5A5A5A; }
                back[0].Trail = 0x5A5A5A5Au;
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, ragged, plan, r);
                if (n != -1)
                {
                    bad += ProbeLog.Fail(@NAME@, "a record region that is not whole records must be malformed and return -1: extra=" + extra + " n=" + n + " malformed=" + r.Malformed + " refused=" + r.Refused);
                    continue;
                }
                if (!r.Malformed)
                {
                    bad += ProbeLog.Fail(@NAME@, "a ragged tail is malformed, never clean: extra=" + extra + " malformed=" + r.Malformed);
                }
                if (r.Refused)
                {
                    bad += ProbeLog.Fail(@NAME@, "a ragged tail is the residue of a bad file, not a refusal by name: extra=" + extra + " refused=" + r.Refused + " reason=" + r.Reason);
                }
                if (r.Reason != null)
                {
                    bad += ProbeLog.Fail(@NAME@, "reason stays untouched on a malformed read: extra=" + extra + " reason=" + r.Reason);
                }
                if (r.Verdict != Schema.TableWire.Verdict.Damaged)
                {
                    bad += ProbeLog.Fail(@NAME@, "this leg answers Verdict.Damaged alongside Malformed: extra=" + extra + " verdict=" + r.Verdict);
                }
                if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "every counter is zero on a malformed read: extra=" + extra + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
                }
                if (r.LayoutHash != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "malformed publishes no hash: extra=" + extra + " hash=0x" + r.LayoutHash.ToString("x16"));
                }
                if (back[0].Lead != 0x5A5A5A5Au || back[0].ValsCount != 0x5A5A5A5A || back[0].Trail != 0x5A5A5A5Au)
                {
                    bad += ProbeLog.Fail(@NAME@, "the malformed read wrote the destination: extra=" + extra + " lead=" + back[0].Lead + " valsCount=" + back[0].ValsCount + " trail=" + back[0].Trail);
                }
                for (int i = 0; i < 8; ++i)
                {
                    if (back[0].Vals[i] != 0x5A5A5A5A)
                    {
                        bad += ProbeLog.Fail(@NAME@, "the malformed read wrote vals[" + i + "]: extra=" + extra + " v=" + back[0].Vals[i]);
                    }
                }
            }
            // extra == recordBytes is ONE MORE WHOLE RECORD, not a ragged tail:
            // the guard does not fire, n is 2, and against a one-slot destination
            // the count-vs-capacity check refuses batch_too_large BY NAME.
            {
                byte[] whole = new byte[data.Length + (int)recordBytes];
                data.CopyTo(whole, 0);
                @TABLE@[] one = new @TABLE@[1];
                one[0] = new @TABLE@();
                TableReport rw = new TableReport();
                TableFixedEntry[] planw = new TableFixedEntry[8192];
                long nw = Schema.@TABLE@FixedLoad(one, whole, planw, rw);
                if (nw != -1 || !rw.Refused || rw.Reason != "batch_too_large" || rw.Malformed)
                {
                    bad += ProbeLog.Fail(@NAME@, "extra == recordBytes is one more whole record and owes batch_too_large, not malformed: n=" + nw + " refused=" + rw.Refused + " reason=" + rw.Reason + " malformed=" + rw.Malformed);
                }
            }`},
	}

	var calls []string
	for i, p := range probes {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer(
			"@NAME@", fmt.Sprintf("%q", p.name),
			"@TABLE@", table,
			"@FILE@", fmt.Sprintf("%q", p.file),
		).Replace(p.body)
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
	out, runErr := cmd.CombinedOutput()

	csVersionAssert(t, out, runErr, "fixed_form_ragged_tail")
}
