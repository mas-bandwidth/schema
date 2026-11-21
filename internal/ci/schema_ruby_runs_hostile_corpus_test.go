package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const (
	rubyHostileManifestName    = "manifest.tsv"
	rubyHostileAccept          = "accept"
	rubyHostileRefuse          = "refuse"
	rubyHostileProbeSchemaPath = "schemas/probe.schema"
	rubyHostileProbeSchema     = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

	// The planted reader bug: the fixed-form gates refuse the form they carry,
	// so the fixed wire the corpus accepts (wire-valid, form 3) is refused. The
	// plant changes a verdict: a refusal conforms on its verdict alone
	// (refusal-verdict-only), so a plant that only renamed a refusal would
	// leave the corpus green.
	rubyHostilePlantFrom     = "if b0 != 3"
	rubyHostilePlantTo       = "if b0 != 3 || true"
	rubyHostilePlantFormFrom = "if b0 != form"
	rubyHostilePlantFormTo   = "if b0 != form || true"

	rubyAllocationFactorF = 512.0
	rubyAllocationSlackKB = 2048.0
)

type rubyHostileCase struct {
	Name       string
	Carrier    string
	Schema     string
	Table      string
	Input      string
	Provenance string
	Verdict    string
	Reason     string
	Digest     string
}

func rubyHostileParse(text string) ([]rubyHostileCase, error) {
	var out []rubyHostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, rubyHostileCase{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

func rubyHostileCorpusCases(t *testing.T, dir string) []rubyHostileCase {
	t.Helper()
	p := filepath.Join(dir, rubyHostileManifestName)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v", p, err)
	}
	cases, err := rubyHostileParse(string(data))
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s names no cases", p)
	}
	return cases
}

type rubyHostileUnit struct {
	schema      string
	pkg         string
	dir         string
	unavailable string
}

func rubyHostileLoadUnit(t *testing.T, schemaPath string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func rubyHostileGenerateUnit(u *ir.Unit) (map[string][]byte, error) {
	return compiler.New().Generate(u, "ruby", compiler.Options{})
}

func rubyHostileWriteTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rubyHostilePackage(t *testing.T, schema string, files map[string][]byte) string {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if path.Dir(name) == "." && strings.HasSuffix(name, ".rb") && !strings.HasPrefix(name, "_") {
			return strings.TrimSuffix(name, ".rb")
		}
	}
	t.Fatalf("%s: emitted no top-level Ruby unit file", schema)
	return ""
}

// rubyHostilePlant plants the reader bug in a unit's fixed-form reader and
// reports whether it landed. The variable-form gate in _table/load.rb is not
// planted: the corpus accepts no form-1 wire, so refusing one changes no
// verdict, and the same gate guards the module's own load-time reads.
func rubyHostilePlant(t *testing.T, files map[string][]byte) bool {
	t.Helper()
	landed := false
	for name := range files {
		if strings.HasSuffix(name, "/_fixed.rb") {
			src := string(files[name])
			if !strings.Contains(src, rubyHostilePlantFrom) {
				t.Fatalf("%s does not carry the planted pattern %q", name, rubyHostilePlantFrom)
			}
			src = strings.ReplaceAll(src, rubyHostilePlantFrom, rubyHostilePlantTo)
			src = strings.ReplaceAll(src, rubyHostilePlantFormFrom, rubyHostilePlantFormTo)
			files[name] = []byte(src)
			landed = true
		}
	}
	return landed
}

func rubyHostileGenerate(t *testing.T, root, dir string, cases []rubyHostileCase, plant bool) []rubyHostileUnit {
	t.Helper()
	seen := map[string]bool{}
	var units []rubyHostileUnit
	planted := false
	for _, c := range cases {
		if seen[c.Schema] {
			continue
		}
		seen[c.Schema] = true
		schemaPath := filepath.Join(root, filepath.FromSlash(c.Schema))
		if c.Schema == rubyHostileProbeSchemaPath {
			schemaPath = filepath.Join(dir, "probe.schema")
			if err := os.WriteFile(schemaPath, []byte(rubyHostileProbeSchema), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		u := rubyHostileLoadUnit(t, schemaPath)
		unitDir := filepath.Join(dir, fmt.Sprintf("unit-%d", len(units)))
		if err := os.MkdirAll(unitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		entry := rubyHostileUnit{schema: c.Schema, dir: unitDir}
		files, err := rubyHostileGenerateUnit(u)
		if err != nil {
			entry.unavailable = err.Error()
		} else {
			entry.pkg = rubyHostilePackage(t, c.Schema, files)
			if plant && rubyHostilePlant(t, files) {
				planted = true
			}
			rubyHostileWriteTree(t, unitDir, files)
		}
		units = append(units, entry)
	}
	if plant && !planted {
		t.Fatal("no emitted unit carries a fixed-form reader (_fixed.rb), so the planted reader bug went nowhere")
	}
	return units
}

const rubyHostileProbe = `# frozen_string_literal: true

require 'fileutils'

package_name = File.read('package', encoding: 'UTF-8').strip
require_relative package_name
mod_name = package_name.split('_').map(&:capitalize).join
PKG = Object.const_get(mod_name, false)
Refused = PKG::Refused

def refuse_reason(carrier, exc)
  reason = exc.respond_to?(:reason) ? exc.reason.to_s : ''
  return 'packet_refused' if carrier == 'packet'
  return 'damaged' if carrier == 'text'

  if carrier == 'wire'
    return 'count_refusal' if reason == 'count_over_extent_cap'
    return reason if %w[message_form_as_file newer_form non_finite_float].include?(reason)

    return 'damaged'
  end
  reason.empty? ? 'damaged' : reason
end

# A writer that refuses the value the read accepted is an error, never a
# refusal: a refusal conforms on its verdict alone (refusal-verdict-only).
def written(what)
  yield
rescue Refused => e
  raise "#{what} refused the decoded value: #{e.respond_to?(:reason) ? e.reason : ''}"
end

def canonical_text(value)
  text = written('the text writer') { value.to_json }
  text = text.byteslice(0, text.bytesize - 1) if text.getbyte(-1) == 10
  text.b
end

def run_case(carrier, root_name, payload)
  root = PKG.const_get(root_name, false)
  case carrier
  when 'packet'
    value = root.read(payload)
    ['accept', '-', written('the packet writer') { value.write }]
  when 'text'
    text = payload.dup.force_encoding(Encoding::UTF_8)
    raise Refused.new('invalid_utf8') unless text.valid_encoding?

    value = root.from_json(text)
    ['accept', '-', canonical_text(value)]
  when 'wire'
    # a fixed root with no fixed reader is a missing codec: a crash of this
    # runner, never a refusal (one made up here would pass on its verdict)
    if root.respond_to?(:descriptor) && root.descriptor && root.descriptor.fixed &&
       !PKG.respond_to?(:_s_fixed_load) &&
       (!PKG.const_defined?(:SchemaTable, false) || !PKG::SchemaTable.respond_to?(:fixed_load))
      raise "reader_unavailable: #{root_name} is a fixed root with no fixed reader"
    end
    value = root.load(payload)
    ['accept', '-', canonical_text(value)]
  when 'announce'
    vocab = PKG::MessageVocabulary.new
    PKG.announce_read(vocab, payload)
    ['accept', '-', nil]
  when 'announce_second'
    vocab = PKG::MessageVocabulary.new
    PKG.announce_read(vocab, payload)
    PKG.announce_read(vocab, payload)
    ['accept', '-', nil]
  when 'announce_bytes'
    vocab = PKG::MessageVocabulary.new
    PKG.announce_read(vocab, payload, max_bytes: 8)
    ['accept', '-', nil]
  when 'announce_entries'
    vocab = PKG::MessageVocabulary.new
    PKG.announce_read(vocab, payload, max_entries: 1)
    ['accept', '-', nil]
  when 'message'
    vocab = PKG::MessageVocabulary.new
    PKG.announce_read(vocab, PKG::ANNOUNCEMENT)
    values = PKG.load_messages(root, vocab, payload, 1)
    raise 'expected 1 message' unless values.length == 1

    ['accept', '-', canonical_text(values[0])]
  when 'message_novocab'
    vocab = PKG::MessageVocabulary.new
    values = PKG.load_messages(root, vocab, payload, 1)
    raise 'expected 1 message' unless values.length == 1

    ['accept', '-', canonical_text(values[0])]
  else
    raise "unknown carrier #{carrier}"
  end
end

FileUtils.mkdir_p('answers')
File.readlines('cases', chomp: true, encoding: 'UTF-8').each do |line|
  next if line.empty?

  name, carrier, root_name, input_name = line.split("\t")
  payload = File.binread(input_name)
  begin
    verdict, reason, answer = run_case(carrier, root_name, payload)
  rescue Refused => e
    verdict = 'refuse'
    reason = refuse_reason(carrier, e)
    answer = nil
  rescue StandardError => e
    verdict = 'crash'
    reason = "#{e.class}: #{e.message}"
    answer = nil
  end
  File.binwrite(File.join('answers', name), answer) unless answer.nil?
  puts "CASE\t#{name}\t#{verdict}\t#{reason}"
end
`

type rubyHostileAnswer struct {
	verdict string
	reason  string
	text    []byte
}

func rubyHostileRun(t *testing.T, root, corpusDir string, cases []rubyHostileCase, plant bool) map[string]rubyHostileAnswer {
	t.Helper()
	dir := t.TempDir()
	units := rubyHostileGenerate(t, root, dir, cases, plant)
	byUnit := map[string][]rubyHostileCase{}
	for _, c := range cases {
		byUnit[c.Schema] = append(byUnit[c.Schema], c)
	}
	answers := map[string]rubyHostileAnswer{}
	for _, unit := range units {
		if unit.unavailable != "" {
			for _, c := range byUnit[unit.schema] {
				answers[c.Name] = rubyHostileUnavailable(unit.unavailable)
			}
			continue
		}
		inputs := map[string][]byte{
			"main.rb": []byte(rubyHostileProbe),
			"package": []byte(unit.pkg),
		}
		var lines []string
		for _, c := range byUnit[unit.schema] {
			inputName := "input-" + c.Name
			data, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(c.Input)))
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			inputs[inputName] = data
			lines = append(lines, strings.Join([]string{c.Name, c.Carrier, c.Table, inputName}, "\t"))
		}
		inputs["cases"] = []byte(strings.Join(lines, "\n"))
		for _, interpreter := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
			stdout, stderr, err := rubyrun.Try(t, interpreter, unit.dir, "main.rb", inputs)
			if err != nil || stderr != "" {
				t.Fatalf("%s (%s): Ruby hostile-corpus runner failed: %v\nstderr:\n%s\nstdout:\n%s",
					unit.schema, filepath.Base(interpreter), err, stderr, stdout)
			}
			for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
				if line == "" {
					continue
				}
				fields := strings.Split(line, "\t")
				if len(fields) != 4 || fields[0] != "CASE" {
					t.Fatalf("unexpected Ruby hostile-corpus output line %q", line)
				}
				answer := rubyHostileAnswer{verdict: fields[2], reason: fields[3]}
				if answer.verdict == rubyHostileAccept {
					answer.text, err = os.ReadFile(filepath.Join(unit.dir, "answers", fields[1]))
					if err != nil && !os.IsNotExist(err) {
						t.Fatalf("read Ruby answer for %s: %v", fields[1], err)
					}
				}
				// two refusals agree whatever each names (refusal-verdict-only)
				if prev, ok := answers[fields[1]]; ok && (prev.verdict != answer.verdict ||
					(answer.verdict != rubyHostileRefuse && prev.reason != answer.reason)) {
					t.Fatalf("%s: the two pinned Ruby interpreters disagree: %s/%s vs %s/%s",
						fields[1], prev.verdict, prev.reason, answer.verdict, answer.reason)
				}
				answers[fields[1]] = answer
			}
		}
	}
	return answers
}

// rubyHostileGrade grades one answer against its case and returns "" when it
// conforms, else the disagreement. A refusal conforms on its verdict alone,
// whatever reason either side names (docs/rulings.json refusal-verdict-only,
// SPEC-TABLES §6.5 and §13.10); an accept on its decoded value's digest; a
// crash or no answer never conforms.
func rubyHostileGrade(c rubyHostileCase, a rubyHostileAnswer, answered bool) string {
	if !answered {
		return fmt.Sprintf("%s: want %s/%s, got no answer", c.Name, c.Verdict, c.Reason)
	}
	if a.verdict != c.Verdict || (c.Verdict != rubyHostileRefuse && a.reason != c.Reason) {
		return fmt.Sprintf("%s: want %s/%s, got %s/%s", c.Name, c.Verdict, c.Reason, a.verdict, a.reason)
	}
	if c.Verdict == rubyHostileAccept {
		if a.text == nil {
			return fmt.Sprintf("%s: want digest %s, got no text", c.Name, c.Digest)
		}
		sum := sha256.Sum256(a.text)
		if got := hex.EncodeToString(sum[:]); got != c.Digest {
			return fmt.Sprintf("%s: want digest %s, got %s", c.Name, c.Digest, got)
		}
	}
	return ""
}

// rubyHostileUnavailable is the answer for every case of a unit that did not
// generate: a crash of this runner, never a refusal.
func rubyHostileUnavailable(why string) rubyHostileAnswer {
	return rubyHostileAnswer{verdict: "crash", reason: "reader_unavailable: " + why}
}

// TestSchemaRubyHostileUnavailableReaderIsNoRefusal: a case with no reader
// fails whatever the manifest says, a refusal included. It runs no Ruby.
func TestSchemaRubyHostileUnavailableReaderIsNoRefusal(t *testing.T) {
	t.Parallel()
	cases := []rubyHostileCase{
		{Name: "refused", Carrier: "wire", Verdict: rubyHostileRefuse, Reason: "damaged", Digest: "-"},
		{Name: "accepted", Carrier: "wire", Verdict: "accept", Reason: "-", Digest: "00"},
	}
	answers := map[string]rubyHostileAnswer{}
	for _, c := range cases {
		answers[c.Name] = rubyHostileUnavailable("the unit did not generate")
	}
	if bad := rubyHostileCompare(cases, answers); len(bad) != len(cases) {
		t.Errorf("a missing reader must fail every case, refused ones included; failed %d of %d: %v", len(bad), len(cases), bad)
	}
}

// rubyHostileCompare returns every disagreement with the manifest. A case with
// no reader (rubyHostileUnavailable) is graded like any other, so it fails: a
// missing codec is never skipped and never a refusal.
func rubyHostileCompare(cases []rubyHostileCase, answers map[string]rubyHostileAnswer) []string {
	var bad []string
	for _, c := range cases {
		a, ok := answers[c.Name]
		if why := rubyHostileGrade(c, a, ok); why != "" {
			bad = append(bad, why)
		}
	}
	return bad
}

// TestSchemaRubyHostileScorerOutcomes grades the shared scorer outcome cases
// (hostilecorpus.ScorerOutcomesPath) through rubyHostileGrade: a refusal with
// another reason or none passes; a changed verdict either way, a different
// accepted value or a crash fails. It runs no Ruby.
func TestSchemaRubyHostileScorerOutcomes(t *testing.T) {
	t.Parallel()
	outcomes, err := hostilecorpus.LoadScorerOutcomes(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		c := rubyHostileCase{Name: o.Name, Carrier: "text", Verdict: o.Verdict, Reason: o.Reason, Digest: o.Digest}
		a := rubyHostileAnswer{verdict: "crash", reason: "SystemExit: the worker exited with status 3"}
		if name, refused := o.Refused(); refused {
			a = rubyHostileAnswer{verdict: rubyHostileRefuse, reason: name}
		} else if text, accepted := o.Accepted(); accepted {
			a = rubyHostileAnswer{verdict: rubyHostileAccept, reason: "-", text: text}
		}
		if why := rubyHostileGrade(c, a, true); (why == "") != o.Pass {
			t.Errorf("%s: graded %q, the fixture says pass=%t", o.Name, why, o.Pass)
		}
	}
}

func TestSchemaRubyRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	root := repoRoot(t)
	corpusDir := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	cases := rubyHostileCorpusCases(t, corpusDir)
	answers := rubyHostileRun(t, root, corpusDir, cases, false)
	if bad := rubyHostileCompare(cases, answers); len(bad) != 0 {
		t.Fatalf("the Ruby port disagrees with %d of %d hostile-corpus cases:\n  %s",
			len(bad), len(cases), strings.Join(bad, "\n  "))
	}
}

func TestSchemaRubyHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	root := repoRoot(t)
	corpusDir := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	cases := rubyHostileCorpusCases(t, corpusDir)
	clean := rubyHostileRun(t, root, corpusDir, cases, false)
	if bad := rubyHostileCompare(cases, clean); len(bad) != 0 {
		t.Fatalf("with the plant out the runner must be green:\n  %s", strings.Join(bad, "\n  "))
	}
	planted := rubyHostileRun(t, root, corpusDir, cases, true)
	bad := rubyHostileCompare(cases, planted)
	if len(bad) == 0 {
		t.Fatal("the planted variable-table form gate bug (0xff admitted) was not observed: corpus stayed green")
	}
}

const rubyResourceBoundsSchema = `package probe

table H
{
    n int32 | min = 0, max = 100000
    big [..1000]int32
    next *H
}
`

func rubyResourceBoundsDocs() map[string]string {
	chain := ""
	for i := 132; i >= 1; i-- {
		if chain == "" {
			chain = fmt.Sprintf(`{"&node": %d}`, i)
		} else {
			chain = fmt.Sprintf(`{"&node": %d, "next": %s}`, i, chain)
		}
	}
	var unk strings.Builder
	unk.WriteByte('{')
	for i := range 2000 {
		if i != 0 {
			unk.WriteByte(',')
		}
		fmt.Fprintf(&unk, `"zz%d":0`, i)
	}
	unk.WriteByte('}')
	return map[string]string{
		"deep-nesting":        strings.Repeat("[", 132) + strings.Repeat("]", 132),
		"huge-count":          `{"big": [` + strings.Repeat("1,", 99999) + "1]}",
		"recursion":           chain,
		"giant-string":        `"` + strings.Repeat("x", 70000) + `"`,
		"many-unknown-fields": unk.String(),
	}
}

type rubyResourceBoundsWant struct {
	verdict string
	reason  string
}

func rubyResourceBoundsWants() map[string]rubyResourceBoundsWant {
	return map[string]rubyResourceBoundsWant{
		"deep-nesting":        {"REFUSED", "malformed"},
		"huge-count":          {"REFUSED", "count_over_bound"},
		"recursion":           {"REFUSED", "malformed"},
		"giant-string":        {"REFUSED", "kind_mismatch"},
		"many-unknown-fields": {"OK", ""},
	}
}

const rubyResourceBoundsProbe = `# frozen_string_literal: true

require_relative 'probe'

NAMES = %w[deep-nesting huge-count recursion giant-string many-unknown-fields].freeze
SLOT_SIZE = GC::INTERNAL_CONSTANTS[:RVALUE_SIZE] || 40

NAMES.each do |shape|
  text = File.binread(shape).force_encoding(Encoding::UTF_8)
  steps = 0
  tp = TracePoint.new(:line) { steps += 1 }
  base_objs = GC.stat(:total_allocated_objects)
  tp.enable
  begin
    Probe::H.from_json(text)
    verdict = 'OK'
    reason = ''
  rescue Probe::Refused => e
    verdict = 'REFUSED'
    reason = e.reason.to_s
  rescue StandardError => e
    verdict = 'CRASH'
    reason = "#{e.class}:#{e.message}"
  ensure
    tp.disable
  end
  allocated = (GC.stat(:total_allocated_objects) - base_objs) * SLOT_SIZE
  puts "#{shape}\t#{verdict}\t#{reason}\t#{allocated}\t#{steps}\t#{text.bytesize}"
end
`

func rubyResourceBoundsUnit(t *testing.T, plantFrom, plantTo string) string {
	t.Helper()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "H.schema")
	if err := os.WriteFile(schema, []byte(rubyResourceBoundsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := rubyHostileLoadUnit(t, schema)
	files, err := compiler.New().Generate(u, "ruby", compiler.Options{})
	if err != nil {
		t.Fatalf("generate ruby resource-bounds unit: %v", err)
	}
	if plantFrom != "" {
		files = maps.Clone(files)
		planted := false
		for name := range files {
			if !strings.HasSuffix(name, "/_table/json_scan.rb") {
				continue
			}
			src := string(files[name])
			if !strings.Contains(src, plantFrom) {
				t.Fatalf("%s does not carry the planted pattern %q", name, plantFrom)
			}
			files[name] = []byte(strings.ReplaceAll(src, plantFrom, plantTo))
			planted = true
		}
		if !planted {
			t.Fatal("the backend emitted no _table/json_scan.rb")
		}
	}
	dir := t.TempDir()
	rubyHostileWriteTree(t, dir, files)
	return dir
}

type rubyResourceBoundsResult struct {
	verdict   string
	reason    string
	allocated int
	steps     int
	bytes     int
}

func rubyResourceBoundsRun(t *testing.T, dir string) map[string]rubyResourceBoundsResult {
	t.Helper()
	inputs := map[string][]byte{"main.rb": []byte(rubyResourceBoundsProbe)}
	for name, text := range rubyResourceBoundsDocs() {
		inputs[name] = []byte(text)
	}
	got := map[string]rubyResourceBoundsResult{}
	for _, interpreter := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		stdout, stderr, err := rubyrun.Try(t, interpreter, dir, "main.rb", inputs)
		if err != nil || stderr != "" {
			t.Fatalf("%s: resource-bounds probe failed: %v\nstderr:\n%s\nstdout:\n%s", filepath.Base(interpreter), err, stderr, stdout)
		}
		for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 6 {
				t.Fatalf("bad resource-bounds probe line %q", line)
			}
			var r rubyResourceBoundsResult
			r.verdict, r.reason = fields[1], fields[2]
			if _, err := fmt.Sscanf(fields[3], "%d", &r.allocated); err != nil {
				t.Fatalf("%s: bad allocated %q: %v", fields[0], fields[3], err)
			}
			if _, err := fmt.Sscanf(fields[4], "%d", &r.steps); err != nil {
				t.Fatalf("%s: bad steps %q: %v", fields[0], fields[4], err)
			}
			if _, err := fmt.Sscanf(fields[5], "%d", &r.bytes); err != nil {
				t.Fatalf("%s: bad bytes %q: %v", fields[0], fields[5], err)
			}
			if prev, ok := got[fields[0]]; ok {
				if prev.verdict != r.verdict || prev.reason != r.reason || prev.bytes != r.bytes {
					t.Fatalf("%s: the two pinned Ruby interpreters disagree: %+v vs %+v", fields[0], prev, r)
				}
				r.allocated = max(r.allocated, prev.allocated)
				r.steps = max(r.steps, prev.steps)
			}
			got[fields[0]] = r
		}
	}
	return got
}

func TestSchemaRubyResourceBoundsHostileShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	dir := rubyResourceBoundsUnit(t, "", "")
	got := rubyResourceBoundsRun(t, dir)
	wants := rubyResourceBoundsWants()
	for _, name := range slices.Sorted(maps.Keys(wants)) {
		r, ok := got[name]
		if !ok {
			t.Fatalf("shape %s: the probe wrote no line", name)
		}
		want := wants[name]
		if r.verdict != want.verdict || r.reason != want.reason {
			t.Fatalf("shape %s: %s %s, want %s %s", name, r.verdict, r.reason, want.verdict, want.reason)
		}
		budget := int(rubyAllocationFactorF*float64(r.bytes)) + int(rubyAllocationSlackKB*1024)
		if r.allocated > budget {
			t.Fatalf("shape %s allocated %d bytes, over budget %d", name, r.allocated, budget)
		}
		stepBudget := 256*r.bytes + (1 << 21)
		if r.steps > stepBudget {
			t.Fatalf("shape %s cost %d line events, over linear budget %d", name, r.steps, stepBudget)
		}
	}
}

func TestSchemaRubyResourceBoundsPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	guarded, ok := rubyResourceBoundsRun(t, rubyResourceBoundsUnit(t, "", ""))["deep-nesting"]
	if !ok || guarded.verdict != "REFUSED" || guarded.reason != "malformed" {
		t.Fatalf("with the depth guard in place deep-nesting must refuse malformed, got %+v", guarded)
	}
	planted, ok := rubyResourceBoundsRun(t, rubyResourceBoundsUnit(t, "fail_scan if depth > MAX_DEPTH", "fail_scan if depth > 100_000"))["deep-nesting"]
	if !ok {
		t.Fatal("the planted run wrote no deep-nesting line")
	}
	if planted.verdict == "REFUSED" && planted.reason == "malformed" {
		t.Fatalf("the planted pattern (depth cap raised) did not change deep-nesting verdict: %+v", planted)
	}
}

func TestSchemaRubyHostileLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-ruby-runs-hostile-corpus.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"schema-ruby-runs-hostile-corpus:",
		"schema-ruby-runs-hostile-corpus-negative-control:",
		"schema-ruby-resource-bounds:",
		"schema-ruby-resource-bounds-negative-control:",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("make/schema-ruby-runs-hostile-corpus.mk does not carry %q", want)
		}
	}
}
