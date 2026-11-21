package tablewire_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/mas-bandwidth/schema/v3/compiler"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// An announcement's build stamp changes with WireLaw. Preserve the actual old
// bytes and vocabulary instead of overwriting that cross-build read fixture.
func TestLegacyAnnouncementRetainsVocabularyAcrossWireLaw(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{"testdata/identity-v2/NonCanonical.schema"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/identity-v2/legacy-announcement.bin")
	if err != nil {
		t.Fatal(err)
	}
	var old, current tablewire.Vocabulary
	var report tabletext.Report
	if err := old.AnnounceRead(data, &report); err != nil || !report.Silent() {
		t.Fatalf("actual old announcement: %v %+v", err, report)
	}
	report = tabletext.Report{}
	if err := current.AnnounceRead(tablewire.Announce(u), &report); err != nil || !report.Silent() {
		t.Fatalf("current announcement: %v %+v", err, report)
	}
	if old.BuildVersion() == 0 || old.BuildVersion() == current.BuildVersion() || current.BuildVersion() != ir.BuildVersion(u) {
		t.Fatal("build epochs were not kept distinct")
	}
	if !reflect.DeepEqual(old.Entries(), current.Entries()) {
		t.Fatal("WireLaw change altered the unchanged source's message vocabulary")
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Message"))
	body, err := os.ReadFile("../../testdata/wire/non-canonical/message-canonical.bin")
	if err != nil {
		t.Fatal(err)
	}
	report = tabletext.Report{}
	ok, err := tablewire.DecodeMessage(m, inst, body, &old, &report)
	if err != nil || !ok || !report.Silent() {
		t.Fatalf("message under its actual old announcement: ok=%v err=%v report=%+v", ok, err, report)
	}
}
