package rubytable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const rubyCardsFacadeProbe = `# frozen_string_literal: true

$LOAD_PATH.unshift(File.expand_path(__dir__ || '.'))
require_relative 'facade'

header = "RESULT: x sha=0123456789ab tier: pro\n"
["KIND: ab\rcd\n", "KIND: ab\r\r\n", "NOTE: ab\rcd\n", "DEADLINE: finish within 200000 minutes.\n"].each do |line|
  rejected = false
  begin
    CardsFacade.parse(header + line)
  rescue CardsFacade::CardError => e
    raise 'header refusal class' unless e.reason == 'malformed'

    rejected = true
  end
  raise 'invalid header accepted' unless rejected
end

valid = CardsFacade.parse("RESULT: x sha=0123456789ab tier: pro\r\nKIND: task\r\nbody\r\n")
raise 'CRLF preservation' unless valid.root.kind == 'task' && valid.root.body == "body\r\n"
raise 'long zero deadline' unless CardsFacade.parse("#{header}DEADLINE: finish within #{'0' * 5000} minutes.\n").root.deadline_minutes.zero?
raise 'opaque body' unless CardsFacade.parse("#{header}ordinary prose\nNOTE: ab\rcd\n").root.body == "ordinary prose\nNOTE: ab\rcd\n"

brief = "RESULT: x sha=0123456789ab tier: pro\nPATHS: a, b\nbody\u2028PATHS: still prose"
r = CardsFacade.parse(brief)
raise 'facade body' unless r.root.body == "body\u2028PATHS: still prose"

original = r.wire
old_hash = CardsFacade.task_hash(r)
r.root.body = Cards::BlobRef.new("body\u2028PATHS: still prose")
raise 'blobref hash' unless CardsFacade.task_hash(r) == old_hash

r.root.body = "body\u2028PATHS: still prose"
[['tier', 'bad'], ['holder', 'x' * 33], ['paths', 'x' * 193]].each do |field, val|
  rejected = false
  begin
    CardsFacade.edit(r, field, val)
  rescue CardsFacade::CardError
    rejected = true
  end
  raise 'invalid edit accepted' unless rejected
  raise 'edit mutated on failure' unless r.wire == original && r.root.body == "body\u2028PATHS: still prose"
end

['', "RESULT: x sha=no tier: pro\n", "RESULT: x sha=0123456789ab tier: pro\nprose\nPATHS: a\n", "RESULT: x sha=0123456789ab tier: pro\n\x00"].each do |bad|
  rejected = false
  begin
    CardsFacade.parse(bad)
  rescue CardsFacade::CardError
    rejected = true
  end
  raise 'bad brief accepted' unless rejected
end

CardsFacade.edit(r, 'paths', 'x,y')
raise 'paths edit' unless r.root.paths.map(&:s) == %w[x y] && r.root.body == "body\u2028PATHS: still prose"

CardsFacade.set_paths(r, ['x,y'])
raise 'set_paths literal' unless r.root.paths.map(&:s) == ['x,y']

future = File.binread('future.wire')
rejected_strict = false
begin
  CardsFacade.read_strict(future)
rescue CardsFacade::CardError => e
  raise 'strict reason' unless e.reason == 'unknown-present'

  rejected_strict = true
end
raise 'strict unknown accepted' unless rejected_strict

puts 'facade checks ok'.unpack1('H*')
`

func generateCardsUnit(t *testing.T, root string) map[string][]byte {
	t.Helper()
	u := loadUnitDir(t, filepath.Join(root, "tables", "cards"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRubyCards(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters (cards facade)")
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := generateCardsUnit(t, root)
	facade, err := os.ReadFile(filepath.Join(root, "tables", "cards", "ruby", "facade.rb"))
	if err != nil {
		t.Fatal(err)
	}
	future, err := os.ReadFile(filepath.Join(root, "tables", "cards", "conformance", "cases", "future-note.wire"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	rubyrun.Analyze(t, dir)
	inputs := map[string][]byte{
		"main.rb":     []byte(rubyCardsFacadeProbe),
		"facade.rb":   facade,
		"future.wire": future,
	}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{rubyrun.Run, rubyrun.RunFloor} {
		lines := rubyrun.HexLines(t, run(t, dir, "main.rb", inputs))
		if len(lines) != 1 || string(lines[0]) != "facade checks ok" {
			t.Fatalf("unexpected facade output: %q", lines)
		}
	}
}

func TestRubyCardsFacadeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters (cards facade negative control)")
	root, err := rubyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := generateCardsUnit(t, root)
	source, err := os.ReadFile(filepath.Join(root, "tables", "cards", "ruby", "facade.rb"))
	if err != nil {
		t.Fatal(err)
	}
	future, err := os.ReadFile(filepath.Join(root, "tables", "cards", "conformance", "cases", "future-note.wire"))
	if err != nil {
		t.Fatal(err)
	}
	controls := []struct{ name, anchor, replacement, failure string }{
		{"body", "root.body = rem.empty? ? nil : rem", "root.body = nil", "CRLF preservation"},
		{"header cr", `raise CardError, 'malformed' if hval.include?("\r") || hval.include?("\n")`, "digits = digits", "invalid header accepted"},
		{"line ending", `line = line[0...-1] if line.end_with?("\r")`, `line = line.sub(/\r+\z/, '')`, "invalid header accepted"},
		{"deadline", "root.deadline_minutes = Integer(digits, 10)", "root.deadline_minutes = 1", "long zero deadline"},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			if strings.Count(string(source), control.anchor) != 1 {
				t.Fatalf("%s: control anchor absent or ambiguous", control.name)
			}
			mutant := strings.Replace(string(source), control.anchor, control.replacement, 1)
			dir := t.TempDir()
			writeTree(t, dir, files)
			inputs := map[string][]byte{
				"main.rb":     []byte(rubyCardsFacadeProbe),
				"facade.rb":   []byte(mutant),
				"future.wire": future,
			}
			for _, interp := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
				_, stderr, runErr := rubyrun.Try(t, interp, dir, "main.rb", inputs)
				if runErr == nil || !strings.Contains(stderr, control.failure) {
					t.Errorf("%s control did not fail by name: %v\n%s", control.name, runErr, stderr)
				}
			}
		})
	}
}
