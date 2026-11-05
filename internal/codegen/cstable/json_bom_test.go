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

// TestJsonLeadingBOMBoundaryCS verifies that the C# JSON reader accepts
// exactly one leading UTF-8 BOM (0xEF 0xBB 0xBF) at byte offset 0 with zero
// report counters and the same canonical value/wire as plain JSON, while
// refusing double BOM, whitespace-before-BOM, truncated BOM, BOM inside the
// object or before a nested table, and BOM on an integer map key, and
// preserving U+FEFF inside string values (docs/SPEC-TABLES.md §16.2, §16.12).
func TestJsonLeadingBOMBoundaryCS(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")

	const schemaSrc = `package probe
table Sub {
 x int32 = 1
}
table Root {
 value int32 = 7
 tag string(32)
 sub Sub
 ids map[int32]int32
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

	const doc = "{\"value\":9,\"tag\":\"ok\"}\n"
	cases := []struct {
		name  string
		input []byte
	}{
		{"plain-empty", []byte("{}\n")},
		{"one-leading-bom-empty", []byte("\xef\xbb\xbf{}\n")},
		{"two-leading-boms-empty", []byte("\xef\xbb\xbf\xef\xbb\xbf{}\n")},
		{"space-before-bom-empty", []byte(" \xef\xbb\xbf{}\n")},
		{"truncated-bom-empty", []byte("\xef\xbb{}\n")},
		{"plain-populated", []byte(doc)},
		{"one-leading-bom-populated", []byte("\xef\xbb\xbf" + doc)},
		{"two-leading-boms-populated", []byte("\xef\xbb\xbf\xef\xbb\xbf" + doc)},
		{"space-before-bom-populated", []byte(" \xef\xbb\xbf" + doc)},
		{"truncated-bom-populated", []byte("\xef\xbb" + doc)},
		{"bom-inside-object", []byte("{\xef\xbb\xbf\"value\":9}\n")},
		{"nested-table-bom-inside", []byte("{\"sub\":{\xef\xbb\xbf\"x\":2}}\n")},
		{"map-int-key-bom", []byte("{\"ids\":{\"\\uFEFF1\":2}}\n")},
		{"field-string-raw-feff", []byte("{\"tag\":\"a\xef\xbb\xbfb\"}\n")},
		{"field-string-escaped-feff", []byte("{\"tag\":\"a\\uFEFFb\"}\n")},
	}

	type result struct {
		OK           bool
		Unknown      int
		KindMismatch int
		Widened      int
		Clamped      int
		Duplicate    int
		Malformed    bool
		Refused      bool
		Canon        string
		Wire         string
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	expected := make([]result, len(cases))
	var caseFiles []string
	for i, c := range cases {
		name := fmt.Sprintf("case-%02d.json", i)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, c.input, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)

		var report tabletext.Report
		inst := model.New(def)
		ok := model.Read(inst, c.input, &report)
		// The verdict, and on acceptance the value, is the cross-language contract;
		// counters are per-language diagnostics (docs/SPEC-TABLES.md §6.5).
		want := result{OK: ok}
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

public static class BomProbe
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
            else if (c < 0x20 || c == 0xfeff) sb.AppendFormat("\\u{0:x4}", (int)c);
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
                ",\"KindMismatch\":" + r.KindMismatch +
                ",\"Widened\":" + r.Widened +
                ",\"Clamped\":" + r.Clamped +
                ",\"Duplicate\":" + r.Duplicate +
                ",\"Malformed\":" + (r.Malformed ? "true" : "false") +
                ",\"Refused\":" + (r.Refused ? "true" : "false") +
                ",\"Canon\":" + Esc(canon) +
                ",\"Wire\":\"" + wire + "\"}");
        }
        return 0;
    }
}
`
	main := filepath.Join(dir, "BomProbe.cs")
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
		t.Fatalf("BomProbe failed: %v\n%s", err, out)
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
			got = result{OK: got.OK, Canon: got.Canon, Wire: got.Wire}
			if got != expected[i] {
				t.Errorf("input bytes %x\nC#: %+v\nGo: %+v", c.input, got, expected[i])
			}
		})
	}
}
