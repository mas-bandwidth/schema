package cstable

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudgetCSharp(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C# announcement entry budgets")
	const emptySchema = "package entrybudget\n\ntable Probe { x int32 }\n"
	const oneSchema = "package entrybudget\n\nfixed table Probe { x int32 }\n"
	empty := tablewire.Announce(unitFrom(t, emptySchema))
	t15RunCS(t, oneSchema, fmt.Sprintf(`
        void Check(string name, ReadOnlySpan<byte> peer, Entrybudget.TableVocabulary v, int count, bool accepts)
        {
            var report = new Entrybudget.TableReport();
            var verdict = Entrybudget.Schema.AnnounceRead(v, peer, report);
            if (accepts ? verdict != Entrybudget.Schema.TableWire.Verdict.Ok || report.Refused || !v.Announced || v.Count != count
                        : verdict != Entrybudget.Schema.TableWire.Verdict.Refused || !report.Refused || report.Damaged
                          || report.Reason != "vocabulary_too_large" || v.Announced || v.Count != 0)
            {
                Console.WriteLine("FAIL " + name + ": " + verdict + " " + report.Reason);
                bad++;
            }
        }
        var empty = %s;
        var one = Entrybudget.Schema.Announce();
        Check("empty/default", empty, new Entrybudget.TableVocabulary(), 0, true);
        Check("empty/zero", empty, new Entrybudget.TableVocabulary { MaxEntries = 0 }, 0, true);
        Check("empty/one", empty, new Entrybudget.TableVocabulary { MaxEntries = 1 }, 0, true);
        Check("one/default", one, new Entrybudget.TableVocabulary(), 1, true);
        Check("one/zero", one, new Entrybudget.TableVocabulary { MaxEntries = 0 }, 0, false);
        Check("one/one", one, new Entrybudget.TableVocabulary { MaxEntries = 1 }, 1, true);
        Check("one/zero-bytes", one, new Entrybudget.TableVocabulary { MaxEntries = 1, MaxBytes = 0 }, 0, false);
`, depthBoundaryCSBytes(empty)))
}
