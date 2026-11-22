package rubytable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// rubyCorpusRows reads one committed cases.tsv: every row that is not blank
// or a # comment, split on tabs, each with exactly want fields.
func rubyCorpusRows(t *testing.T, path string, want int) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != want {
			t.Fatalf("%s line %d: %d fields, want %d", path, n+1, len(f), want)
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		t.Fatalf("%s: no cases", path)
	}
	return rows
}

// rubyCompressedEndsDir is the committed corpus: Fends.schema (a compressed
// float whose declared top is 0.1, a decimal float32 cannot hold: its float32
// end is 0.10000000149011612; z sits under a +0 default) and cases.tsv.
const rubyCompressedEndsDir = "testdata/float32-ends"

// TestRubyCompressedFloatFloat32Ends pins SPEC.md 4.6 for the Ruby table
// leg: a float32 field's declared range is compared at float32. The pure half
// reads the emitted descriptors, which must carry the float32 ends of the
// range (the runtime rounds the value to float32 before it compares); the
// gated half runs every row of the committed corpus on both pins, on the
// variable save and load, the JSON write and read, a JSON literal read and the
// message save and load: the declared top and a value rounding into it held
// at the float32 0.1f, a value past it refused, a compressed -0 landing as +0,
// each beside its lawful bottom.
func TestRubyCompressedFloatFloat32Ends(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(rubyCompressedEndsDir, "Fends.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	gen := all.String()
	if !strings.Contains(gen, "fmax: 0.10000000149011612") {
		t.Errorf("the descriptor of tenth must carry its float32 top 0.10000000149011612")
	}
	if strings.Contains(gen, "fmax: 0.1,") || strings.Contains(gen, "fmax: 0.1)") {
		t.Errorf("the descriptor of tenth carries the double 0.1, which the float32 0.1f exceeds")
	}

	rows := rubyCorpusRows(t, filepath.Join(rubyCompressedEndsDir, "cases.tsv"), 5)
	var probe strings.Builder
	probe.WriteString(rubyCompressedEndsProbeHead)
	forms := map[string]int{}
	for _, r := range rows {
		id, form, field, value, expect := r[0], r[1], r[2], r[3], r[4]
		if field != "z" && field != "tenth" {
			t.Fatalf("%s: field %q is not one of Fends.schema's", id, field)
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			t.Fatalf("%s: value %q: %v", id, value, err)
		}
		if expect != "refused" && !strings.HasPrefix(expect, "held ") {
			t.Fatalf("%s: expect %q", id, expect)
		}
		forms[form+"/"+strings.Fields(expect)[0]]++
		switch form {
		case "variable":
			fmt.Fprintf(&probe, "step(%q) { x = Fends::Probe.new; x.%s = %s; Fends::Probe.load(x.save).%s }\n", id, field, value, field)
		case "json":
			fmt.Fprintf(&probe, "step(%q) { x = Fends::Probe.new; x.%s = %s; Fends::Probe.from_json(x.to_json).%s }\n", id, field, value, field)
		case "json-text":
			fmt.Fprintf(&probe, "step(%q) { Fends::Probe.from_json(%q).%s }\n", id, fmt.Sprintf(`{"%s": %s}`, field, value), field)
		case "message":
			fmt.Fprintf(&probe, "step(%q) { m = Fends::Msg.new; m.%s = %s; message_round(m).%s }\n", id, field, value, field)
		default:
			t.Fatalf("%s: form %q", id, form)
		}
	}
	for _, form := range []string{"variable", "json", "json-text", "message"} {
		if forms[form+"/held"] == 0 || forms[form+"/refused"] == 0 {
			t.Fatalf("cases.tsv: form %s owes a held case and a refusal beside it: %v", form, forms)
		}
	}

	t.Run("ruby", func(t *testing.T) {
		slowtest.Gate(t, "both pinned Ruby interpreters")
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(probe.String())})
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			id, answer, _ := strings.Cut(line, " ")
			got[id] = answer
		}
		for _, r := range rows {
			if got[r[0]] != r[4] {
				t.Errorf("%s (%s %s = %s): answered %q, want %q", r[0], r[1], r[2], r[3], got[r[0]], r[4])
			}
		}
	})
}

const rubyCompressedEndsProbeHead = `# frozen_string_literal: true

require_relative 'fends'

def step(id)
  back = yield
  puts "#{id} held #{back.inspect}"
rescue Fends::Refused, Fends::InvalidValue
  puts "#{id} refused"
rescue StandardError => e
  puts "#{id} error #{e.class}: #{e.message}"
end

def message_round(msg)
  batch = Fends.save_messages(Fends::Msg, [msg])
  vocab = Fends::MessageVocabulary.new
  Fends.announce_read(vocab, Fends::ANNOUNCEMENT)
  Fends.load_messages(Fends::Msg, vocab, batch)[0]
end

`
