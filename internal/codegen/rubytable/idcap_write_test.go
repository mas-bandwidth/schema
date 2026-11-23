package rubytable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestRubyWriterRefusesMoreThan4096Ids holds the Ruby variable-form writer to
// the id table's maximum (docs/SPEC-TABLES.md §3, ruling R8). Ruby is on
// neither list of the write-side rule, so it refuses in every build: save
// raises InvalidValue for a value that needs 4097 ids and writes nothing. The
// values that need 4095 and 4096 are written, carry that count word, and read
// back. The unit is tablewire.IdCapSchema.
func TestRubyWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	files, err := Generate(loadUnit(t, tablewire.IdCapSchema()))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := `# frozen_string_literal: true

require_relative 'idcap'

def value(x, y)
  v = Idcap::Root.new
  (1..4094).each { |k| v.keys[k] = 1 }
  v.x = x
  v.y = y
  v
end

[[0, 4095], [1, 4096]].each do |x, ids|
  b = value(x, 0).save
  raise "ids #{ids}: count word #{b.byteslice(-8, 8).unpack1('Q<')}" unless b.byteslice(-8, 8).unpack1('Q<') == ids
  raise "ids #{ids}: read" unless Idcap::Root.load(b).x == x
end
begin
  value(1, 1).save
  raise '4097 ids were written'
rescue Idcap::InvalidValue
  puts 'ok'
end
`
	out := runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(probe)})
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("probe answered %q", out)
	}
}
