package cstable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ILL-FORMED TEXT IS DAMAGE ON THE MESSAGE FORM'S SKIP PATH TOO
// (docs/SPEC-TABLES.md §3.3). A writer announces a string field this reader
// does not declare, so the reader skips its payload; a payload that is not
// well-formed UTF-8, or that carries a zero byte, discards the batch exactly as
// it would on a named read. The engine's verdict on the same bytes is the
// oracle, and only verdicts are compared.

const messageSkipTextWriter = `package skiptext

fixed table Root {
	a int32
	s string(8)
}
`

const messageSkipTextReader = `package skiptext

fixed table Root {
	a int32
}
`

// messageSkipTextPayload is the writer's text, chosen so its bytes appear once
// in the batch: a string payload is byte-aligned on this form.
const messageSkipTextPayload = "okok"

func messageSkipTextUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("SkipText.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "SkipText.schema",
		Name:  "SkipText.schema",
		Base:  "SkipText",
		Bytes: []byte(src),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

type messageSkipTextCase struct {
	name string
	wire []byte
}

// messageSkipTextFixtures answers the reader's unit, the writer's
// announcement, and three batches: the writer's own, its text's first byte
// made 0xff (not UTF-8), and made 0x00 (a zero byte).
func messageSkipTextFixtures(t *testing.T) (*ir.Unit, []byte, []messageSkipTextCase) {
	t.Helper()
	writer := messageSkipTextUnit(t, messageSkipTextWriter)
	reader := messageSkipTextUnit(t, messageSkipTextReader)
	wm := tabletext.NewModel(writer)
	inst := wm.New(wm.Lookup("Root"))
	var placed tabletext.Report
	if !wm.Read(inst, []byte(`{"a":5,"s":"`+messageSkipTextPayload+`"}`), &placed) || !placed.Silent() {
		t.Fatalf("the writer's value did not place cleanly: %+v", placed)
	}
	good, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(good, []byte(messageSkipTextPayload))
	if at < 0 || bytes.Index(good[at+1:], []byte(messageSkipTextPayload)) >= 0 {
		t.Fatalf("the text is not once in the batch: %x", good)
	}
	notUTF8 := bytes.Clone(good)
	notUTF8[at] = 0xff
	zero := bytes.Clone(good)
	zero[at] = 0x00
	cases := []messageSkipTextCase{{"well_formed", good}, {"not_utf8", notUTF8}, {"zero_byte", zero}}
	return reader, tablewire.Announce(writer), cases
}

// messageSkipTextEngine is the engine's acceptance of each batch, read by the
// reader's own unit against the writer's announcement.
func messageSkipTextEngine(t *testing.T, reader *ir.Unit, ann []byte, cases []messageSkipTextCase) map[string]bool {
	t.Helper()
	rm := tabletext.NewModel(reader)
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(ann, &report); err != nil {
		t.Fatalf("the engine refused the announcement: %v", err)
	}
	accepted := map[string]bool{}
	for _, c := range cases {
		var r tabletext.Report
		inst := rm.New(rm.Lookup("Root"))
		_, ok, err := tablewire.DecodeMessages(rm, []*tabletext.Instance{inst}, c.wire, &vocabulary, &r)
		accepted[c.name] = ok && err == nil
	}
	return accepted
}

func runCSMessageSkipTextProbe(t *testing.T, reader *ir.Unit, ann []byte, cases []messageSkipTextCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	files, err := Generate(reader)
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
using Skiptext;

static class Program
{
    static int Main()
    {
`)
	fmt.Fprintf(&prog, "        byte[] ann = %s;\n", messageLoadFreshVerdictCSBytes(ann))
	prog.WriteString(`        var vocab = new TableVocabulary();
        var arep = new TableReport();
        if (Schema.AnnounceRead(vocab, ann, arep) != Schema.TableWire.Verdict.Ok || !vocab.Announced)
        {
            Console.WriteLine("AnnounceRead failed: " + arep.Reason);
            return 2;
        }
`)
	for _, c := range cases {
		fmt.Fprintf(&prog, "        {\n            var rep = new TableReport();\n            var values = new Root[] { new Root() };\n")
		fmt.Fprintf(&prog, "            var v = Schema.RootLoadMessages(values, %s, vocab, rep, out int n);\n", messageLoadFreshVerdictCSBytes(c.wire))
		fmt.Fprintf(&prog, "            Console.WriteLine(\"RESULT %s \" + v + \" \" + n);\n        }\n", c.name)
	}
	prog.WriteString(`        return 0;
    }
}
`)
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(prog.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "skiptext.dll")
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
	if err := os.WriteFile(filepath.Join(dir, "skiptext.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	return string(out), err
}

// messageSkipTextVerdicts answers, per case, whether the C# leg read the batch
// (Ok with one body).
func messageSkipTextVerdicts(t *testing.T, out string, cases []messageSkipTextCase) map[string]bool {
	t.Helper()
	read := map[string]bool{}
	for _, c := range cases {
		line, ok := messageLoadFreshVerdictLine(out, c.name)
		if !ok {
			t.Fatalf("%s DID NOT RUN: no RESULT line\n%s", c.name, out)
		}
		read[c.name] = line == "RESULT "+c.name+" Ok 1"
	}
	return read
}

func TestCSharpMessageSkipIllFormedText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	reader, ann, cases := messageSkipTextFixtures(t)
	engine := messageSkipTextEngine(t, reader, ann, cases)
	if !engine["well_formed"] || engine["not_utf8"] || engine["zero_byte"] {
		t.Fatalf("the engine's verdicts are not the rule's: %v", engine)
	}
	out, err := runCSMessageSkipTextProbe(t, reader, ann, cases, nil)
	if err != nil {
		t.Fatalf("csharp message skip probe failed: %v\n%s", err, out)
	}
	leg := messageSkipTextVerdicts(t, out, cases)
	for _, c := range cases {
		if leg[c.name] != engine[c.name] {
			t.Errorf("%s: the C# leg reads=%t, the engine reads=%t\n%s", c.name, leg[c.name], engine[c.name], out)
		}
	}
}

// The control takes the text check out of the generated skip path and
// requires the ill-formed batches to read, so the test above is red on it.
func TestCSharpMessageSkipIllFormedTextNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	reader, ann, cases := messageSkipTextFixtures(t)
	const textCheck = "if (!TextValid(r.Bytes.Slice((int)(r.At / 8), (int)length))) { return false; }"
	out, err := runCSMessageSkipTextProbe(t, reader, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			if s := string(data); strings.Contains(s, textCheck) {
				files[name] = []byte(strings.Replace(s, textCheck, "", 1))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected the generated C# message skip to check its text")
		}
	})
	if err != nil {
		t.Fatalf("control without the skip's text check failed to compile/run: %v\n%s", err, out)
	}
	leg := messageSkipTextVerdicts(t, out, cases)
	if !leg["well_formed"] || !leg["not_utf8"] || !leg["zero_byte"] {
		t.Fatalf("control without the skip's text check did not read the ill-formed batches:\n%s", out)
	}
}
