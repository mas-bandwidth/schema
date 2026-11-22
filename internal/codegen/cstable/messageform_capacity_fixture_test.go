package cstable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestCSharpMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine), through the
// generated C# LoginRequestLoadMessages. The R4 ruling: after ANY refusal
// every value handed in holds its declared default (client_build 0), the
// whole array and not 0..count, and the count is 0:
//   - storage for one body (batch_too_large),
//   - storage for three with the form byte changed (a framing refusal),
//   - storage for three with the last byte dropped (a body or trailer refusal).
//
// All values start at the sentinel 17; slot 2 is past the batch's count of 2,
// so a reset of 0..count alone leaves it at 17. Storage for two owes 2 and 40.
func TestCSharpMessageLoadCapacityFixture(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "backend", "Backend.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	parts := make([]string, len(fixture))
	for i, v := range fixture {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	prog := strings.ReplaceAll(csCapacityFixtureMain, "FIXTURE", strings.Join(parts, ", "))
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(prog), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "capacityprobe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nowarn:CS8500", "-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, probePath)
	compile := testproc.Command(t, dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, out)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "capacityprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ROW capacity_fixture: ok") {
		t.Fatalf("capacity fixture: %v\n%s", err, out)
	}
}

const csCapacityFixtureMain = `using System;
using Backenddemo;

static class Program
{
    static string Refusal(string name, byte[] wire, int capacity, TableVocabulary voc)
    {
        var values = new LoginRequest[capacity];
        for (int i = 0; i < capacity; i++) { values[i] = new LoginRequest(); values[i].ClientBuild = 17; }
        var rep = new TableReport();
        var verdict = Schema.LoginRequestLoadMessages(values, wire, voc, rep, out int count);
        if (verdict == Schema.TableWire.Verdict.Ok || count != 0) { return name + ": verdict=" + verdict + " count=" + count + ", want a refusal with count 0"; }
        for (int i = 0; i < capacity; i++)
        {
            if (values[i].ClientBuild != 0) { return name + ": value " + i + " client_build reads " + values[i].ClientBuild + ", not its declared default 0"; }
        }
        return null;
    }

    static int Main()
    {
        byte[] wire = new byte[]{FIXTURE};
        var voc = new TableVocabulary();
        var arep = new TableReport();
        if (Schema.AnnounceRead(voc, Schema.Announce(), arep) != Schema.TableWire.Verdict.Ok) { Console.WriteLine("ROW capacity_fixture: FAILED announce"); return 1; }
        byte[] form = (byte[])wire.Clone(); form[0] = 3;
        byte[] cut = new byte[wire.Length - 1]; Array.Copy(wire, cut, cut.Length);
        string bad = Refusal("batch_too_large", wire, 1, voc) ?? Refusal("framing", form, 3, voc) ?? Refusal("dropped_last_byte", cut, 3, voc);
        if (bad != null) { Console.WriteLine("ROW capacity_fixture: FAILED " + bad); return 1; }
        var two = new LoginRequest[] { new LoginRequest(), new LoginRequest() };
        var loaded = new TableReport();
        var ok2 = Schema.LoginRequestLoadMessages(two, wire, voc, loaded, out int got);
        if (ok2 != Schema.TableWire.Verdict.Ok || got != 2 || two[0].ClientBuild != 40 || two[1].ClientBuild != 40) { Console.WriteLine("ROW capacity_fixture: FAILED two bodies into storage for two: " + ok2 + " " + got); return 1; }
        Console.WriteLine("ROW capacity_fixture: ok");
        return 0;
    }
}
`
