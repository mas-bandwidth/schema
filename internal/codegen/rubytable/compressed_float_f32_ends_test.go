package rubytable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A compressed float whose declared top is 0.1, a decimal float32 cannot
// hold: its float32 end is 0.10000000149011612. z sits under a +0 default.
const rubyCompressedEndsSchema = `package fends

fixed table Msg
{
    z float32 | min = -1, max = 1, resolution = 0.01
    tenth float32 | min = 0, max = 0.1, resolution = 0.01
}

table Probe
{
    z float32 | min = -1, max = 1, resolution = 0.01
    tenth float32 | min = 0, max = 0.1, resolution = 0.01
}
`

// TestRubyCompressedFloatFloat32Ends pins SPEC.md 4.6 for the Ruby table
// leg: a float32 field's declared range is compared at float32. The pure half
// reads the emitted descriptors, which must carry the float32 ends of the
// range (the runtime rounds the value to float32 before it compares); the
// gated half runs the variable save and load, the JSON write and read, and the
// message save and load on both pins, each holding the float32 0.1f inside
// max = 0.1, and a compressed -0 landing as +0.
func TestRubyCompressedFloatFloat32Ends(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, rubyCompressedEndsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	src := all.String()
	if !strings.Contains(src, "fmax: 0.10000000149011612") {
		t.Errorf("the descriptor of tenth must carry its float32 top 0.10000000149011612")
	}
	if strings.Contains(src, "fmax: 0.1,") || strings.Contains(src, "fmax: 0.1)") {
		t.Errorf("the descriptor of tenth carries the double 0.1, which the float32 0.1f exceeds")
	}

	t.Run("ruby", func(t *testing.T) {
		slowtest.Gate(t, "both pinned Ruby interpreters")
		dir := t.TempDir()
		writeTree(t, dir, files)
		got := strings.TrimSpace(runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(rubyCompressedEndsProbe)}))
		want := strings.Join([]string{
			"variable save: ok",
			"variable load: ok",
			"json write: ok",
			"json read: ok",
			"json read literal: ok",
			"message save: ok",
			"message load: ok",
			"negative zero: ok",
		}, "\n")
		if got != want {
			t.Errorf("probe answered:\n%s\nwant:\n%s", got, want)
		}
	})
}

const rubyCompressedEndsProbe = `# frozen_string_literal: true

require_relative 'fends'

F = [0.1].pack('e').unpack1('e')

def step(name)
  puts "#{name}: #{yield ? 'ok' : 'wrong'}"
rescue StandardError => e
  puts "#{name}: #{e.class}: #{e.message}"
end

x = Fends::Probe.new
x.tenth = 0.1
wire = nil
step('variable save') { wire = x.save; true }
step('variable load') { Fends::Probe.load(wire).tenth == F }
text = nil
step('json write') { text = x.to_json; true }
step('json read') { Fends::Probe.from_json(text).tenth == F }
step('json read literal') { Fends::Probe.from_json('{"tenth": 0.1}').tenth == F }

m = Fends::Msg.new
m.tenth = 0.1
batch = nil
step('message save') { batch = Fends.save_messages(Fends::Msg, [m]); true }
step('message load') do
  vocab = Fends::MessageVocabulary.new
  Fends.announce_read(vocab, Fends::ANNOUNCEMENT)
  Fends.load_messages(Fends::Msg, vocab, batch)[0].tenth == F
end

z = Fends::Probe.new
z.z = -0.0
step('negative zero') do
  back = Fends::Probe.load(z.save).z
  back.zero? && !(1.0 / back).negative?
end
`
