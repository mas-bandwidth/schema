package cstable

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestCSharpJsonWriterRefusesZeroByte holds docs/SPEC.md §5.1 row 8 and
// docs/SPEC-TABLES.md §16.3, §16.14 W0 and W8 on the C# leg: a string field,
// a text-list element and a map key holding a zero byte is storage that is not
// text, caught by the leg's debug write assert as its binary writers are. The
// same value without a zero writes the canonical text as before. In a release
// build the assert is gone, which is the leg's write-refusal doctrine.
func TestCSharpJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	const schemaSrc = `package probe
table Root {
    s string(8)
    l [..4]string(8)
    m map[string(8)]int32
}
`
	const input = `{"s":"ab","l":["x","y"],"m":{"k":7}}`

	u := unitFrom(t, schemaSrc)
	model := tabletext.NewModel(u)
	value := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(value, []byte(input), &report) {
		t.Fatalf("control did not read: %+v", report)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimRight(string(text), "\n")

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
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
	main := filepath.Join(dir, "ZeroProbe.cs")
	if err := os.WriteFile(main, []byte(zeroProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)

	csc, refs, runtime := csDotnetToolchain(t)
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-define:DEBUG", "-out:" + filepath.Join(dir, "probe.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(mode, site string) ([]byte, error) {
		cmd := exec.Command(csDotnet(), "exec", filepath.Join(dir, "probe.dll"), mode, site)
		cmd.Env = csDotnetEnv(dir, dir)
		return cmd.CombinedOutput()
	}

	// The control: the same value with no zero writes the canonical text, and
	// measure and write agree, in a debug build where every write check is on.
	control, controlErr := run("control", "none")
	if controlErr != nil {
		t.Fatalf("the control write did not return: %v\n%s", controlErr, control)
	}
	marker := "CANON:"
	at := strings.Index(string(control), marker)
	if at < 0 {
		t.Fatalf("the control write printed no canonical text:\n%s", control)
	}
	encoded := strings.TrimSpace(string(control)[at+len(marker):])
	encoded = strings.SplitN(encoded, "\n", 2)[0]
	canon, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("control canonical text: %v", err)
	}
	if string(canon) != want {
		t.Fatalf("the control wrote %q, want %q", canon, want)
	}

	// A zero in a string field, a text-list element and a map key is refused by
	// the writer, under measure and under write alike, in a debug build.
	for _, site := range []string{"string", "list", "map"} {
		for _, mode := range []string{"measure", "write"} {
			t.Run(site+"-"+mode, func(t *testing.T) {
				out, runErr := run(mode, site)
				if !strings.Contains(string(out), "CALLING "+mode+" "+site) {
					t.Fatalf("the probe did not reach the writer:\n%s", out)
				}
				if runErr == nil {
					t.Fatalf("a zero in %s returned from %s:\n%s", site, mode, out)
				}
				if strings.Contains(string(out), "RETURNED") {
					t.Fatalf("a zero in %s did not assert under %s:\n%s", site, mode, out)
				}
				if !strings.Contains(string(out), "Assertion failed.") {
					t.Fatalf("a zero in %s did not hit the debug write assert under %s:\n%s", site, mode, out)
				}
			})
		}
	}
}

const zeroProbe = `using System;
using System.Text;
using Probe;

static class ZeroProbe
{
    const string input = "{\"s\":\"ab\",\"l\":[\"x\",\"y\"],\"m\":{\"k\":7}}";

    static int Main(string[] args)
    {
        string mode = args[0];
        string site = args[1];
        var value = new Root();
        var report = new TableReport();
        if (!Schema.RootFromJson(value, Encoding.UTF8.GetBytes(input), report))
        {
            Console.WriteLine("READ-FAILED");
            return 2;
        }
        if (site == "string") { value.S[0] = 0; }
        else if (site == "list") { value.L[0].Value[0] = 0; }
        else if (site == "map") { value.M[0].Key[0] = 0; }
        Console.WriteLine("CALLING " + mode + " " + site);
        if (mode == "measure")
        {
            Console.WriteLine("MEASURED " + Schema.RootToJsonMeasure(value));
        }
        else if (mode == "write")
        {
            byte[] buffer = new byte[4096];
            Console.WriteLine("WROTE " + Schema.RootToJson(value, buffer));
        }
        else
        {
            long n = Schema.RootToJsonMeasure(value);
            byte[] buffer = new byte[n];
            if (Schema.RootToJson(value, buffer) != n) { Console.WriteLine("CONTROL-SIZE-MISMATCH"); return 3; }
            string canon = Encoding.UTF8.GetString(buffer, 0, (int)n).TrimEnd('\n');
            Console.WriteLine("CANON:" + Convert.ToBase64String(Encoding.UTF8.GetBytes(canon)));
        }
        Console.WriteLine("RETURNED");
        return 0;
    }
}
`
