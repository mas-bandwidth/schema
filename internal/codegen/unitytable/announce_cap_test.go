package unitytable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY OF MORE THAN 4096 ENTRIES
// (docs/SPEC-TABLES.md §3.3, ruling R8 follow-up), for C# (unity): 4096 entries
// are written and read at the default maximum; 4097 make Announce and
// AnnounceMeasure answer nothing.
func TestUnityAnnouncementWriterRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect announcement vocabulary maximum")
	run := func(entries int, body string) {
		u := fixedproof.ParseCheckUnit(t, tablewire.AnnounceSchema(entries))
		files, err := unityGenerateFiles(u, nil)
		if err != nil {
			t.Fatalf("unityGenerateFiles: %v", err)
		}
		mustRunUnityVTProbe(t, files, "using System;\nusing Idcap;\nclass Program { static int Main() {\n"+body+"\nConsole.WriteLine(\"UNITY_ANN_OK\"); return 0; } }\n")
	}
	run(4096, `
int n = Schema.AnnounceMeasure();
if (n <= 0) throw new Exception("4096 entries: AnnounceMeasure refused");
var bytes = Schema.Announce();
if (bytes.Length != n) throw new Exception("4096 entries: Announce length");
var report = new Schema.TableReport();
if (!Schema.AnnounceRead(new Schema.TableVocabulary(), bytes, ref report)) throw new Exception("4096 entries: the default-maximum read refused");
`)
	run(4097, `
if (Schema.AnnounceMeasure() >= 0) throw new Exception("4097 entries: AnnounceMeasure did not refuse");
if (Schema.Announce().Length != 0) throw new Exception("4097 entries: Announce wrote bytes");
`)
}
