package rubytable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// announce and announce_measure raise Refused for a unit with 4097 and write
// the one with exactly 4096, which announce_read accepts. Ruby refuses in every
// build. Red when the refusal is removed.
func TestRubyAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		schema := strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1)
		files, err := Generate(loadUnit(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := "# frozen_string_literal: true\n\nrequire_relative 'idcap'\n\n"
		if row.refused {
			probe += `[:announce, :announce_measure].each do |call|
  begin
    Idcap.public_send(call)
    raise '4097 entries: the announcement was written'
  rescue Idcap::Refused => e
    raise "reason #{e.reason}" unless e.reason == 'vocabulary_too_large'
  end
end
puts 'ok'
`
		} else {
			probe += `raw = Idcap.announce
raise 'measure' unless Idcap.announce_measure == raw.bytesize && raw.bytesize.positive?
Idcap.announce_read(Idcap::MessageVocabulary.new, raw)
puts 'ok'
`
		}
		out := runBothRubyPins(t, dir, "probe.rb", map[string][]byte{"probe.rb": []byte(probe)})
		if strings.TrimSpace(out) != "ok" {
			t.Fatalf("%d entries: probe answered %q", row.entries, out)
		}
	}
}
