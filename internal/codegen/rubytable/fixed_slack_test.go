package rubytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header. A Ruby counted
// array is an Array of exactly `count` elements, so the rule here is that
// after fixed_load_into A and then B into the SAME targets every array holds
// exactly B's count and no element left over from A, the same as a fresh
// load of B. The leg holds the rule already (fixed_load_into copies freshly
// loaded values into the targets); this test guards it on both pins.
func TestRubyFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters (they run the generated fixed load over FixedSlack.schema)")
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := rubytable.Generate(fixedproof.ParseCheckUnit(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := rubyFixedRun(t, files, rubySlackProbe, nil)
	out = strings.TrimSpace(out)
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 subs 1 77",
		"num 77",
	}
	for _, tag := range []string{"fresh", "reused"} {
		if got := rubySlackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant exactly B's live elements\n%s\nprobe output:\n%s",
				tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	if !strings.Contains(out, "refused 0") {
		t.Fatalf("the truncated file read:\n%s", out)
	}
	newborn := rubySlackLines(out, "newborn")
	if strings.Join(newborn, "\n") != "cells 0 nums 0\nnum" {
		t.Fatalf("a fresh SlackRoot dumps %q", newborn)
	}
	if got := rubySlackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left %q, want a fresh value %q\nprobe output:\n%s", got, newborn, out)
	}
}

func rubySlackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && rest != "0" {
			got = append(got, rest)
		}
	}
	return got
}

const rubySlackProbe = `require_relative "tblslack"

def sub(v)
  s = Tblslack::SlackSub.new
  s.v = v
  s
end

def cell(n)
  c = Tblslack::SlackCell.new
  c.id = 77
  c.subs = (0...n).map { sub(77) }
  c
end

def dump(tag, r)
  puts "#{tag} cells #{r.cells.length} nums #{r.nums.length}"
  r.cells.each_with_index do |c, i|
    puts(["#{tag} cell #{i} #{c.id} subs #{c.subs.length}", *c.subs.map(&:v)].join(" "))
  end
  puts(["#{tag} num", *r.nums].join(" "))
end

a = Tblslack::SlackRoot.new
a.cells = (0...3).map { cell(2) }
a.nums = [77] * 4
b = Tblslack::SlackRoot.new
b.cells = [cell(1)]
b.nums = [77]
fa = Tblslack::SlackRoot.fixed_save([a])
fb = Tblslack::SlackRoot.fixed_save([b])
dump("fresh", Tblslack::SlackRoot.fixed_load(fb)[0])
targets = [Tblslack::SlackRoot.new]
Tblslack::SlackRoot.fixed_load_into(targets, fa)
Tblslack::SlackRoot.fixed_load_into(targets, fb)
dump("reused", targets[0])
Tblslack::SlackRoot.fixed_load_into(targets, fa)
puts "refused #{Tblslack::SlackRoot.fixed_load_into(targets, fb.byteslice(0, fb.bytesize - 1))}"
dump("refused", targets[0])
dump("newborn", Tblslack::SlackRoot.new)
`
