package cstable

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestCSJsonCommentIsWellFormedUTF8 holds the generated C# reader to the JSON
// comment text rule (docs/SPEC-TABLES.md §16.2, §16.3): the bytes inside a
// line or block comment are well-formed UTF-8 and hold no NUL, so a comment
// that carries an ill-formed sequence or a NUL refuses the read. Every vector
// under testdata/wire/json-comment-text-rules is read by the compiler's own
// engine, the reference, and by the generated C#, and each verdict is compared
// with the vector's manifest verdict.
func TestCSJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	const schemaSrc = `package probe
table Root {
 value int32 = 7
 name string(32)
 experience uint32 = 0
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

	const vectorDir = "../../../testdata/wire/json-comment-text-rules"
	manifest, err := os.Open(filepath.Join(vectorDir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	type vector struct {
		name    string
		verdict string
		input   []byte
		engine  bool
	}
	var vectors []vector
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("bad manifest line %q", line)
		}
		name, verdict := cols[0], cols[1]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("bad manifest verdict %q for %s", verdict, name)
		}
		input, err := os.ReadFile(filepath.Join(vectorDir, name))
		if err != nil {
			t.Fatal(err)
		}
		vectors = append(vectors, vector{name: name, verdict: verdict, input: input})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("manifest contains no comment-rule vectors")
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("missing Root")
	}
	var caseFiles []string
	for i := range vectors {
		var report tabletext.Report
		inst := model.New(def)
		ok := model.Read(inst, vectors[i].input, &report)
		read := ok && !report.Refused
		if read && vectors[i].verdict != "read" {
			t.Errorf("%s: manifest expects read, engine refused: %+v", vectors[i].name, report)
		}
		if !read && vectors[i].verdict != "refused" {
			t.Errorf("%s: manifest expects refused, engine read: %+v", vectors[i].name, report)
		}
		vectors[i].engine = read
		p := filepath.Join(dir, fmt.Sprintf("case-%02d.json", i))
		if err := os.WriteFile(p, vectors[i].input, 0o600); err != nil {
			t.Fatal(err)
		}
		caseFiles = append(caseFiles, p)
	}

	const driverSrc = `using System;
using System.IO;

public static class CommentProbe
{
    public static int Main(string[] args)
    {
        foreach (string path in args)
        {
            var value = new Probe.Root();
            var report = new Probe.TableReport();
            bool ok = Probe.Schema.RootFromJson(value, File.ReadAllBytes(path), report);
            Console.WriteLine(ok ? "read" : "refused");
        }
        return 0;
    }
}
`
	main := filepath.Join(dir, "CommentProbe.cs")
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
		t.Fatalf("CommentProbe failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(vectors) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(vectors), out)
	}
	for i := range vectors {
		t.Run(vectors[i].name, func(t *testing.T) {
			got := strings.TrimSpace(lines[i])
			if got != "read" && got != "refused" {
				t.Fatalf("bad probe answer %q", got)
			}
			want := "refused"
			if vectors[i].engine {
				want = "read"
			}
			if got != want {
				t.Errorf("C# answered %s, engine answered %s on %x", got, want, vectors[i].input)
			}
		})
	}
}
