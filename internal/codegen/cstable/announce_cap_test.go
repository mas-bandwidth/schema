package cstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY OF MORE THAN 4096 ENTRIES
// (docs/SPEC-TABLES.md §3.3, ruling R8 follow-up): a receiver at the default
// maximum refuses it as vocabulary_too_large, so Announce and AnnounceMeasure
// answer nothing for it. A vocabulary of exactly 4096 is written and read.
func TestCSharpAnnouncementWriterRefusesMoreThan4096Entries(t *testing.T) {
	t15RunCS(t, tablewire.AnnounceSchema(4096), `
int n = Idcap.Schema.AnnounceMeasure();
if (n <= 0) throw new Exception("4096 entries: AnnounceMeasure refused");
var bytes = Idcap.Schema.Announce();
if (bytes.Length != n) throw new Exception("4096 entries: Announce length " + bytes.Length + " != " + n);
var report = new Idcap.TableReport();
var verdict = Idcap.Schema.AnnounceRead(new Idcap.TableVocabulary(), bytes, report);
if (verdict.ToString() != "Ok") throw new Exception("4096 entries: the default-maximum read answered " + verdict);
`)
	t15RunCS(t, tablewire.AnnounceSchema(4097), `
if (Idcap.Schema.AnnounceMeasure() >= 0) throw new Exception("4097 entries: AnnounceMeasure did not refuse");
if (Idcap.Schema.Announce().Length != 0) throw new Exception("4097 entries: Announce wrote bytes");
`)
}
