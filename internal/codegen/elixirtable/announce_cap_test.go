package elixirtable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up), on the Elixir leg, which refuses in
// every build: announce and announce_measure refuse a unit with 4097
// vocabulary entries and write the one with exactly 4096, which announce_read
// accepts at the default maxima. Red when the refusal is removed.
func TestElixirAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused string
	}{{4096, "false"}, {4097, "true"}} {
		u := unitFrom(t, tablewire.AnnounceSchema(row.entries))
		body := `  n = M.announce_measure()
  a = M.announce()
  if ` + row.refused + ` do
    check(n == -1 and match?({:error, _}, a), "measure #{inspect(n)}, announce refused? #{inspect(is_binary(a))}, want both refused")
  else
    check(is_binary(a) and n == byte_size(a) and n > 0, "the announcement did not write")
    check(match?({:ok, _}, M.announce_read(a)), "the reader refused the written announcement")
  end
`
		if out, err := runMessageProbe(t, u, "/dev/null", body); err != nil {
			t.Fatalf("%d entries: %v\n%s", row.entries, err, out)
		}
	}
}
