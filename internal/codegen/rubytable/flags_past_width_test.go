package rubytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestRubyFlagsPastWidthRefuse holds a flags mask to its reader's width W
// (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the newer
// reader reads old data, and never the other way. A newer build's variable
// file with its appended bit set, a narrower unsigned field widened into the
// mask with a bit past W, and a fixed record with a stored bit past W each
// refuse the whole load out_of_range; the twins inside W read. The variable
// and fixed savers raise InvalidValue for a mask past W, so a same-build round
// trip never writes what its own reader refuses. Under the old rule (a mask's
// range was the u64's own) every refusal here read.
func TestRubyFlagsPastWidthRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	wire := func(src, text string) []byte {
		t.Helper()
		m := tabletext.NewModel(fixedproof.ParseCheckUnit(t, src))
		inst := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) || rep.Refused {
			t.Fatalf("the Go engine does not read %s: %+v", text, rep)
		}
		b, err := tablewire.EncodeVariable(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	const newer = "package flagspast\nflags Cap { Trace, Verbose, Extra }\ntable Root { cap Cap }\n"
	const narrow = "package flagspast\ntable Root { cap uint8 }\n"
	inputs := map[string][]byte{
		"past":       wire(newer, `{"cap":["Trace","Extra"]}`),
		"inside":     wire(newer, `{"cap":["Trace","Verbose"]}`),
		"widepast":   wire(narrow, `{"cap":5}`),
		"wideinside": wire(narrow, `{"cap":3}`),
	}
	for _, tc := range []struct{ name, src, probe string }{
		{"variable", "package flagspast\nflags Cap { Trace, Verbose }\ntable Root { cap Cap }\n", flagsPastVariableProbe},
		{"fixed", "package flagspastfixed\nflags Cap { Trace, Verbose }\nfixed table Root { cap Cap }\n", flagsPastFixedProbe},
	} {
		files, err := rubytable.Generate(fixedproof.ParseCheckUnit(t, tc.src))
		if err != nil {
			t.Fatal(err)
		}
		if out, _ := rubyFixedRun(t, files, tc.probe, inputs); strings.TrimSpace(out) != "ok" {
			t.Errorf("%s: %s", tc.name, out)
		}
	}
}

const flagsPastVariableProbe = `require_relative "flagspast"
%w[past widepast].each do |name|
  begin
    Flagspast::Root.load(File.binread(name))
  rescue Flagspast::Refused => err
    abort("#{name}: refused #{err.reason}, want out_of_range") unless err.reason == "out_of_range"
  else
    abort("#{name}: read a mask bit past W")
  end
end
%w[inside wideinside].each do |name|
  got = Flagspast::Root.load(File.binread(name)).cap
  abort("#{name}: read #{got}, want 3") unless got == 3
end
r = Flagspast::Root.new
r.cap = 5
begin
  r.save
rescue Flagspast::InvalidValue
  nil
else
  abort("the variable save wrote a mask bit past W")
end
puts "ok"
`

const flagsPastFixedProbe = `require_relative "flagspastfixed"
def image(mask)
  r = Flagspastfixed::Root.new
  r.cap = mask
  Flagspastfixed::Root.fixed_save([r])
end
low = image(1)
both = image(3)
at = (0...low.bytesize).select { |i| low.getbyte(i) != both.getbyte(i) }
abort("no single mask byte: #{at}") unless low.bytesize == both.bytesize && at.length == 1
forged = low.dup
forged.setbyte(at[0], low.getbyte(at[0]) | 4)
begin
  Flagspastfixed::Root.fixed_load(forged)
rescue Flagspastfixed::Refused => err
  abort("fixed: refused #{err.reason}, want out_of_range") unless err.reason == "out_of_range"
else
  abort("fixed: read a mask bit past W")
end
got = Flagspastfixed::Root.fixed_load(both)[0].cap
abort("fixed: read #{got}, want 3") unless got == 3
r = Flagspastfixed::Root.new
r.cap = 5
begin
  Flagspastfixed::Root.fixed_save([r])
rescue Flagspastfixed::InvalidValue
  nil
else
  abort("the fixed save wrote a mask bit past W")
end
puts "ok"
`
