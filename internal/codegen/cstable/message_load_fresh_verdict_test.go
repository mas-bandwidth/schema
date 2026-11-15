package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageLoadFreshVerdictSchema = `package freshprobe

fixed table Root {
	a int32
}
`

func messageLoadFreshVerdictUnit(t *testing.T) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("FreshProbe.schema", []byte(messageLoadFreshVerdictSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "FreshProbe.schema",
		Name:  "FreshProbe.schema",
		Base:  "FreshProbe",
		Bytes: []byte(messageLoadFreshVerdictSchema),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func messageLoadFreshVerdictBatch(refBits int, value uint64) []byte {
	var bits []uint8
	put := func(val uint64, width int) {
		for b := range width {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	put(1, refBits)
	put(value, 32)
	put(0, refBits)

	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 0
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func messageLoadFreshVerdictFixtures(t *testing.T) (*ir.Unit, []byte, []byte, []byte) {
	t.Helper()
	u := messageLoadFreshVerdictUnit(t)
	st := u.Tables["Root"]
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("expected Root table with 1 field, got %+v", st)
	}
	entries := []ir.TableVocabularyEntry{ir.TableFieldEntry(st.Fields[0])}
	refBits := ir.TableMessageRefBits(len(entries))
	if refBits != 1 {
		t.Fatalf("refBits=%d, want 1", refBits)
	}
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, ir.TableVocabularyBytes(entries))
	good := messageLoadFreshVerdictBatch(refBits, 42)
	refused := []byte{3}
	return u, ann, good, refused
}

func messageLoadFreshVerdictCSBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return "new byte[]{" + strings.Join(parts, ", ") + "}"
}

func messageLoadFreshVerdictLine(out, name string) (string, bool) {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "RESULT "+name+" ") {
			return line, true
		}
	}
	return "", false
}

func runCSMessageLoadFreshVerdictProbe(t *testing.T, u *ir.Unit, ann, good, refused []byte, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	var prog strings.Builder
	prog.WriteString(`using System;
using Freshprobe;

static class Program
{
    static void Emit(string name, Schema.TableWire.Verdict verdict, int count)
    { Console.WriteLine("RESULT " + name + " " + verdict + " " + count); }

    static int Main()
    {
`)
	fmt.Fprintf(&prog, "        byte[] ann = %s;\n", messageLoadFreshVerdictCSBytes(ann))
	fmt.Fprintf(&prog, "        byte[] good = %s;\n", messageLoadFreshVerdictCSBytes(good))
	fmt.Fprintf(&prog, "        byte[] refused = %s;\n", messageLoadFreshVerdictCSBytes(refused))
	prog.WriteString(`        var vocab = new TableVocabulary();
        var arep = new TableReport();
        if (Schema.AnnounceRead(vocab, ann, arep) != Schema.TableWire.Verdict.Ok || !vocab.Announced)
        {
            Console.WriteLine("AnnounceRead failed: " + arep.Reason);
            return 2;
        }
        var rep = new TableReport();
        var values = new Root[] { new Root { A = -7 } };
        var v1 = Schema.RootLoadMessages(values, good, vocab, rep, out int c1);
        Emit("fresh_read", v1, c1);
        var v2 = Schema.RootLoadMessages(values, refused, vocab, rep, out int c2);
        Emit("refused_read", v2, c2);
        var v3 = Schema.RootLoadMessages(values, good, vocab, rep, out int c3);
        Emit("re_read", v3, c3);
        return 0;
    }
}
`)
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(prog.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "freshprobe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, probePath)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if compileOut, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, compileOut)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "freshprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func checkMessageLoadFreshVerdict(t *testing.T, out string) {
	t.Helper()
	cases := []struct {
		name    string
		verdict string
		count   int
	}{
		{"fresh_read", "Ok", 1},
		{"refused_read", "Refused", 0},
		{"re_read", "Ok", 1},
	}
	for _, c := range cases {
		line, ok := messageLoadFreshVerdictLine(out, c.name)
		if !ok {
			t.Fatalf("%s DID NOT RUN: no RESULT line\n%s", c.name, out)
		}
		want := fmt.Sprintf("RESULT %s %s %d", c.name, c.verdict, c.count)
		if line != want {
			t.Fatalf("%s: got %q, want %q\n%s", c.name, line, want, out)
		}
	}
}

func TestCSharpMessageLoadFreshVerdict(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	u, ann, good, refused := messageLoadFreshVerdictFixtures(t)
	out, err := runCSMessageLoadFreshVerdictProbe(t, u, ann, good, refused, nil)
	if err != nil {
		t.Fatalf("csharp message load probe failed: %v\n%s", err, out)
	}
	checkMessageLoadFreshVerdict(t, out)
}

func TestCSharpMessageLoadFreshVerdictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	u, ann, good, refused := messageLoadFreshVerdictFixtures(t)
	controlOut, controlErr := runCSMessageLoadFreshVerdictProbe(t, u, ann, good, refused, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "count = 0; report.Refused = false; report.Reason = null; ") {
				files[name] = []byte(strings.Replace(s, "count = 0; report.Refused = false; report.Reason = null; ", "count = 0; ", 1))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected generated C# MessageLoad to reset the verdict at its head")
		}
	})
	if controlErr != nil {
		t.Fatalf("control without head reset failed to compile/run: %v\n%s", controlErr, controlOut)
	}
	line, ok := messageLoadFreshVerdictLine(controlOut, "re_read")
	if !ok || !strings.Contains(line, "Refused") {
		t.Fatalf("control without head reset did not fail re_read:\n%s", controlOut)
	}
}
