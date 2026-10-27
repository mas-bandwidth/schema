package cstable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const depthBoundarySchema = `package depthprobe

fixed table Root {
	a int32
}
`

type depthBoundaryCase struct {
	name     string
	family   string
	depth    int
	wire     []byte
	expectOK bool
}

func depthBoundaryUnit(t *testing.T) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("DepthProbe.schema", []byte(depthBoundarySchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  "DepthProbe.schema",
		Name:  "DepthProbe.schema",
		Base:  "DepthProbe",
		Bytes: []byte(depthBoundarySchema),
		AST:   ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

func depthBoundaryAppendLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, uint8(v)|0x80)
		v >>= 7
	}
	return append(out, uint8(v))
}

func depthBoundaryEncodeAnnouncement(buildVersion uint64, vocabBytes []byte) []byte {
	var out []byte
	out = append(out, ir.TableWireForm)
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, buildVersion)
	out = append(out, 2, uint8(ir.TableKindArray))
	var sub []byte
	sub = append(sub, uint8(ir.TableKindU8))
	sub = depthBoundaryAppendLeb(sub, uint64(len(vocabBytes)))
	sub = append(sub, vocabBytes...)
	out = depthBoundaryAppendLeb(out, uint64(len(sub)))
	out = append(out, sub...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}

func depthBoundaryEncodeBatch(t *testing.T, family string, depth, refBits int) []byte {
	t.Helper()
	var bits []uint8
	put := func(val uint64, width int) {
		for b := range width {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	switch family {
	case "body":
		for range depth {
			put(2, refBits)
		}
		for range depth {
			put(0, refBits)
		}
	case "union":
		for i := 0; i < depth+1; i++ {
			put(3, refBits)
		}
		put(0, refBits)
	case "array":
		for i := 0; i < depth-2; i++ {
			put(2, refBits)
		}
		put(4, refBits)
		for i := 0; i < depth-1; i++ {
			put(0, refBits)
		}
	default:
		t.Fatalf("unknown family %q", family)
	}
	put(1, refBits)
	put(42, 32)
	put(0, refBits)
	put(1, refBits)
	put(99, 32)
	put(0, refBits)

	out := make([]byte, 2+(len(bits)+7)/8)
	out[0] = ir.TableWireMessageForm
	out[1] = 1
	for i, bit := range bits {
		if bit != 0 {
			out[2+(i>>3)] |= uint8(1 << (i & 7))
		}
	}
	return out
}

func depthBoundaryFixtures(t *testing.T) (*ir.Unit, []byte, []depthBoundaryCase) {
	t.Helper()
	u := depthBoundaryUnit(t)
	st := u.Tables["Root"]
	if st == nil || len(st.Fields) != 1 {
		t.Fatalf("expected Root table with 1 field, got %+v", st)
	}
	entries := []ir.TableVocabularyEntry{
		ir.TableFieldEntry(st.Fields[0]),
		{Id: 0x9991, Kind: ir.TableKindTable},
		{Id: 0x9992, Kind: ir.TableKindUnion},
		{
			Id:   0x9993,
			Kind: ir.TableKindArray,
			Shape: ir.TableMessageShape{
				Min:   1,
				Max:   1,
				Elem:  ir.TableKindTable,
				Inner: &ir.TableMessageShape{},
			},
		},
	}
	refBits := ir.TableMessageRefBits(len(entries))
	unitRefBits := ir.TableMessageRefBits(len(ir.TableVocabulary(u)))
	if refBits != 3 || unitRefBits != 3 {
		t.Fatalf("refBits=%d unitRefBits=%d, want 3", refBits, unitRefBits)
	}
	vocabBytes := ir.TableVocabularyBytes(entries)
	ann := depthBoundaryEncodeAnnouncement(0x0102030405060708, vocabBytes)

	families := []string{"body", "union", "array"}
	depths := []int{127, 128, 129}
	var cases []depthBoundaryCase
	byName := map[string][]byte{}
	for _, family := range families {
		for _, d := range depths {
			wire := depthBoundaryEncodeBatch(t, family, d, refBits)
			name := fmt.Sprintf("%s_%d", family, d)
			byName[name] = wire
			cases = append(cases, depthBoundaryCase{
				name:     name,
				family:   family,
				depth:    d,
				wire:     wire,
				expectOK: d <= 128,
			})
			if d == 128 {
				trunc := append([]byte(nil), wire[:len(wire)-12]...)
				cases = append(cases, depthBoundaryCase{
					name:     name + "_truncated",
					family:   family,
					depth:    d,
					wire:     trunc,
					expectOK: false,
				})
			}
		}
	}
	if len(byName["union_129"]) != len(byName["union_128"]) || len(byName["union_129"]) >= len(byName["body_128"]) {
		t.Fatalf("expected len(union_129)=%d == len(union_128)=%d < len(body_128)=%d",
			len(byName["union_129"]), len(byName["union_128"]), len(byName["body_128"]))
	}
	return u, ann, cases
}

func depthBoundaryCSBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return "new byte[]{" + strings.Join(parts, ", ") + "}"
}

func runCSDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
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
using Depthprobe;

static class Program
{
    static bool failed = false;
    static void CheckCase(string name, TableVocabulary vocab, byte[] wire, bool expectOK)
    {
        var values = new Root[] { new Root { A = -7 }, new Root { A = -7 } };
        var rep = new TableReport();
        var verdict = Schema.RootLoadMessages(values, wire, vocab, rep, out int count);
        if (expectOK)
        {
            if (verdict == Schema.TableWire.Verdict.Ok && count == 2 && !rep.Malformed && !rep.Refused && rep.Unknown == 1 && values[0].A == 42 && values[1].A == 99)
            {
                Console.WriteLine("PASS " + name);
            }
            else
            {
                Console.WriteLine("FAIL " + name + ": verdict=" + verdict + " count=" + count + " malformed=" + rep.Malformed + " refused=" + rep.Refused + " unknown=" + rep.Unknown + " a0=" + values[0].A + " a1=" + values[1].A);
                failed = true;
            }
        }
        else
        {
            if (verdict == Schema.TableWire.Verdict.Damaged && count == 0 && rep.Malformed && !rep.Refused && (values[0].A == 0 || values[0].A == -7) && values[1].A == -7)
            {
                Console.WriteLine("PASS " + name);
            }
            else
            {
                Console.WriteLine("FAIL " + name + ": verdict=" + verdict + " count=" + count + " malformed=" + rep.Malformed + " refused=" + rep.Refused + " a0=" + values[0].A + " a1=" + values[1].A);
                failed = true;
            }
        }
    }

    static int Main()
    {
`)
	fmt.Fprintf(&prog, "        byte[] ann = %s;\n", depthBoundaryCSBytes(ann))
	prog.WriteString(`        var vocab = new TableVocabulary();
        var arep = new TableReport();
        if (Schema.AnnounceRead(vocab, ann, arep) != Schema.TableWire.Verdict.Ok || !vocab.Announced)
        {
            Console.WriteLine("AnnounceRead failed: " + arep.Reason);
            return 2;
        }
`)
	for _, tc := range cases {
		fmt.Fprintf(&prog, "        CheckCase(%q, vocab, %s, %v);\n", tc.name, depthBoundaryCSBytes(tc.wire), tc.expectOK)
	}
	prog.WriteString(`        return failed ? 1 : 0;
    }
}
`)
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(prog.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "depthprobe.dll")
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
	if err := os.WriteFile(filepath.Join(dir, "depthprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runCSDepthBoundaryProbe(t, u, ann, cases, nil)
	if err != nil {
		t.Fatalf("csharp message depth boundary failed:\n%s\n%v", out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}

	shiftedOut, shiftedErr := runCSDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "if (depth > 128)") {
				files[name] = []byte(strings.ReplaceAll(s, "if (depth > 128)", "if (depth > 256)"))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected generated C# files to contain 'if (depth > 128)'")
		}
	})
	if shiftedErr == nil {
		t.Fatalf("guard shift control (128 -> 256) unexpectedly passed:\n%s", shiftedOut)
	}
	for _, name := range []string{"body_129", "union_129", "array_129"} {
		if !strings.Contains(shiftedOut, "FAIL "+name) {
			t.Fatalf("guard shift control (128 -> 256) did not fail %s:\n%s", name, shiftedOut)
		}
	}
}
