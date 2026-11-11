package darttable

// THE ANNOUNCEMENT'S ENTRY CAPACITY ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// "A payload longer than the caller's entry bound refuses by name" (§3.3), and
// the caller's bound is both facts it handed in: TableVocabulary.maxEntries and
// the length of the entries list the announced entries are written into. A
// vocabulary whose list is shorter than maxEntries must refuse a longer
// announcement with vocabulary_too_large; writing past the list is a RangeError
// out of announceRead, so the peer would decide whether the reader throws.
//
// Both tests drive the emitted Dart: the runtime and the unit's own 45-entry
// announcement are the emitters' output, run by the leg's own toolchain.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartAnnounceEntryCapacity reads a 45-entry announcement into a
// TableVocabulary with four entry slots and the default maxEntries, and
// requires the named refusal with no exception; a list of exactly the entry
// count is accepted.
func TestDartAnnounceEntryCapacity(t *testing.T) {
	t.Parallel()
	u, entries := announceCapacityUnit(t)
	body := announceCapacityBody(tablewire.Announce(u), entries)
	out, err := runAnnounceProbe(t, u, body)
	if err != nil {
		t.Fatalf("announceRead entry capacity: %v\n%s", err, out)
	}
}

// TestDartAnnounceEntryCapacityNegativeControl removes the entries.length bound
// from the emitted runtime in memory and requires the same probe red.
func TestDartAnnounceEntryCapacityNegativeControl(t *testing.T) {
	t.Parallel()
	u, entries := announceCapacityUnit(t)
	body := announceCapacityBody(tablewire.Announce(u), entries)
	out, err := runAnnounceProbe(t, u, body)
	if err != nil {
		t.Fatalf("the emitted announceRead does not hold the entry capacity rule, so the control has nothing to break: %v\n%s", err, out)
	}
	const bound = "seen >= vocabulary.maxEntries || seen >= vocabulary.entries.length"
	if !strings.Contains(dartMessageAnnounceRuntime, bound) {
		t.Fatalf("the emitted announceRead carries no entries.length bound to remove")
	}
	sabotaged := strings.Replace(dartMessageAnnounceRuntime, bound, "seen >= vocabulary.maxEntries", 1)
	out, err = runAnnounceProbeWithRuntime(t, u, body, sabotaged)
	if err == nil {
		t.Fatalf("negative control passed: without the entries.length bound the short list stayed green:\n%s", out)
	}
	if !strings.Contains(out, "RangeError") && !strings.Contains(out, "short list") {
		t.Fatalf("negative control failed for a different reason: %v\n%s", err, out)
	}
}

// announceCapacityUnit is the message corpus unit, whose announcement carries
// 45 entries: more than the four-slot list and fewer than maxEntries' default.
func announceCapacityUnit(t *testing.T) (*ir.Unit, int) {
	t.Helper()
	u := loadDirUnit(t, "../../../tables/messages")
	entries := len(tablewire.MessageVocabulary(u))
	if entries <= 4 {
		t.Fatalf("the unit's vocabulary has %d entries; the short-list case needs more than four", entries)
	}
	if entries >= 4096 {
		t.Fatalf("the unit's vocabulary has %d entries; it does not fit the default maxEntries", entries)
	}
	return u, entries
}

// announceCapacityBody is the probe: the four-slot list refused by name, then
// a list of exactly the entry count accepted.
func announceCapacityBody(wire []byte, entries int) string {
	return fmt.Sprintf(`
  final valid = Uint8List.fromList(%s);
  final shortList = t.TableVocabulary(
      entries: List<t.TableMessageEntry>.generate(4, (i) => t.TableMessageEntry(0, 0)));
  final shortReport = t.TableReport();
  check(!t.announceRead(shortList, valid, shortReport),
      'short list: the %d-entry announcement was not refused');
  check(shortReport.refused && shortReport.reason == 'vocabulary_too_large',
      'short list: reason=${shortReport.reason} refused=${shortReport.refused}');
  check(shortList.count == 0, 'short list: count=${shortList.count} want=0');
  final exactList = t.TableVocabulary(
      entries: List<t.TableMessageEntry>.generate(%d, (i) => t.TableMessageEntry(0, 0)));
  final exactReport = t.TableReport();
  check(t.announceRead(exactList, valid, exactReport),
      'exact list: a %d-entry list refused the %d-entry announcement: %s');
  check(exactList.announced && !exactReport.refused,
      'exact list: announced=${exactList.announced} refused=${exactReport.refused} reason=${exactReport.reason}');
  check(exactList.count == %d, 'exact list: count=${exactList.count} want=%d');
`, dartBytes(wire), entries, entries, entries, entries, "${exactReport.reason} refused=${exactReport.refused}", entries, entries)
}
