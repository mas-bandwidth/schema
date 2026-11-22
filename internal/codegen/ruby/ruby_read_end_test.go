package ruby

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/check"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/parser"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// TestRubyPacketReadEnforcesSerializeEnd pins SPEC §5 ("Read termination") on
// generated Ruby top-level Cls.read(data): non-zero unused bits in the final
// byte and extra unconsumed bytes after the packet must raise Refused, while
// Cls.read_bits(data, bit_offset) preserves bit-stream sub-reads.
func TestRubyPacketReadEnforcesSerializeEnd(t *testing.T) {
	t.Parallel()

	src := `package endprobe

type FiveBits
{
    flag bool
    val  uint8 | min = 0, max = 15
}
`
	ast, perrs := parser.Parse("EndProbe.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "EndProbe.schema", Name: "EndProbe.schema", Base: "EndProbe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	code := string(files["endprobe/EndProbe.rb"])
	if !strings.Contains(code, "unless stream.serialize_end") {
		t.Fatalf("generated self.read must check stream.serialize_end:\n%s", code)
	}

	t.Run("runtime", func(t *testing.T) {
		slowtest.Gate(t, "ruby (runs generated packet Cls.read termination check)")
		interp := rubyrun.Interpreter(t)
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := `# frozen_string_literal: true
require_relative "endprobe"

v = Endprobe::FiveBits.new
v.flag = true
v.val = 9
wire = v.write
raise "expected 1 byte, got #{wire.bytesize}" unless wire.bytesize == 1

ok = Endprobe::FiveBits.read(wire)
raise "roundtrip mismatch" unless ok.flag == true && ok.val == 9

[
  ["bit5", (wire.getbyte(0) | 0x20).chr(Encoding::ASCII_8BIT)],
  ["bit7", (wire.getbyte(0) | 0x80).chr(Encoding::ASCII_8BIT)],
  ["extra_zero", wire + "\x00".b],
  ["extra_ff", wire + "\xff".b],
].each do |label, bad|
  refused = false
  begin
    Endprobe::FiveBits.read(bad)
  rescue Endprobe::Refused
    refused = true
  end
  raise "FiveBits.read accepted #{label}" unless refused
end

sub, bits = Endprobe::FiveBits.read_bits(wire + "\xff".b, bit_offset: 0)
raise "read_bits failed" unless sub.val == 9 && bits == 5
puts "ok"
`
		if err := os.WriteFile(filepath.Join(dir, "probe.rb"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, rerr := rubyrun.TryArgs(t, interp, dir, "--disable-yjit", []string{"probe.rb"}, nil, nil)
		if rerr != nil || stderr != "" || strings.TrimSpace(stdout) != "ok" {
			t.Fatalf("probe failed: err=%v stderr=%q stdout=%q", rerr, stderr, stdout)
		}
	})
}
