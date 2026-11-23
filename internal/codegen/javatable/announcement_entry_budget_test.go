package javatable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudgetJava(t *testing.T) {
	t.Parallel()
	const emptySchema = "package entrybudget\n\ntable Probe { x int32 }\n"
	const oneSchema = "package entrybudget\n\nfixed table Probe { x int32 }\n"
	empty := tablewire.Announce(unitFrom(t, emptySchema))
	one := unitFrom(t, oneSchema)
	main := fmt.Sprintf(announceEntryBudgetJavaMain, hexOf(empty))
	if got := strings.TrimSpace(runMessageJava(t, one, main)); got != "" {
		t.Fatal(got)
	}
}

const announceEntryBudgetJavaMain = `    public static void main(String[] args) {
        final byte[] empty = fromHex("%s");
        final byte[] one = new byte[TableVocabulary.announceMeasure()];
        if (TableVocabulary.announce(one) != one.length) { fail("announce"); }
        check("empty/fresh", empty, new TableVocabulary(), 0, true);
        check("one/fresh", one, new TableVocabulary(), 0, false);
        check("empty/reset-zero", empty, receiver(0, 0, 65536), 0, true);
        check("one/reset-zero", one, receiver(0, 0, 65536), 0, false);
        check("empty/reset-one", empty, receiver(1, 1, 65536), 0, true);
        check("one/reset-one", one, receiver(1, 1, 65536), 1, true);
        check("empty/default", empty, receiver(4096, 4096, 65536), 0, true);
        check("empty/zero", empty, receiver(4096, 0, 65536), 0, true);
        check("empty/one", empty, receiver(4096, 1, 65536), 0, true);
        check("one/default", one, receiver(4096, 4096, 65536), 1, true);
        check("one/zero", one, receiver(4096, 0, 65536), 0, false);
        check("one/one", one, receiver(4096, 1, 65536), 1, true);
        check("one/zero-bytes", one, receiver(4096, 1, 0), 0, false);
    }

    private static TableVocabulary receiver(int capacity, int entries, int bytes) {
        final TableVocabulary reader = new TableVocabulary();
        reader.reset(capacity);
        if (reader.maxEntries != entries) { reader.maxEntries = entries; }
        if (reader.maxBytes != bytes) { reader.maxBytes = bytes; }
        return reader;
    }

    private static void check(String name, byte[] peer, TableVocabulary reader, int count, boolean accepts) {
        final TableVocabulary.Report report = new TableVocabulary.Report();
        final boolean ok = TableVocabulary.announceRead(reader, peer, report);
        if (ok != accepts || report.damaged || reader.count != count || reader.announced != accepts ||
            report.refused == accepts || (!accepts && !"vocabulary_too_large".equals(report.reason))) {
            System.out.println(name + ": accepted=" + ok + ", count=" + reader.count + ", reason=" + report.reason);
        }
    }
`
