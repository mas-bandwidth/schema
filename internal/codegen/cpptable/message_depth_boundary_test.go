package cpptable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

func depthBoundaryCppBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02x", v)
	}
	return strings.Join(parts, ", ")
}

func runCppDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, p)
		}
	}
	var src strings.Builder
	src.WriteString(`#include "DepthProbeTable.h"
#include <cstdio>
#include <cstdint>
#include <cstring>
using namespace depthprobe;

static bool failed = false;

static void check_case(const char * name, const TableVocabulary & vocab, const uint8_t * wire, int64_t len, bool expect_ok)
{
    Root values[2];
    RootReset(values[0]);
    RootReset(values[1]);
    values[0].a = -7;
    values[1].a = -7;
    int64_t count = 2;
    TableReport rep;
    memset(&rep, 0, sizeof(rep));
    bool ok = RootLoadMessages(values, &count, vocab, wire, len, &rep);
    if (expect_ok) {
        if (ok && count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d unknown=%d a0=%d a1=%d\n",
                name, (int)ok, (long long)count, (int)rep.malformed, (int)rep.refused, (int)rep.unknown, (int)values[0].a, (int)values[1].a);
            failed = true;
        }
    } else {
        if (!ok && count == 0 && rep.malformed && !rep.refused && (values[0].a == 0 || values[0].a == -7) && values[1].a == -7) {
            printf("PASS %s\n", name);
        } else {
            printf("FAIL %s: ok=%d count=%lld malformed=%d refused=%d a0=%d a1=%d\n",
                name, (int)ok, (long long)count, (int)rep.malformed, (int)rep.refused, (int)values[0].a, (int)values[1].a);
            failed = true;
        }
    }
}

int main(void)
{
`)
	fmt.Fprintf(&src, "    static const uint8_t ann[] = { %s };\n", depthBoundaryCppBytes(ann))
	src.WriteString(`    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocab(entries, kTableMessageEntriesHere);
    TableReport arep;
    memset(&arep, 0, sizeof(arep));
    if (!AnnounceRead(vocab, ann, (int64_t)sizeof(ann), &arep) || !vocab.announced) {
        printf("AnnounceRead failed\n");
        return 2;
    }
`)
	for i, tc := range cases {
		fmt.Fprintf(&src, "    static const uint8_t w%d[] = { %s };\n", i, depthBoundaryCppBytes(tc.wire))
		fmt.Fprintf(&src, "    check_case(%q, vocab, w%d, (int64_t)sizeof(w%d), %v);\n", tc.name, i, i, tc.expectOK)
	}
	src.WriteString(`    return failed ? 1 : 0;
}
`)
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "depthprobe")
	args := []string{"-std=c++17", "-O2", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-I", dir, mainPath}
	args = append(args, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c++ compiler (it compiles and runs the generated depth probe)")
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runCppDepthBoundaryProbe(t, u, ann, cases, nil)
	if err != nil {
		t.Fatalf("cpp message depth boundary failed:\n%s\n%v", out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}

	shiftedOut, shiftedErr := runCppDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "if ( depth > 128 )") {
				files[name] = []byte(strings.ReplaceAll(s, "if ( depth > 128 )", "if ( depth > 256 )"))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected generated C++ files to contain 'if ( depth > 128 )'")
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
