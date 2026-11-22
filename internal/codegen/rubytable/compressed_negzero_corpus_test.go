package rubytable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE RUBY LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Ruby unit on both pins: load <base>.json with the Ruby
// text read, set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte.
//
// Beside each row, every other Ruby writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save and the
// retaining save (a variable root's), the fixed save and the message form (a
// fixed table's), the JSON writer and the cook writer. A compressed row's -0
// output equals its twin's; the plain `p` rows are the control, whose -0 rides
// bit-exact, so their output differs from the twin's.
//
// It runs the pinned interpreters, so it is a slow test; the conformance lane
// runs it (make/ruby-compressed-negzero.mk). TestRubyCompressedFloatStoresAreGuarded
// is its fast twin.

const rubyNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// rubyNegZeroNoWriter names the corpus rows whose <form> writer the Ruby
// surface does not have, and why. The row's root still answers every twin its
// writers owe.
var rubyNegZeroNoWriter = map[string]string{
	"var_fixed_table_leaf": "the Ruby save of a fixed table writes the fixed form (Fix#save is _s_fixed_save): the leg has no variable-form writer for a fixed root",
}

const rubyNegZeroProbe = `# frozen_string_literal: true

require_relative 'nzgo'

NZ = -0.0
raise "the probe's -0 is not -0" unless NZ.zero? && (1.0 / NZ).negative?

def hex(b)
  return 'nil' if b.nil?
  b.to_s.unpack1('H*')
end

# set writes -0 into the cell a corpus path names: field, field[index],
# field[Variant] for an enum-keyed slot, or field.arm for a union's set arm
def set(v, path)
  if (m = path.match(/\A(\w+)\.(\w+)\z/))
    u = v.public_send(m[1])
    raise "the base does not set arm #{m[2]}" if u.nil? || u.type.zero?
    u.public_send("#{m[2]}=", NZ)
  elsif (m = path.match(/\A(\w+)\[(\w+)\]\z/))
    arr = v.public_send(m[1])
    key = m[2].match?(/\A\d+\z/) ? Integer(m[2]) : Nzgo::Level.const_get(m[2])
    arr[key] = NZ
  else
    v.public_send("#{path}=", NZ)
  end
end

def load(root, base)
  [Nzgo.const_get(root).from_json(File.read("#{base}.json")), nil]
end

# the retaining load (§6.6): its store belongs to the root it loaded, so the
# retaining save runs over that root, with -0 set in it after the load
def load_retain(base)
  store = Nzgo::TableRetain.new
  v = Nzgo::Var.load_retain(File.binread("#{base}.variable.bin"), store, Nzgo::LoadReport.new)
  raise "#{base}: the retaining load refused" if v.nil?
  [v, store]
end

# each writer the root has: [name, load, write] over a native value
def writers(root, base)
  json = -> { load(root, base) }
  out = [
    ['json', json, ->(v, _) { v.to_json }],
    ['cook', json, ->(v, _) { v.cook }]
  ]
  if root == 'Fix'
    # Fix#save of a fixed table writes the fixed form, so it is the "fixed"
    # writer's twin, not a variable writer
    out << ['save', json, ->(v, _) { v.save }]
    out << ['fixed', json, ->(v, _) { Nzgo::Fix.fixed_save([v]) }]
    out << ['message', json, ->(v, _) { Nzgo.save_messages(Nzgo::Fix, [v]) }]
  else
    out << ['variable', json, ->(v, _) { v.save }]
    out << ['retain', -> { load_retain(base) }, ->(v, store) { v.save_retain(store, Nzgo::LoadReport.new) }]
  end
  out
end

ROWS.each do |name, _form, root, base, path|
  writers(root, base).each do |w, loader, write|
    plus = loader.call
    minus = loader.call
    set(minus[0], path)
    bp = begin; hex(write.call(*plus)); rescue StandardError; 'error'; end
    bm = begin; hex(write.call(*minus)); rescue StandardError; 'error'; end
    puts "#{name} #{w} #{bm} #{bp}"
  end
end
`

// TestRubyCompressedNegZeroCorpus runs the corpus on the Ruby leg.
func TestRubyCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	src, err := os.ReadFile(filepath.Join(rubyNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	rows := rubyCorpusRows(t, filepath.Join(rubyNegZeroCorpusDir, "cases.tsv"), 6)
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(rubyNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	inputs := map[string][]byte{}
	var probe strings.Builder
	probe.WriteString("ROWS = [\n")
	for _, r := range rows {
		inputs[r[3]+".json"] = read(r[3] + ".json")
		if r[2] == "Var" {
			inputs[r[3]+".variable.bin"] = read(r[3] + ".variable.bin")
		}
		fmt.Fprintf(&probe, "  [%q, %q, %q, %q, %q],\n", r[0], r[1], r[2], r[3], r[4])
	}
	probe.WriteString("].freeze\n")
	inputs["probe.rb"] = []byte(strings.Replace(rubyNegZeroProbe, "require_relative 'nzgo'\n", "require_relative 'nzgo'\n\n"+probe.String(), 1))

	dir := t.TempDir()
	writeTree(t, dir, files)
	out := runBothRubyPins(t, dir, "probe.rb", inputs)

	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("the probe printed %q, not a row, a writer and two outputs", line)
		}
		if got[f[0]] == nil {
			got[f[0]] = map[string]pair{}
		}
		got[f[0]][f[1]] = pair{f[2], f[3]}
	}

	held, twins, exempt := 0, 0, 0
	for _, r := range rows {
		name, form, root, path, expect := r[0], r[1], r[2], r[4], r[5]
		ws := got[name]
		if ws == nil {
			t.Errorf("%s: the probe did not answer the row", name)
			continue
		}
		plain := path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin
		want := hex.EncodeToString(read(expect + "." + form + ".bin"))
		if why, ok := rubyNegZeroNoWriter[name]; ok {
			// held by name, never in silence: the row's writer must still be
			// absent, and its root's other writers still answer its twins
			if _, has := ws[form]; has {
				t.Errorf("%s: the Ruby leg now has a %s writer for %s; drop the exemption (%s)", name, form, root, why)
			}
			t.Logf("%s: not carried on the Ruby leg: %s", name, why)
			exempt++
		} else if p, ok := ws[form]; !ok {
			t.Errorf("%s: the Ruby leg has no %s writer for %s", name, form, root)
		} else if p.minus != want {
			t.Errorf("%s: the Ruby %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", name, form, path, expect, form, p.minus, want)
		} else {
			held++
		}
		// THE TWINS: every writer the root has
		for w, p := range ws {
			if p.minus == "error" || p.plus == "error" || p.minus == "nil" || p.plus == "nil" {
				t.Errorf("%s/%s: a writer refused: -0 %s, +0 %s", name, w, p.minus, p.plus)
				continue
			}
			switch {
			case plain && p.minus == p.plus:
				t.Errorf("%s/%s: a plain float's -0 did not ride: %s", name, w, p.minus)
			case !plain && p.minus != p.plus:
				t.Errorf("%s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", name, w, p.minus, p.plus)
			default:
				twins++
			}
		}
	}
	if len(rows) != 15 {
		t.Errorf("cases.tsv has %d rows, want 15", len(rows))
	}
	if held+exempt != len(rows) || exempt != len(rubyNegZeroNoWriter) {
		t.Errorf("%d rows held and %d exempt of %d; %d exemptions named", held, exempt, len(rows), len(rubyNegZeroNoWriter))
	}
	t.Logf("%d of %d corpus rows held (%d not carried, by name); %d writer twins held", held, len(rows), exempt, twins)
}
