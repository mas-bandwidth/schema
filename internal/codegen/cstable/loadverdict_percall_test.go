package cstable

// The C# null-report read keeps no verdict in state another read can reach
// (docs/SPEC-TABLES.md §6.5). TestCSharpLoadVerdictPerCall checks that the
// emitted runtime carries no process-wide report a load writes, then runs two
// threads over one valid and one refused wire of one root, each doing a fixed
// count of null-report loads. TestCSharpLoadVerdictPerCallNegativeControl puts
// the shared fallback back in a copy of the generated artifact and requires the
// source check to reject it by name.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const csLoadVerdictSchema = `package probe

table Root
{
    nums [..4]uint8
}
`

const csLoadVerdictWiderSchema = `package probe

table Root
{
    nums [..5]uint8
}
`

func TestCSharpLoadVerdictPerCall(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C#)")
	files := generateFiles(t, "Probe", csLoadVerdictSchema)
	if err := csLoadVerdictNoSharedReport(files); err != nil {
		t.Error(err)
	}
	out, err := csLoadVerdictRun(t, files)
	t.Logf("probe:\n%s", out)
	if err != nil || !strings.Contains(out, "LOAD_VERDICT_OK") {
		t.Fatalf("null-report verdict per call:\n%s\n%v", out, err)
	}
}

func TestCSharpLoadVerdictPerCallNegativeControl(t *testing.T) {
	t.Parallel()
	files := generateFiles(t, "Probe", csLoadVerdictSchema)
	if err := csLoadVerdictNoSharedReport(files); err != nil {
		t.Fatalf("unplanted tree: %v", err)
	}
	planted := make(map[string][]byte, len(files))
	maps.Copy(planted, files)
	if err := csLoadVerdictPlantSharedReport(planted); err != nil {
		t.Fatal(err)
	}
	err := csLoadVerdictNoSharedReport(planted)
	if err == nil || !strings.Contains(err.Error(), "Ignored") {
		t.Fatalf("the planted shared report was not rejected by name: %v", err)
	}
	t.Logf("planted check: %v", err)
}

// csLoadVerdictReportField matches a static field whose type is TableReport.
// A method returning one has a parameter list where the field name stands, so
// this cannot mistake a helper for an instance.
var csLoadVerdictReportField = regexp.MustCompile(`^[ \t]*(?:(?:public|internal|private|protected)\s+)?static\s+(?:readonly\s+)?TableReport\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=.*)?;[ \t]*$`)

// csLoadVerdictNoSharedReport fails on a static TableReport instance that is
// not thread-static: that is the one a load on another thread can reach.
func csLoadVerdictNoSharedReport(files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		lines := strings.Split(string(files[name]), "\n")
		for i, line := range lines {
			m := csLoadVerdictReportField.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			threadStatic := strings.Contains(line, "[ThreadStatic]")
			if !threadStatic {
				for j := i - 1; j >= 0; j-- {
					prev := strings.TrimSpace(lines[j])
					if prev == "" || strings.HasPrefix(prev, "//") {
						continue
					}
					threadStatic = strings.Contains(prev, "[ThreadStatic]")
					break
				}
			}
			if !threadStatic {
				return fmt.Errorf("%s:%d: static TableReport %q is shared by every thread; a read's verdict must be its own", name, i+1, m[1])
			}
		}
	}
	return nil
}

// csLoadVerdictPlantSharedReport puts the process-wide fallback back in one
// generated file, both the instance and the null-report route to it.
func csLoadVerdictPlantSharedReport(files map[string][]byte) error {
	const anchor = "public enum Verdict { Ok, Refused, Damaged, BodyStopped }"
	planted := 0
	for name, data := range files {
		s := string(data)
		i := strings.Index(s, anchor)
		if i < 0 {
			continue
		}
		lineEnd := strings.IndexByte(s[i:], '\n')
		if lineEnd < 0 {
			return fmt.Errorf("%s: the form-1 Verdict line has no end", name)
		}
		at := i + lineEnd + 1
		s = s[:at] + "            public static readonly TableReport Ignored = new TableReport();\n" + s[at:]
		s = strings.Replace(s, "if (report == null) { report = IgnoredReport(); }", "if (report == null) { report = Ignored; }", 1)
		files[name] = []byte(s)
		planted++
	}
	if planted != 1 {
		return fmt.Errorf("plant touched %d files, want 1", planted)
	}
	return nil
}

// csLoadVerdictWires builds a valid wire at the declared bound and a refused
// wire whose count is one past it. The refused wire is legal under a wider
// declaration of the same field, so the wire's own bytes are what the reader
// refuses, not the writer's.
func csLoadVerdictWires(t *testing.T) (valid, refused string) {
	t.Helper()
	validWire := vtCellsEncode(t, csLoadVerdictSchema, "Root", `{"nums":[1,2,3,4]}`)
	refusedWire := vtCellsEncode(t, csLoadVerdictWiderSchema, "Root", `{"nums":[1,2,3,4,5]}`)
	return fmt.Sprintf("%x", validWire), fmt.Sprintf("%x", refusedWire)
}

func csLoadVerdictRun(t *testing.T, files map[string][]byte) (string, error) {
	t.Helper()
	valid, refused := csLoadVerdictWires(t)
	dir := t.TempDir()
	var sources []string
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
	main := filepath.Join(dir, "LoadVerdictProbe.cs")
	probe := fmt.Sprintf(csLoadVerdictProbeSrc, valid, refused)
	if err := os.WriteFile(main, []byte(probe), 0o600); err != nil {
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
		return string(out), fmt.Errorf("csc: %w", err)
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
		return string(out), err
	}
	return string(out), nil
}

const csLoadVerdictProbeSrc = `using System;
using System.Threading;
using Probe;

public static class LoadVerdictProbe
{
    const int Count = 200000;
    static byte[] valid;
    static byte[] refused;
    static int validBad;
    static int refusedBad;

    static void ValidLoop()
    {
        Root v = new Root();
        for (int i = 0; i < Count; i++)
        {
            if (!Schema.RootLoad(v, valid, null)) { validBad++; }
        }
    }

    static void RefusedLoop()
    {
        Root v = new Root();
        for (int i = 0; i < Count; i++)
        {
            if (Schema.RootLoad(v, refused, null)) { refusedBad++; }
        }
    }

    static void Race()
    {
        validBad = 0;
        refusedBad = 0;
        Barrier gate = new Barrier(3);
        Thread a = new Thread(() => { gate.SignalAndWait(); ValidLoop(); });
        Thread b = new Thread(() => { gate.SignalAndWait(); RefusedLoop(); });
        a.Start();
        b.Start();
        gate.SignalAndWait();
        a.Join();
        b.Join();
    }

    public static int Main()
    {
        valid = Convert.FromHexString("%s");
        refused = Convert.FromHexString("%s");
        if (!Schema.RootLoad(new Root(), valid, null))
        {
            Console.WriteLine("a valid wire was refused with no report on one thread");
            return 2;
        }
        if (Schema.RootLoad(new Root(), refused, null))
        {
            Console.WriteLine("a refused wire was accepted with no report on one thread");
            return 3;
        }
        Race();
        if (validBad != 0 || refusedBad != 0)
        {
            Console.WriteLine("a read's verdict crossed to another read: valid refused " + validBad + ", refused accepted " + refusedBad + " of " + Count + " each");
            return 1;
        }
        Console.WriteLine("LOAD_VERDICT_OK");
        return 0;
    }
}
`
