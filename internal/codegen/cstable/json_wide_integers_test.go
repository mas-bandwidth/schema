package cstable

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

// jsonIntegersSchema is the unit testdata/wire/json-integers/ reads: an int32,
// an int64, a uint64, a ranged int128, a ranged int64, a wide fixed-point field
// and a map keyed by uint64 — every integer storage width the two JSON integer
// rules name (docs/SPEC-TABLES.md §16.2).
const jsonIntegersSchema = `package jsonintegers

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105727, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
enum Key { A, B }
union Pick {
    count int64
    name string(8)
}
table Sites {
    small int32
    field int64
    elems []int64
    maybe ?int64
    pick Pick
    values map[uint64]int64
    keyed [Key]int64
    u128 uint128
    b33 bits(33)
    f128 fixed(64, 64) | min = -10, max = 10
}
`

// TestCSharpJsonWideIntegersAreStrings holds testdata/wire/json-integers/
// through the C# writer and reader: a kind wider than 32 bits is written as a
// JSON string of decimal digits (docs/rulings.json
// json-wide-integers-are-strings), reads only as a string, and every
// integer field takes an integer token only — no fraction, no exponent, no
// negative zero — with its range tested on the exact value (docs/rulings.json
// json-integer-token-only, docs/SPEC-TABLES.md §16.2, §16.14 W10). The verdict
// and bare wide number tokens report kind_mismatch.
func TestCSharpJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vectors' manifest: %v", err)
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatalf("the canonical text: %v", err)
	}

	u := unitFrom(t, jsonIntegersSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	packet, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	out := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}

	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "serialize.cs", "src", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	type row struct {
		name        string
		json        string
		verdict     string
		root        string
		canon       string
		numberToken bool
	}
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want five columns, got %d", dir, i+1, len(fields))
		}
		if fields[3] != "read" && fields[3] != "refused" {
			t.Fatalf("%s/MANIFEST.tsv:%d: verdict %q, want read or refused", dir, i+1, fields[3])
		}
		rows = append(rows, row{name: fields[0], json: fields[2], verdict: fields[3], root: "Integers", canon: string(canonical)})
	}
	if len(rows) == 0 {
		t.Fatal("no manifest rows read; the gate would pass over an empty set")
	}

	tokens, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	numberRows := 0
	for line := range strings.SplitSeq(string(tokens), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) != 5 || f[3] != "refused" {
			t.Fatalf("invalid NUMBER-TOKEN row %q", line)
		}
		rows = append(rows, row{name: f[0], json: f[2], verdict: f[3], root: "Integers", numberToken: true})
		numberRows++
	}
	if numberRows != 13 {
		t.Fatalf("got %d number-token rows, want 13", numberRows)
	}
	sites := []struct {
		name, text string
		read       bool
	}{
		{"strings", `{"small":7,"field":"1","elems":["2"],"maybe":"3","pick":{"count":"4"},"values":{"1":"5"},"keyed":{"A":"6"},"u128":"340282366920938463463374607431768211455","b33":"8589934591","f128":"0.5"}`, true},
		{"field", `{"field":1}`, false}, {"element", `{"elems":[1]}`, false},
		{"optional", `{"maybe":1}`, false}, {"arm", `{"pick":{"count":1}}`, false},
		{"map", `{"values":{"1":1}}`, false}, {"keyed", `{"keyed":{"A":1}}`, false},
		{"u128", `{"u128":1}`, false}, {"bits33", `{"b33":1}`, false}, {"fixed128", `{"f128":0.5}`, false},
	}
	model := tabletext.NewModel(u)
	for _, tc := range sites {
		inst := model.New(model.Lookup("Sites"))
		var report tabletext.Report
		ok := model.Read(inst, []byte(tc.text), &report)
		if ok != tc.read {
			t.Fatalf("site %s: engine verdict %t", tc.name, ok)
		}
		path := filepath.Join(out, "site-"+tc.name+".json")
		if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
			t.Fatal(err)
		}
		r := row{name: "site-" + tc.name, json: path, root: "Sites", verdict: "refused", numberToken: !tc.read}
		if tc.read {
			b, err := model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			r.verdict = "read"
			r.canon = string(b)
		}
		rows = append(rows, r)
	}

	var paths []string
	for _, r := range rows {
		if r.root == "Sites" {
			paths = append(paths, r.json)
		} else {
			paths = append(paths, filepath.Join(dir, r.json))
		}
	}

	const driverSrc = `using System;
using System.IO;
using System.Text;

public static class WideIntegerProbe
{
    public static int Main(string[] args)
    {
        foreach (string path in args)
        {
            byte[] input = File.ReadAllBytes(path);
            var v = new Jsonintegers.Integers();
            var r = new Jsonintegers.TableReport();
            bool sites = Path.GetFileName(path).StartsWith("site-");
            var site = new Jsonintegers.Sites();
            bool ok = sites ? Jsonintegers.Schema.SitesFromJson(site, input, r) : Jsonintegers.Schema.IntegersFromJson(v, input, r);
            string canon = "";
            if (ok)
            {
                long jlen = sites ? Jsonintegers.Schema.SitesToJsonMeasure(site) : Jsonintegers.Schema.IntegersToJsonMeasure(v);
                if (jlen < 0) { Console.WriteLine("{\"Error\":\"measure refused\"}"); continue; }
                byte[] jbuf = new byte[jlen];
                long jwrote = sites ? Jsonintegers.Schema.SitesToJson(site, jbuf) : Jsonintegers.Schema.IntegersToJson(v, jbuf);
                if (jwrote != jlen) { Console.WriteLine("{\"Error\":\"write short\"}"); continue; }
                // the writer emits exactly one trailing newline (§16.14 W2);
                // the committed canonical text is the value's bytes without it
                int kept = jbuf.Length;
                if (kept > 0 && jbuf[kept - 1] == (byte)'\n') { kept--; }
                canon = Convert.ToBase64String(jbuf, 0, kept);
            }
            Console.WriteLine(
                "{\"OK\":" + (ok ? "true" : "false") +
                ",\"Refused\":" + (r.Refused ? "true" : "false") +
                ",\"Reason\":\"" + (r.Reason ?? "") + "\"" +
                ",\"Canon\":\"" + canon + "\"}");
        }
        return 0;
    }
}
`
	main := filepath.Join(out, "WideIntegerProbe.cs")
	if err := os.WriteFile(main, []byte(driverSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	buildArgs := []string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(out, "probe.dll")}
	buildArgs = append(buildArgs, sources...)
	for _, r := range refs {
		buildArgs = append(buildArgs, "-r:"+r)
	}
	build := exec.Command(csDotnet(), buildArgs...)
	build.Env = csDotnetEnv(out, out)
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", b)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, runtime)
	if err := os.WriteFile(filepath.Join(out, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}

	runArgs := append([]string{"exec", filepath.Join(out, "probe.dll")}, paths...)
	run := exec.Command(csDotnet(), runArgs...)
	run.Env = csDotnetEnv(out, out)
	got, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("WideIntegerProbe failed: %v\n%s", err, got)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != len(rows) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(rows), got)
	}
	type result struct {
		OK      bool
		Refused bool
		Canon   string
		Error   string
		Reason  string
	}
	for i, r := range rows {
		var res result
		if err := json.Unmarshal([]byte(lines[i]), &res); err != nil {
			t.Fatalf("unmarshal %q: %v", lines[i], err)
		}
		t.Run(r.name, func(t *testing.T) {
			if res.Error != "" {
				t.Fatalf("%s: %s", r.name, res.Error)
			}
			switch r.verdict {
			case "read":
				if !res.OK || res.Refused {
					t.Fatalf("%s: C# refused a vector the corpus reads", r.name)
				}
				text, err := base64.StdEncoding.DecodeString(res.Canon)
				if err != nil {
					t.Fatalf("%s: decoded canonical text: %v", r.name, err)
				}
				if string(text) != r.canon {
					t.Errorf("%s: C# wrote a different canonical text\nC#:\n%s\nwant:\n%s", r.name, text, r.canon)
				}
			case "refused":
				if res.OK || !res.Refused {
					t.Fatalf("%s: C# accepted a vector the corpus refuses", r.name)
				}
				if r.numberToken && res.Reason != "kind_mismatch" && !strings.HasPrefix(res.Reason, "kind_mismatch: ") {
					t.Errorf("%s: reason %q, want kind_mismatch", r.name, res.Reason)
				}
			}
		})
	}
}
