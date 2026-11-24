package rubytable

import (
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
// The shared cases are the conformance `retain-refusal` rows
// (testdata/conformance/tables/MANIFEST.txt): one store through a retaining
// read, a save, a refused read (form byte 3, two bytes, no bytes, the message
// form, damage mid-body) and a second save, answered by every leg.

// TestRubyRetainResetsBeforeOpen is the fast half: the
// emitted load_retain empties the store before the integrity check and the
// file read, so no refusal can leave it as an earlier load left it.
func TestRubyRetainResetsBeforeOpen(t *testing.T) {
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

// TestRubyRetainRefusalEmptiesTheStore is the run-time proof of the one
// refusal the shared corpus cannot carry, on both pinned interpreters: a
// failed integrity check (no conformance unit declares `integrity`). The
// frame, short, empty, form-two and body refusals are the conformance
// `retain-refusal` rows every leg answers (testdata/conformance/tables/
// MANIFEST.txt, test/conformance/ruby/surfaces/retain-refusal.rb).
func TestRubyRetainRefusalEmptiesTheStore(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
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
