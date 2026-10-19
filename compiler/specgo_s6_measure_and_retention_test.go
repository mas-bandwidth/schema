// TestSpecGoS6MeasureAndRetention pins §6.2, §6.3, §6.5 and §6.6 to what
// Go's table backend already does. Each row is one fact a cold reader found
// stale on the page, with the Go file and line that carries it; the page is
// read as text so a rule that moves back to the old wording fails here.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecGoS6MeasureAndRetention(t *testing.T) {
	t.Parallel()

	page, err := os.ReadFile(filepath.Join("..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("SPEC-TABLES.md: %v", err)
	}
	text := strings.Join(strings.Fields(string(page)), " ")

	rows := []struct {
		name  string
		want  string
		stale string
	}{
		{
			name: "§6.2 AllocWString is ahead of its implementation",
			want: "`AllocWString( n )` — the wide member, which §2.5 specifies ahead of its implementation and the checker refuses by name — hand",
		},
		{
			name: "§6.3 *wstring blob is ahead of its implementation",
			want: "The blob is specified ahead of its implementation (§2.5), and the checker refuses `*wstring` by name.",
		},
		{
			name:  "§6.5 where TableRefuseReason is carried",
			want:  "an error value in Go",
			stale: "No port spells the enum yet",
		},
		{
			name:  "§6.5 data_cycle has a carrier",
			want:  "`data_cycle` IS CARRIED, and Go is its first carrier",
			stale: "THE ONE VALUE WITH NO CARRIER TODAY",
		},
		{
			name:  "§6.5 LoadMeasureReason refuses an unreadable trailer",
			want:  "Go's `LoadMeasureReason` answers `wire_damaged` for it, the exception §11 states.",
			stale: "writes nothing either",
		},
		{
			name: "§6.5 LoadMeasure and LoadMeasureReason disagree on damage",
			want: "The plain `LoadMeasure` and `LoadMeasureReason` PURPOSELY DISAGREE on a damaged wire.",
		},
		{
			name: "§6.5 Go's LoadMeasure is combined size",
			want: "Go's `LoadMeasure` answers the data and attribution as one COMBINED size",
		},
		{
			name: "§6.5 FromJson on a locked builder",
			want: "the builder `FromJson` and `FromJsonWith` answer `false` on before they read, leaving the report empty",
		},
		{
			name:  "§6.6 backend status names Go",
			want:  "Backend status: the C++ REFERENCE, the ORACLE and GO carry it.",
			stale: "the C++ REFERENCE and the ORACLE carry it, and no port does",
		},
		{
			name: "§6.6 retention keeps an integrity trailer",
			want: "`LoadRetain` opens the CRC-32C trailer before it decodes",
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if !strings.Contains(text, row.want) {
				t.Errorf("docs/SPEC-TABLES.md does not state %q", row.want)
			}
			if row.stale != "" && strings.Contains(text, row.stale) {
				t.Errorf("docs/SPEC-TABLES.md still carries the stale sentence %q", row.stale)
			}
		})
	}
}
