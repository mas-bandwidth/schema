package cstable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCSharpSkippedValueIsValidated holds the shared skipped-value vectors
// through the generated C# reader (docs/SPEC-TABLES.md §3.3, §4, §16.3;
// docs/rulings.json skipped-value-is-validated): a value the reader SKIPS is
// held to what a read of it would hold. Every manifest row is one case: a
// message batch is loaded against the writer's announcement (a nested body's
// reserved or repeated id, a string's text, a float's finiteness, a quantized
// index, a count, an enum-keyed slot's key), and a JSON text is read at the
// reader unit's Root (a value under a key the reader cannot name, and every
// object key, held to the placed text's rules). Only the VERDICT is compared.
func TestCSharpSkippedValueIsValidated(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")

	rows := append(csSkippedValueRows(t), csSkippedValueWalkRows(t)...)
	out, code := csSkippedValueRun(t, rows)
	if code != 0 || !strings.Contains(out, "SKIPPED-VALUE PASS") {
		t.Fatalf("the C# reader did not hold every skipped-value row (exit %d)\n%s", code, out)
	}
	for _, row := range rows {
		if !strings.Contains(out, "ROW "+row.file+": ok") {
			t.Fatalf("%s DID NOT RUN: the assembly did not build, or the name moved\n%s", row.file, out)
		}
	}
}

type csSkippedValueRow struct {
	file, form, verdict string
	// ann and wire, when set, carry a message row's announcement and batch
	// inline instead of naming a file under testdata/wire/skipped-value/
	ann, wire []byte
}

// csSkippedValueWalkRows are message rows whose skipped entry is an array
// walked element by element (docs/SPEC-TABLES.md §3.3, "A WALKED ARRAY ELEMENT
// THAT CONSUMES NO BITS REFUSES THE READ"). The entry is an array of up to 4
// strings whose max is 0: a string's length then has width zero, and once the
// walk is byte-aligned an element rides no bits. A walk of two or more
// elements reaches an element that consumes nothing and refuses, however few
// bits its count claims; a count of zero walks nothing and reads, and a count
// of one reads, its one element stepping over the alignment's pad. The Go
// engine is held to the same verdict for the same bytes, so the rows cannot
// drift from the reference.
func csSkippedValueWalkRows(t *testing.T) []csSkippedValueRow {
	t.Helper()
	u := unitFrom(t, csSkippedValueReader(t))
	hostile := ir.TableVocabularyEntry{
		Id:   0x5a5a5a5a00000002,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min:   0,
			Max:   4,
			Elem:  ir.TableKindString,
			Inner: &ir.TableMessageShape{Max: 0},
		},
	}
	entries := append(tablewire.MessageVocabulary(u), hostile)
	vocabulary := ir.TableVocabularyBytes(entries)
	ann := []byte{ir.TableWireForm, 1, uint8(ir.TableKindU64)}
	ann = binary.LittleEndian.AppendUint64(ann, ir.BuildVersion(u))
	ann = append(ann, 2, uint8(ir.TableKindArray))
	sub := []byte{uint8(ir.TableKindU8)}
	sub = binary.AppendUvarint(sub, uint64(len(vocabulary)))
	sub = append(sub, vocabulary...)
	ann = binary.AppendUvarint(ann, uint64(len(sub)))
	ann = append(ann, sub...)
	ann = append(ann, 0)
	ann = binary.LittleEndian.AppendUint64(ann, ir.TableBuildVersionWireId)
	ann = binary.LittleEndian.AppendUint64(ann, ir.TableMessageVocabularyWireId)
	ann = binary.LittleEndian.AppendUint64(ann, 2)

	slot, refBits := uint64(len(entries)), ir.TableMessageRefBits(len(entries))
	width := ir.TableMessageBitsRequired(0, 4)
	batch := func(count uint64) []byte {
		out := []byte{ir.TableWireMessageForm, 0}
		var acc byte
		var at int
		put := func(v uint64, bits int) {
			for i := range bits {
				if (v>>i)&1 != 0 {
					acc |= 1 << at
				}
				at++
				if at == 8 {
					out = append(out, acc)
					acc, at = 0, 0
				}
			}
		}
		put(slot, refBits)
		put(count, width)
		// the first string aligns the walk; each empty string after it
		// rides no bits, so the body's terminator follows the pad
		if count > 0 && at > 0 {
			put(0, 8-at)
		}
		put(0, refBits)
		if at > 0 {
			out = append(out, acc)
		}
		return out
	}

	var announced tablewire.Vocabulary
	var announceReport tabletext.Report
	if err := announced.AnnounceRead(ann, &announceReport); err != nil {
		t.Fatalf("the Go engine refused the walk rows' announcement: %v %+v", err, announceReport)
	}
	model := tabletext.NewModel(u)
	var rows []csSkippedValueRow
	// one string reads where its alignment steps over pad bits, the control
	// that a walk which moves is never refused
	if (refBits+width)%8 == 0 {
		t.Fatalf("the walk rows need an unaligned count: refBits %d, width %d", refBits, width)
	}
	for _, count := range []uint64{0, 1, 2, 4} {
		verdict := "refused"
		if count < 2 {
			verdict = "read"
		}
		wire := batch(count)
		var report tabletext.Report
		ok, err := tablewire.DecodeMessage(model, model.New(u.Tables["Root"]), wire, &announced, &report)
		if engine := ok && err == nil; engine != (verdict == "read") {
			t.Fatalf("walk row of count %d: the Go engine read=%v (err %v), the row says %s", count, engine, err, verdict)
		}
		rows = append(rows, csSkippedValueRow{
			file: fmt.Sprintf("walked-zero-bit-element-count-%d", count), form: "message", verdict: verdict,
			ann: ann, wire: wire,
		})
	}
	return rows
}

func csSkippedValueRows(t *testing.T) []csSkippedValueRow {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "skipped-value")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []csSkippedValueRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("MANIFEST.tsv:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", i+1, line)
		}
		if f[1] != "json" && f[1] != "message" {
			t.Fatalf("MANIFEST.tsv:%d: form %q, want json or message", i+1, f[1])
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[2])
		}
		rows = append(rows, csSkippedValueRow{file: f[0], form: f[1], verdict: f[2]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// csSkippedValueReader is the reader unit every row is read at; it declares
// `known` only, so the writer's other fields are skipped.
func csSkippedValueReader(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "skipped-value", "reader", "Reader.schema"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func csSkippedValueRun(t *testing.T, rows []csSkippedValueRow) (string, int) {
	t.Helper()
	dir := t.TempDir()

	u := unitFrom(t, csSkippedValueReader(t))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
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

	driver := filepath.Join(dir, "SkippedValueProbe.cs")
	if err := os.WriteFile(driver, []byte(csSkippedValueDriver(rows)), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)

	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "skippedvalue.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return string(compileOut), 1
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "skippedvalue.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	vectors, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "skipped-value"))
	if err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app, vectors)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	var buf bytes.Buffer
	buf.Write(compileOut)
	runOut, runErr := run.CombinedOutput()
	buf.Write(runOut)
	code := 0
	if runErr != nil {
		code = 1
	}
	return buf.String(), code
}

func csSkippedValueDriver(rows []csSkippedValueRow) string {
	var b strings.Builder
	b.WriteString(`// generated by internal/codegen/cstable's skipped-value harness: every
// manifest row goes through the generated C# reader in gen/, message rows
// against the writer's announcement and JSON rows at the reader's Root.
using System;
using System.IO;

static class SkippedValueProbe
{
    sealed class Row
    {
        public string File, Form, Verdict;
        public byte[] Ann, Wire;
        public Row(string file, string form, string verdict, byte[] ann = null, byte[] wire = null) { File = file; Form = form; Verdict = verdict; Ann = ann; Wire = wire; }
    }

    static readonly Row[] rows = new Row[]
    {
`)
	for _, row := range rows {
		if row.wire != nil {
			fmt.Fprintf(&b, "        new Row(%q, %q, %q, %s, %s),\n", row.file, row.form, row.verdict, depthBoundaryCSBytes(row.ann), depthBoundaryCSBytes(row.wire))
			continue
		}
		fmt.Fprintf(&b, "        new Row(%q, %q, %q),\n", row.file, row.form, row.verdict)
	}
	b.WriteString(`    };

    static int bad;

    static void Fail(string row, string why) { Console.WriteLine("ROW " + row + ": FAILED " + why); bad++; }
    static void Ok(string row) { Console.WriteLine("ROW " + row + ": ok"); }

    static int Main(string[] args)
    {
        if (args.Length != 1) { Console.WriteLine("PROBE FAILED: usage: app <dir>"); return 2; }
        string dir = args[0];
        byte[] announcement = File.ReadAllBytes(Path.Combine(dir, "announcement.bin"));
        var vocabulary = new Skippedreader.TableVocabulary();
        var announceReport = new Skippedreader.TableReport();
        bool announced = Skippedreader.Schema.AnnounceRead(vocabulary, announcement, announceReport) == Skippedreader.Schema.TableWire.Verdict.Ok;

        foreach (Row row in rows)
        {
            bool read;
            if (row.Wire != null)
            {
                var inlineVocabulary = new Skippedreader.TableVocabulary();
                var inlineAnnounce = new Skippedreader.TableReport();
                var report = new Skippedreader.TableReport();
                var values = new Skippedreader.Root[] { new Skippedreader.Root() };
                int count;
                read = Skippedreader.Schema.AnnounceRead(inlineVocabulary, row.Ann, inlineAnnounce) == Skippedreader.Schema.TableWire.Verdict.Ok &&
                    Skippedreader.Schema.RootLoadMessages(values, row.Wire, inlineVocabulary, report, out count) == Skippedreader.Schema.TableWire.Verdict.Ok;
                if (read == (row.Verdict == "read")) { Ok(row.File); }
                else { Fail(row.File, "read=" + read + ", want " + row.Verdict); }
                continue;
            }
            byte[] data = File.ReadAllBytes(Path.Combine(dir, row.File));
            if (row.Form == "json")
            {
                var report = new Skippedreader.TableReport();
                read = Skippedreader.Schema.RootFromJson(new Skippedreader.Root(), data, report);
            }
            else if (row.File == "announcement.bin")
            {
                read = announced;
            }
            else
            {
                var report = new Skippedreader.TableReport();
                var values = new Skippedreader.Root[] { new Skippedreader.Root() };
                int count;
                read = Skippedreader.Schema.RootLoadMessages(values, data, vocabulary, report, out count) == Skippedreader.Schema.TableWire.Verdict.Ok;
            }
            bool wantRead = row.Verdict == "read";
            if (read == wantRead) { Ok(row.File); }
            else { Fail(row.File, "read=" + read + ", want " + row.Verdict); }
        }
        Console.WriteLine(bad == 0 ? "SKIPPED-VALUE PASS" : "SKIPPED-VALUE FAIL");
        return bad == 0 ? 0 : 1;
    }
}
`)
	return b.String()
}
