package rubytable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
)

// A PLAN THAT READS PAST A SHORT RECORD REFUSES BY NAME, IN EVERY BUILD.
//
// THE LAW: docs/FIXED-FORM-ALGORITHM.md §4.2 and §5.2 PLAN (fix 3) — every
// compiled entry is bounded by the WRITER'S OWN DECLARED RECORD, and an entry
// reaching past it is `layout_record_too_large`. Ruby is a managed leg, so it
// refuses in every build, and nothing fails silently.
//
// THE DEFECT, until 2026-11-25: runtime/_fixed.rb read the record through
// getbyte and byteslice with `|| 0` and `|| "".b` behind them, so a read past
// the end of a record was a zero or an EMPTY slice, never an error. An integer
// read there (_s_fixed_int, so a guard, a widen, an ordinal) came back 0, a
// copy, count or text op landed FEWER bytes and shrank the image under every
// field behind it, and a dead span past the end read as all zero. A plan that
// reached past the record answered a wrong value with no refusal.
//
// NO LAWFUL PLAN REACHES THAT PATH: compileRubyFixedPlan bounds every entry at
// generate time. So the test plants one, the way R14 forges a lock layout: (1)
// every op kind and a guard, run through _s_fixed_apply / _s_fixed_guarded over
// a 4-byte record body, each reaching past it; (2) the production load,
// fixed_load and fixed_load_into, over a file this build wrote, with the plan
// of its own lineage entry swapped for one forged op that reads two bytes past
// the record.
//
// THE PLANT: delete the _s_fixed_within call from _s_fixed_int and
// _s_fixed_apply, and every case below either reads (a 0, a short copy) or
// reads a wrong value through fixed_load: red by name.
func TestRubyFixedShortRecordRefusesByName(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "package rshort\nfixed table T { a uint32 }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := `# frozen_string_literal: true

require_relative "rshort"
WANT = "layout_record_too_large"
BODY = [1, 2, 3, 4].pack("C*").freeze

def refuses(name)
  yield
rescue Rshort::Refused => e
  abort("short_#{name} refused #{e.reason}, want #{WANT}") unless e.reason.to_s == WANT
else
  abort("short_#{name} read past the record with no refusal")
end

# 1. EVERY OP KIND, each reaching past a 4-byte record body. Nothing is written.
OPS = {
  "copy" => { "op" => "copy", "src" => 2, "dst" => 0, "size" => 4, "guards" => [] },
  "widen" => { "op" => "widen", "src" => 2, "dst" => 0, "size" => 4, "width" => 8, "signed" => false, "guards" => [] },
  "widenf" => { "op" => "widenf", "src" => 2, "dst" => 0, "size" => 4, "width" => 8, "guards" => [] },
  "count" => { "op" => "count", "src" => 2, "dst" => 0, "size" => 4, "guards" => [] },
  "text" => { "op" => "text", "src" => 0, "dst" => 0, "size" => 4, "guards" => [] },
  "ordinal" => { "op" => "ordinal", "src" => 3, "dst" => 0, "size" => 2, "width" => 2, "remap" => [0, 7], "guards" => [] }
}.freeze
OPS.each do |name, op|
  image = ("\x00" * 16).b
  refuses(name) { Rshort._s_fixed_apply(op, BODY, image) }
  abort("short_#{name} wrote the image before it refused") unless image == ("\x00" * 16).b
end
refuses("guard") { Rshort._s_fixed_guarded({ "guards" => [[3, 2, 0, "eq"]] }, BODY) }
# an ALL-ZERO dead span running past the end read as dead and passed
refuses("dead") { Rshort._s_fixed_dead(("\x00" * 4).b, 2, 6) }
refuses("bool") { Rshort._s_fixed_bool(BODY, 4) }
# a const reads no record byte, and an op inside the record still reads
image = ("\x00" * 8).b
Rshort._s_fixed_apply({ "op" => "const", "src" => 0, "dst" => 0, "size" => 8, "value" => 5, "guards" => [] }, BODY, image)
Rshort._s_fixed_apply({ "op" => "copy", "src" => 0, "dst" => 0, "size" => 4, "guards" => [] }, BODY, image)
abort("short_control #{image.unpack1("H*")}") unless image == [1, 2, 3, 4, 0, 0, 0, 0].pack("C*")

# 2. THE LOAD: this build's own file, its own lineage entry's plan forged.
v = Rshort::T.new
v.a = 0x11223344
data = Rshort::T.fixed_save([v])
abort("short_load_control") unless Rshort::T.fixed_load(data)[0].a == 0x11223344
root = Rshort._s_fixed_roots["T"]
entry = root["known"][root["hash"]]
entry["direct"] = false
entry["plan"] = [{ "op" => "copy", "src" => 2, "dst" => 0, "size" => 4, "guards" => [] }]
entry["split"] = 1
refuses("load") { Rshort::T.fixed_load(data) }
targets = [Rshort::T.new]
report = Rshort::FixedLoadReport.new
n = Rshort::T.fixed_load_into(targets, data, nil, report)
abort("short_load_into #{n} #{report.refused.inspect} #{report.reason.inspect}") unless n.zero? && report.refused && report.reason.to_s == WANT
puts "short ok"
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", rubyrun.Interpreter}, {"floor", rubyrun.Floor}} {
		out, stderr, err := rubyrun.Try(t, p.path(t), dir, "main.rb", map[string][]byte{"main.rb": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "short ok" {
			t.Fatalf("a plan past a short record on the %s pin: %v\n%s\n%s", p.name, err, out, stderr)
		}
	}
}
