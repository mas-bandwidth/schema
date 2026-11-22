package rubytable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRubyMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of tables/backend,
// client_build 40, written by the Go engine and pinned to it by
// test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Ruby's load_messages. A capacity of one owes "A
// refused batch returns a count of `0`" (docs/SPEC-TABLES.md §3.3): the Ruby
// read raises Refused('batch_too_large') and returns nothing, so no count and
// no body reaches the caller. A capacity of two owes both bodies, each
// carrying 40. A caller's capacity is a resource limit (§6.5), so this row
// runs here, in the leg's own tests, never in the shared suites.
func TestRubyMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	u := loadUnitDir(t, filepath.Join(root, "tables", "backend"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	fixture, err := os.ReadFile(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := `# frozen_string_literal: true

require_relative 'backenddemo'

vocab = Backenddemo::MessageVocabulary.new
Backenddemo.announce_read(vocab, File.binread('conn.bin'))
wire = File.binread('wire.bin')
begin
  Backenddemo.load_messages(Backenddemo::LoginRequest, vocab, wire, 1)
  puts 'one delivered'
rescue Backenddemo::Refused => e
  puts "one refused #{e.reason}"
end
two = Backenddemo.load_messages(Backenddemo::LoginRequest, vocab, wire, 2)
puts "two #{two.length} #{two.map(&:client_build).join(' ')}"
`
	out := strings.TrimSpace(runBothRubyPins(t, dir, "probe.rb", map[string][]byte{
		"probe.rb": []byte(probe),
		"conn.bin": conn,
		"wire.bin": fixture,
	}))
	if want := "one refused batch_too_large\ntwo 2 40 40"; out != want {
		t.Fatalf("capacity fixture:\n got  %q\n want %q", out, want)
	}
}
