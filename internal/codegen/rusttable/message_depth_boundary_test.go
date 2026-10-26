package rusttable

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

func depthBoundaryRustBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("0x%02xu8", v)
	}
	return "&[" + strings.Join(parts, ", ") + "]"
}

func runRustDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	messageBatchRustc(t)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(`
fn check_case(name: &str, vocab: &TableVocabulary, wire: &[u8], expect_ok: bool, failed: &mut bool) {
    let mut values = [RootRow { a: -7 }, RootRow { a: -7 }];
    let mut rep = TableReport::default();
    let (count, ok) = root_load_messages(&mut values, vocab, wire, &mut rep);
    if expect_ok {
        if ok && count == 2 && !rep.malformed && !rep.refused && rep.unknown == 1 && values[0].a == 42 && values[1].a == 99 {
            println!("PASS {name}");
        } else {
            println!("FAIL {name}: ok={ok} count={count} malformed={} refused={} unknown={} a0={} a1={}", rep.malformed, rep.refused, rep.unknown, values[0].a, values[1].a);
            *failed = true;
        }
    } else {
        if !ok && count == 0 && rep.malformed && !rep.refused && (values[0].a == 0 || values[0].a == -7) && values[1].a == -7 {
            println!("PASS {name}");
        } else {
            println!("FAIL {name}: ok={ok} count={count} malformed={} refused={} a0={} a1={}", rep.malformed, rep.refused, values[0].a, values[1].a);
            *failed = true;
        }
    }
}

fn main() {
`)
	fmt.Fprintf(&main, "    let ann: &[u8] = %s;\n", depthBoundaryRustBytes(ann))
	main.WriteString(`    let mut storage = vec![TableMessageEntry::default(); 64];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    assert!(announce_read(&mut vocab, ann, &mut arep) && vocab.announced, "announce_read failed");
    let mut failed = false;
`)
	for _, tc := range cases {
		fmt.Fprintf(&main, "    check_case(%q, &vocab, %s, %v, &mut failed);\n", tc.name, depthBoundaryRustBytes(tc.wire), tc.expectOK)
	}
	main.WriteString(`    if failed {
        std::process::exit(1);
    }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "depthprobe")
	cmd := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rustc failed: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runRustDepthBoundaryProbe(t, u, ann, cases, nil)
	if err != nil {
		t.Fatalf("rust message depth boundary failed:\n%s\n%v", out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}

	shiftedOut, shiftedErr := runRustDepthBoundaryProbe(t, u, ann, cases, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, "if depth > 128") {
				files[name] = []byte(strings.ReplaceAll(s, "if depth > 128", "if depth > 256"))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("expected generated Rust files to contain 'if depth > 128'")
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
