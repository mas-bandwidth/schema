package rubytable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4), and a file refused
// AT ITS FRAME, before any body is walked, is such a load: a form byte this
// reader does not take, a file too short to carry its id trailer, a failed
// integrity check. The retaining load hands the caller's store back empty on
// every refusal, not only on an accepted read: before this, load_retain reset
// the store inside the capture walk, which runs only after the plain read
// accepted, so a refused file left an earlier load's captures (its used and
// id_used counts, its loaded root) in the store.
//
// The committed corpus cases are the framing rows
// testdata/wire/tables/framing/frame_form_three_at_variable_root.bin (a
// fixed-form header at the variable root graphdemo ListNode) and
// node_count_disagrees.bin (body damage), beside node_clean.bin, their
// accepted twin on the same root.

// TestRubyRetainingLoadResetsBeforeItOpensTheFile is the fast half: the
// emitted load_retain empties the store before the integrity check and the
// file read, so no refusal can leave it as an earlier load left it.
func TestRubyRetainingLoadResetsBeforeItOpensTheFile(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, schema, open string }{
		{"plain", "package probe\ntable Root { n int32 }\n", "SchemaTable.load_table(Root, data, report, retain)"},
		{"crc32c", "package probe\ntable Root | integrity = \"crc32c\" { n int32 }\n", "data = SchemaTable.open_crc(data)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			files, err := Generate(loadUnit(t, c.schema))
			if err != nil {
				t.Fatal(err)
			}
			var all strings.Builder
			for _, data := range files {
				all.Write(data)
			}
			src := all.String()
			start := strings.Index(src, "    def self.load_retain(data, retain, report = nil)")
			if start < 0 {
				t.Fatal("the retaining load is not emitted")
			}
			body := src[start:]
			if end := strings.Index(body, "\n    end\n"); end >= 0 {
				body = body[:end]
			}
			reset := strings.Index(body, "retain.reset")
			open := strings.Index(body, c.open)
			if open < 0 {
				t.Fatalf("the retaining load does not open the file with %q:\n%s", c.open, body)
			}
			if reset < 0 || reset > open {
				t.Fatalf("the retaining load opens the file before it empties the store:\n%s", body)
			}
		})
	}
}

// TestRubyRetainingLoadEmptiesTheStoreOnARefusal is the run-time proof, on
// both pinned interpreters: a store an earlier load left a capture in is
// handed back empty by every refused retaining load, and an accepted load of
// the clean twin is the control.
func TestRubyRetainingLoadEmptiesTheStoreOnARefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	root := filepath.Join("..", "..", "..")
	graph, err := Generate(loadUnitDir(t, filepath.Join(root, "tables", "pointers")))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range []string{"frame_form_three_at_variable_root", "node_count_disagrees", "node_clean"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name+".bin"] = data
	}
	inputs["main.rb"] = []byte(`# frozen_string_literal: true

require_relative 'graphdemo'

def seeded
  keep = Graphdemo::TableRetain.new
  keep.used = 7
  keep.id_used = 1
  keep.loaded_root = 'an earlier load'
  keep
end

cases = %w[frame_form_three_at_variable_root node_count_disagrees].map { |n| [n, File.binread("#{n}.bin")] }
cases += [['form_two', "\x02\x00".b + ("\x00".b * 8)], ['empty', ''.b], ['short', "\x01\x00".b]]
cases.each do |name, wire|
  keep = seeded
  begin
    Graphdemo::ListNode.load_retain(wire, keep)
    raise "#{name}: the retaining load accepted"
  rescue Graphdemo::Refused
    nil
  end
  got = [keep.used, keep.id_used, keep.loaded_root]
  raise "#{name}: the store is #{got.inspect}" unless got == [0, 0, nil]
end
keep = seeded
value = Graphdemo::ListNode.load_retain(File.binread('node_clean.bin'), keep)
raise 'the clean twin left the store wrong' unless keep.used.zero? && keep.id_used.zero? && keep.loaded_root.equal?(value)
puts 'retain-refusal'
`)
	dir := t.TempDir()
	writeTree(t, dir, graph)
	if out := strings.TrimSpace(runBothRubyPins(t, dir, "main.rb", inputs)); out != "retain-refusal" {
		t.Fatalf("a refused retaining load left the store as it was: %s", out)
	}

	crc, err := Generate(loadUnit(t, "package probe\ntable Root | integrity = \"crc32c\" { n int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	crcDir := t.TempDir()
	writeTree(t, crcDir, crc)
	crcMain := []byte(`# frozen_string_literal: true

require_relative 'probe'

value = Probe::Root.new
value.n = 7
wire = value.save
keep = Probe::TableRetain.new
loaded = Probe::Root.load_retain(wire, keep)
raise 'the sealed file did not load' unless loaded.n == 7 && keep.loaded_root.equal?(loaded)
keep.used = 7
keep.id_used = 1
bad = wire.dup
bad.setbyte(bad.bytesize - 1, bad.getbyte(bad.bytesize - 1) ^ 1)
begin
  Probe::Root.load_retain(bad, keep)
  raise 'the retaining load accepted a failed integrity check'
rescue Probe::Refused => e
  raise "refused #{e.reason}" unless e.reason == 'integrity_mismatch'
end
got = [keep.used, keep.id_used, keep.loaded_root]
raise "the store is #{got.inspect}" unless got == [0, 0, nil]
puts 'retain-refusal'
`)
	if out := strings.TrimSpace(runBothRubyPins(t, crcDir, "main.rb", map[string][]byte{"main.rb": crcMain})); out != "retain-refusal" {
		t.Fatalf("a refused retaining load left the store as it was: %s", out)
	}
}
