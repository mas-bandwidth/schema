package unitytable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestAnnouncementEntryBudgetUnity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated Unity announcement entry budgets")
	const emptySchema = "package entrybudget\n\ntable Probe { x int32 }\n"
	const oneSchema = "package entrybudget\n\nfixed table Probe { x int32 }\n"
	empty := tablewire.Announce(fixedproof.ParseCheckUnit(t, emptySchema))
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, oneSchema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityAnnouncementEntryBudgetMain, map[string][]byte{"empty.bin": empty})
	if err != nil || code != 0 {
		t.Fatalf("Unity announcement entry budget (exit %d): %v\n%s", code, err, out)
	}
}

const unityAnnouncementEntryBudgetMain = `using System;
using System.IO;
using Entrybudget;

static class ProbeMain
{
    static int Main()
    {
        int bad = 0;
        void Check(string name, ReadOnlySpan<byte> peer, Schema.TableVocabulary v, int count, bool accepts)
        {
            var report = new Schema.TableReport();
            bool ok = Schema.AnnounceRead(v, peer, ref report);
            if (ok != accepts || (accepts ? report.Refused || !v.Announced || v.Count != count
                                         : !report.Refused || report.Damaged || report.ReasonText != "vocabulary_too_large"
                                           || v.Announced || v.Count != 0))
            {
                Console.WriteLine("FAIL " + name + ": " + report.ReasonText);
                bad++;
            }
        }
        var empty = File.ReadAllBytes("empty.bin");
        var one = Schema.Announce();
        Check("empty/default", empty, new Schema.TableVocabulary(), 0, true);
        Check("empty/zero", empty, new Schema.TableVocabulary { MaxEntries = 0 }, 0, true);
        Check("empty/one", empty, new Schema.TableVocabulary { MaxEntries = 1 }, 0, true);
        Check("one/default", one, new Schema.TableVocabulary(), 1, true);
        Check("one/zero", one, new Schema.TableVocabulary { MaxEntries = 0 }, 0, false);
        Check("one/one", one, new Schema.TableVocabulary { MaxEntries = 1 }, 1, true);
        Check("one/zero-bytes", one, new Schema.TableVocabulary { MaxEntries = 1, MaxBytes = 0 }, 0, false);
        return bad == 0 ? 0 : 1;
    }
}
`
