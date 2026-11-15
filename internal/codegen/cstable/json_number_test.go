package cstable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestCSJsonNumberAnyLength verifies that a JSON number token's length is
// never why a read refuses: a valid number is read by its value however many
// bytes it spans, whether it lands in a field or under an unknown key, and an
// integer outside the field's domain still refuses (docs/SPEC-TABLES.md §16.2
// Numbers).
func TestCSJsonNumberAnyLength(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	const schemaSrc = `package probe
table Root {
    experience uint32 = 0
    precision  float64
}
`
	u := unitFrom(t, schemaSrc)
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

	zeros := func(n int) string { return strings.Repeat("0", n) }
	cases := []struct {
		name         string
		input        []byte
		checkUnknown bool
	}{
		{"experience-509-zeros", []byte(`{"experience":1.` + zeros(509) + "}\n"), false},
		{"experience-510-zeros", []byte(`{"experience":1.` + zeros(510) + "}\n"), false},
		{"experience-511-zeros", []byte(`{"experience":1.` + zeros(511) + "}\n"), false},
		{"experience-600-zeros", []byte(`{"experience":1.` + zeros(600) + "}\n"), false},
		{"precision-600-zeros", []byte(`{"precision":1.` + zeros(600) + "}\n"), false},
		{"unknown-5000-digit-integer", []byte(`{"unlisted":1` + strings.Repeat("0", 4999) + "}\n"), true},
		{"experience-600-digit-integer", []byte(`{"experience":1` + zeros(599) + "}\n"), false},
	}

	type result struct {
		OK      bool
		Unknown int
		Canon   string
		Wire    string
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]result, len(cases))
	var caseFiles []string
	for i, c := range cases {
		p := filepath.Join(dir, fmt.Sprintf("case-%02d.json", i))
		if err := os.WriteFile(p, c.input, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)

		var report tabletext.Report
		inst := model.New(def)
		ok := model.Read(inst, c.input, &report)
		want := result{OK: ok, Unknown: report.Unknown}
		if ok {
			text, err := model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
			want.Canon = strings.TrimRight(string(text), "\n")
			want.Wire = hex.EncodeToString(wire)
		}
		expected[i] = want
	}

	const driverSrc = `using System;
using System.IO;
using System.Text;

public static class NumberProbe
{
    static string Hex(ReadOnlySpan<byte> bytes)
    {
        var sb = new StringBuilder(bytes.Length * 2);
        for (int i = 0; i < bytes.Length; i++) sb.Append(bytes[i].ToString("x2"));
        return sb.ToString();
    }

    static string Esc(string s)
    {
        var sb = new StringBuilder();
        sb.Append('"');
        foreach (char c in s)
        {
            if (c == '"') sb.Append("\\\"");
            else if (c == '\\') sb.Append("\\\\");
            else if (c == '\n') sb.Append("\\n");
            else if (c == '\r') sb.Append("\\r");
            else if (c == '\t') sb.Append("\\t");
            else if (c < 0x20) sb.AppendFormat("\\u{0:x4}", (int)c);
            else sb.Append(c);
        }
        sb.Append('"');
        return sb.ToString();
    }

    public static int Main(string[] args)
    {
        foreach (string path in args)
        {
            byte[] input = File.ReadAllBytes(path);
            var v = new Probe.Root();
            var r = new Probe.TableReport();
            bool ok = Probe.Schema.RootFromJson(v, input, r);
            string canon = "";
            string wire = "";
            if (ok)
            {
                long jlen = Probe.Schema.RootToJsonMeasure(v);
                byte[] jbuf = new byte[jlen];
                long jwrote = Probe.Schema.RootToJson(v, jbuf);
                canon = Encoding.UTF8.GetString(jbuf, 0, (int)jwrote).TrimEnd('\n');

                long wlen = Probe.Schema.RootMeasure(v);
                byte[] wbuf = new byte[wlen];
                long wwrote = Probe.Schema.RootSave(v, wbuf);
                wire = Hex(wbuf.AsSpan(0, (int)wwrote));
            }
            Console.WriteLine(
                "{\"OK\":" + (ok ? "true" : "false") +
                ",\"Unknown\":" + r.Unknown +
                ",\"Canon\":" + Esc(canon) +
                ",\"Wire\":\"" + wire + "\"}");
        }
        return 0;
    }
}
`
	main := filepath.Join(dir, "NumberProbe.cs")
	if err := os.WriteFile(main, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(dir, "probe.dll")}
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
	runArgs := append([]string{"exec", filepath.Join(dir, "probe.dll")}, caseFiles...)
	run := exec.Command(csDotnet(), runArgs...)
	run.Env = csDotnetEnv(dir, dir)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("NumberProbe failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got result
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", lines[i], err)
			}
			got = result{OK: got.OK, Unknown: got.Unknown, Canon: got.Canon, Wire: got.Wire}
			if got.OK != expected[i].OK || got.Canon != expected[i].Canon || got.Wire != expected[i].Wire {
				t.Errorf("input bytes %d\nC#: %+v\nGo: %+v", len(c.input), got, expected[i])
			}
			if c.checkUnknown && (got.Unknown != 1 || expected[i].Unknown != 1) {
				t.Errorf("unknown count: C# %d, Go %d, want one each", got.Unknown, expected[i].Unknown)
			}
		})
	}
}
