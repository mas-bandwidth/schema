package roadmap

import (
	"fmt"
	"regexp"
	"strings"
)

// GlennFiedler is the one author whose commits may change ROADMAP.md outside
// the nova-work markers: the title, headings and prose there are his, written
// by hand, and never generated or edited by anyone else.
const GlennFiedler = "Glenn Fiedler"

// SourceDataLink is the one non-table line a generated block may carry.
const SourceDataLink = "[Source data](docs/roadmap.sexp)"

var (
	startMarkerRx = regexp.MustCompile(`^<!-- nova-work:([a-z0-9-]+):start -->$`)
	endMarkerRx   = regexp.MustCompile(`^<!-- nova-work:([a-z0-9-]+):end -->$`)
)

// OutsideMarkers is raw with every block's contents removed and its marker
// lines kept: the hand-written part of the page, byte for byte.
func OutsideMarkers(raw string) string {
	var b strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(raw, "\n") {
		bare := strings.TrimSuffix(line, "\n")
		switch {
		case startMarkerRx.MatchString(bare):
			inside = true
			b.WriteString(line)
		case endMarkerRx.MatchString(bare):
			inside = false
			b.WriteString(line)
		case !inside:
			b.WriteString(line)
		}
	}
	return b.String()
}

// CheckBlocksHoldOnlyTables refuses a generated block carrying anything but
// table lines, blank lines and the source-data link: a paragraph inside a
// block is prose the renderer was made to write.
func CheckBlocksHoldOnlyTables(raw string) error {
	inside := ""
	for n, line := range strings.Split(raw, "\n") {
		if m := startMarkerRx.FindStringSubmatch(line); m != nil {
			inside = m[1]
			continue
		}
		if endMarkerRx.MatchString(line) {
			inside = ""
			continue
		}
		if inside == "" {
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "|") || line == SourceDataLink {
			continue
		}
		return fmt.Errorf("ROADMAP.md:%d: block %q carries a line that is neither table nor source link: %q", n+1, inside, line)
	}
	return nil
}

// CheckHandTextChange refuses a commit by anyone but Glenn Fiedler that
// changes ROADMAP.md outside the nova-work markers. before is the page at the
// commit's parent ("" when it did not exist), after the page at the commit.
func CheckHandTextChange(commit, author, before, after string) error {
	if OutsideMarkers(before) == OutsideMarkers(after) || author == GlennFiedler {
		return nil
	}
	return fmt.Errorf("%s by %s changes ROADMAP.md outside the nova-work markers%s; that text is %s's, edited by hand, and only the tables are generated (make roadmap)",
		commit, author, firstDifference(OutsideMarkers(before), OutsideMarkers(after)), GlennFiedler)
}

func firstDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf(" (first at hand-text line %d: %q became %q)", i+1, x, y)
		}
	}
	return ""
}
