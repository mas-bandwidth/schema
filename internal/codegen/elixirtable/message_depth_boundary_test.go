package elixirtable

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

func depthBoundaryElixirBinaryLiteral(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return "<<" + strings.Join(parts, ", ") + ">>"
}

func runElixirDepthBoundaryProbe(t *testing.T, u *ir.Unit, ann []byte, cases []depthBoundaryCase, edit func(string) string) (string, error) {
	t.Helper()
	elixirBin := elixirBinary(t)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	modBytes, ok := tables[TableMessageModule+".ex"]
	if !ok {
		t.Fatalf("missing %s.ex", TableMessageModule)
	}
	modSrc := string(modBytes)
	if edit != nil {
		modSrc = edit(modSrc)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, TableMessageModule+".ex"), []byte(modSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "defmodule DepthProbe do\n  alias Depthprobe.TableMessage, as: M\n\n  def check_case(name, vocab, wire, true) do\n")
	fmt.Fprintf(&body, "    case M.load_messages(vocab, wire, 2) do\n")
	fmt.Fprintf(&body, "      {:ok, [b0, b1]} ->\n")
	fmt.Fprintf(&body, "        if length(b0) == 2 and Enum.at(b0, 1) == {1, 42} and b1 == [{1, 99}] do\n")
	fmt.Fprintf(&body, "          IO.puts(\"PASS \" <> name)\n")
	fmt.Fprintf(&body, "        else\n")
	fmt.Fprintf(&body, "          IO.puts(\"FAIL \" <> name <> \": unexpected bodies \" <> inspect({b0, b1}))\n")
	fmt.Fprintf(&body, "          Process.put(:failed, true)\n")
	fmt.Fprintf(&body, "        end\n")
	fmt.Fprintf(&body, "      other ->\n")
	fmt.Fprintf(&body, "        IO.puts(\"FAIL \" <> name <> \": expected {:ok, _}, got \" <> inspect(other))\n")
	fmt.Fprintf(&body, "        Process.put(:failed, true)\n")
	fmt.Fprintf(&body, "    end\n  end\n\n")
	fmt.Fprintf(&body, "  def check_case(name, vocab, wire, false) do\n")
	fmt.Fprintf(&body, "    case M.load_messages(vocab, wire, 2) do\n")
	fmt.Fprintf(&body, "      {:malformed, 0, []} ->\n")
	fmt.Fprintf(&body, "        IO.puts(\"PASS \" <> name)\n")
	fmt.Fprintf(&body, "      other ->\n")
	fmt.Fprintf(&body, "        IO.puts(\"FAIL \" <> name <> \": expected {:malformed, 0, []}, got \" <> inspect(other))\n")
	fmt.Fprintf(&body, "        Process.put(:failed, true)\n")
	fmt.Fprintf(&body, "    end\n  end\n\n")
	fmt.Fprintf(&body, "  def run do\n")
	fmt.Fprintf(&body, "    {:ok, vocab} = M.announce_read(%s)\n", depthBoundaryElixirBinaryLiteral(ann))
	for _, tc := range cases {
		fmt.Fprintf(&body, "    check_case(%q, vocab, %s, %v)\n", tc.name, depthBoundaryElixirBinaryLiteral(tc.wire), tc.expectOK)
	}
	fmt.Fprintf(&body, "    if Process.get(:failed), do: System.halt(1)\n  end\nend\n\nDepthProbe.run()\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.exs"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(elixirBin, "-r", TableMessageModule+".ex", "probe.exs")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+otpBin(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGeneratedMessageDepthBoundary(t *testing.T) {
	t.Parallel()
	u, ann, cases := depthBoundaryFixtures(t)
	out, err := runElixirDepthBoundaryProbe(t, u, ann, cases, nil)
	if err != nil {
		t.Fatalf("elixir message depth boundary failed:\n%s\n%v", out, err)
	}
	for _, tc := range cases {
		if !strings.Contains(out, "PASS "+tc.name) {
			t.Fatalf("missing PASS for %s in:\n%s", tc.name, out)
		}
	}

	shiftedOut, shiftedErr := runElixirDepthBoundaryProbe(t, u, ann, cases, func(src string) string {
		if !strings.Contains(src, "when depth > 128") {
			t.Fatalf("expected generated TableMessage.ex to contain 'when depth > 128'")
		}
		return strings.ReplaceAll(src, "when depth > 128", "when depth > 256")
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
