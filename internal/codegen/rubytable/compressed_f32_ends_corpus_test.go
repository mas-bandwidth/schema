package rubytable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// f32EndsDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS HELD
// AT FLOAT32 (docs/SPEC-TABLES.md §3.3); its cases.tsv says what each row owes.
const f32EndsDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// TestRubyCompressedF32EndsDescriptors is the fast pin on the corpus's own
// schema: every descriptor of a compressed float32 carries the float32 nearest
// each declared end (0.1f is 0.10000000149011612, -0.3f is
// -0.30000001192092896), never the double the schema spells. A plain float32
// beside it carries no range at all.
func TestRubyCompressedF32EndsDescriptors(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(f32EndsDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(loadUnit(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	gen := all.String()
	for _, want := range []string{"fmax: 0.10000000149011612", "fmin: -0.30000001192092896"} {
		if !strings.Contains(gen, want) {
			t.Errorf("no descriptor carries the float32 end %s", want)
		}
	}
	for _, double := range []string{"fmax: 0.1,", "fmax: 0.1)", "fmin: -0.3,", "fmin: -0.3)"} {
		if strings.Contains(gen, double) {
			t.Errorf("a descriptor carries the double end %q, which the float32 end exceeds", double)
		}
	}
	// The control: a plain float32 has no range test.
	plain, err := Generate(loadUnit(t, "package plainf\n\ntable P\n{\n    x float32\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var p strings.Builder
	for _, data := range plain {
		p.Write(data)
	}
	if !strings.Contains(p.String(), "float_range: false") || strings.Contains(p.String(), "float_range: true") {
		t.Errorf("a plain float32 carries a range test")
	}
}

// TestRubyCompressedF32EndsCorpus runs every cases.tsv row on both Ruby pins:
// the variable form (V.load, save), the fixed form (F.fixed_load,
// F.fixed_save) and JSON (from_json), each save checked byte for byte against
// the corpus.
func TestRubyCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join(f32EndsDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rows := rubyCorpusRows(t, filepath.Join(f32EndsDir, "cases.tsv"), 8)
	inputs := map[string][]byte{}
	entries, err := os.ReadDir(f32EndsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") || strings.HasSuffix(e.Name(), ".json") {
			b, err := os.ReadFile(filepath.Join(f32EndsDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			inputs[e.Name()] = b
		}
	}
	mod := ruby.ModuleName(u.Package)
	var probe strings.Builder
	fmt.Fprintf(&probe, f32EndsRubyHead, u.Package, mod, mod, mod, mod)
	for _, r := range rows {
		name, op, form, root, input, path, bits := r[0], r[1], r[2], r[3], r[4], r[5], r[6]
		switch op {
		case "read":
			fmt.Fprintf(&probe, "step(%q) { save(%q, load(%q, %q, %q)) }\n", name, root, root, form, input)
		case "write":
			if _, err := strconv.ParseUint(bits, 16, 32); err != nil {
				t.Fatalf("%s: bits %q: %v", name, bits, err)
			}
			if (form == "variable") != (root == "V") {
				t.Fatalf("%s: form %s under root %s", name, form, root)
			}
			fmt.Fprintf(&probe, "step(%q) { write(%q, %q, %q, 0x%s) }\n", name, root, input, path, bits)
		default:
			t.Fatalf("%s: op %q", name, op)
		}
	}
	t.Run("ruby", func(t *testing.T) {
		slowtest.Gate(t, "both pinned Ruby interpreters")
		dir := t.TempDir()
		writeTree(t, dir, files)
		ins := map[string][]byte{"probe.rb": []byte(probe.String())}
		for k, v := range inputs {
			ins[k] = v
		}
		out := runBothRubyPins(t, dir, "probe.rb", ins)
		got := map[string]string{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			id, answer, _ := strings.Cut(line, " ")
			got[id] = answer
		}
		for _, r := range rows {
			want := "refused"
			if r[7] != "refused" {
				own := r[2]
				if r[1] == "read" {
					own = map[string]string{"V": "variable", "F": "fixed"}[r[3]]
				}
				b, err := os.ReadFile(filepath.Join(f32EndsDir, r[7]+"."+own+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				want = fmt.Sprintf("held %x", b)
			}
			if got[r[0]] != want {
				t.Errorf("%s: answered %q, want %q", r[0], got[r[0]], want)
			}
		}
	})
}

const f32EndsRubyHead = `# frozen_string_literal: true

require_relative %q

ROOTS = { 'V' => %s::V, 'F' => %s::F }.freeze

def raw(name)
  File.binread(File.join(__dir__, name))
end

def load(root, form, stem)
  return ROOTS[root].from_json(raw("#{stem}.json").force_encoding('UTF-8')) if form == 'json'
  return ROOTS['V'].load(raw("#{stem}.variable.bin")) if form == 'variable'

  rows = ROOTS['F'].fixed_load(raw("#{stem}.fixed.bin"))
  raise "#{rows.length} fixed rows" unless rows.length == 1

  rows[0]
end

def save(root, value)
  root == 'V' ? value.save : ROOTS['F'].fixed_save([value])
end

def write(root, stem, path, bits)
  value = load(root, 'json', stem)
  v = [bits].pack('L<').unpack1('e')
  name, index = path.split('[')
  if index
    value.public_send(name)[index.delete(']').to_i] = v
  else
    value.public_send("#{name}=", v)
  end
  save(root, value)
end

def step(id)
  out = yield
  puts "#{id} held #{out.unpack1('H*')}"
rescue %s::Refused, %s::InvalidValue
  puts "#{id} refused"
rescue StandardError => e
  puts "#{id} error #{e.class}: #{e.message.tr("\n", ' ')}"
end

`
