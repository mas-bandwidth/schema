package javatable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
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
		for b := 0; b < width; b++ {
			bits = append(bits, uint8((val>>b)&1))
		}
	}
	switch family {
	case "body":
		for i := 0; i < depth; i++ {
			put(2, refBits)
		}
		for i := 0; i < depth; i++ {
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

func depthBoundaryJavaBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("(byte)0x%02x", v)
	}
	return "new byte[]{" + strings.Join(parts, ", ") + "}"
}

func runJavaDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	javac, javaBin := javaTools(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("packet generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", u.Package)
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"--release", "17", "-nowarn", "-d", classes}
	var names []string
	for name := range files {
		keep := name == "TableFixed.java" ||
			name == "TableVocabulary.java" ||
			name == "TableMessage.java" ||
			strings.HasSuffix(name, "Fixed.java") ||
			strings.HasSuffix(name, "Message.java")
		if keep {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(srcDir, name)
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		args = append(args, p)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data, ok := pkt[name]; ok {
			p := filepath.Join(srcDir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			args = append(args, p)
		}
	}
	var driver strings.Builder
	fmt.Fprintf(&driver, "package %s;\n\npublic final class Driver {\n", u.Package)
	driver.WriteString(`    static boolean failed = false;
    static void checkCase(String name, TableVocabulary vocab, byte[] wire, boolean expectOK) {
        RootFixed.Value v0 = new RootFixed.Value();
        v0.a = -7;
        RootFixed.Value v1 = new RootFixed.Value();
        v1.a = -7;
        RootFixed.Value[] values = new RootFixed.Value[]{v0, v1};
        TableVocabulary.Report rep = new TableVocabulary.Report();
        int count = RootMessage.loadMessages(values, values.length, vocab, wire, rep);
        boolean ok = !rep.malformed && !rep.refused && count == 2;
        if (expectOK) {
            if (ok && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99) {
                System.out.println("PASS " + name);
            } else {
                System.out.println("FAIL " + name + ": count=" + count + " malformed=" + rep.malformed + " refused=" + rep.refused + " unknown=" + rep.unknown + " a0=" + values[0].a + " a1=" + values[1].a);
                failed = true;
            }
        } else {
            if (count == 0 && rep.malformed && !rep.refused && (values[0].a == 0 || values[0].a == -7) && values[1].a == -7) {
                System.out.println("PASS " + name);
            } else {
                System.out.println("FAIL " + name + ": count=" + count + " malformed=" + rep.malformed + " refused=" + rep.refused + " a0=" + values[0].a + " a1=" + values[1].a);
                failed = true;
            }
        }
    }
    public static void main(String[] args) {
`)
	fmt.Fprintf(&driver, "        byte[] ann = %s;\n", depthBoundaryJavaBytes(ann))
	driver.WriteString(`        TableVocabulary vocab = new TableVocabulary();
        vocab.reset(64);
        TableVocabulary.Report arep = new TableVocabulary.Report();
        if (!TableVocabulary.announceRead(vocab, ann, arep) || !vocab.announced) {
            throw new RuntimeException("announceRead failed");
        }
`)
	for _, tc := range cases {
		fmt.Fprintf(&driver, "        checkCase(%q, vocab, %s, %v);\n", tc.name, depthBoundaryJavaBytes(tc.wire), tc.expectOK)
	}
	driver.WriteString(`        if (failed) {
            System.exit(1);
        }
    }
}
`)
	driverPath := filepath.Join(srcDir, "Driver.java")
	if err := os.WriteFile(driverPath, []byte(driver.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	args = append(args, driverPath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, u.Package+".Driver").CombinedOutput()
	return string(out), err
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runJavaDepthBoundaryProbe(t, u, ann, cases, nil)
	if err != nil {
		t.Fatalf("java message depth boundary failed:\n%s\n%v", out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}

	shiftedOut, shiftedErr := runJavaDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "if (depth > 128") {
				files[name] = []byte(strings.ReplaceAll(s, "if (depth > 128", "if (depth > 256"))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected generated Java files to contain 'if (depth > 128'")
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
