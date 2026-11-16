package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Projection to a reader default does not prove the sender wrote a default.
func TestGoBirthMessageChecksOriginalSpelling(t *testing.T) {
	for _, tc := range []struct {
		name, writer, reader, text string
		unknown                    int
		foreign                    bool
	}{
		{"enum", `package probe
enum E {Known,Newer}
fixed table Root {items [2..3]E}`, `package probe
enum E {Known}
fixed table Root {items [2..3]E}`, `{"items":["Newer","Newer"]}`, 2, false},
		{"nested enum", `package probe
enum E {Known,Newer}
type Child {n int32=55
 value E}
fixed table Root {items [1..2]Child}`, `package probe
enum E {Known}
type Child {n int32=55
 value E}
fixed table Root {items [1..2]Child}`, `{"items":[{"value":"Newer"}]}`, 1, false},
		{"foreign record", `package probe
type Child {n int32=55
 added int32}
fixed table Root {items [1..2]Child}`, `package probe
type Child {n int32=55}
fixed table Root {items [1..2]Child}`, `{"items":[{"added":7}]}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitOf(t, tc.writer)
			m := tabletext.NewModel(u)
			v := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if !m.Read(v, []byte(tc.text), &report) {
				t.Fatal(report)
			}
			wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v})
			if err != nil {
				t.Fatal(err)
			}
			probe := fmt.Sprintf(`package probe
import "testing"
func TestOriginalSpelling(t *testing.T){var storage [256]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);var report TableReport;if !AnnounceRead(&vocab,%#v,&report){t.Fatal(report)};vocab.BuildVersion=BuildVersion;var out [1]Root;if n,ok:=RootLoadMessages(out[:],&vocab,%#v,&report);!ok||n!=1||report.Unknown!=%d{t.Fatalf("projected default refused: n=%%d ok=%%v report=%%+v",n,ok,report)}}
`, tablewire.Announce(u), wire, tc.unknown)
			// Current message policy refuses unknown FIELDS in an own-build body;
			// foreign records retain their sender's build, while enum values test
			// original-spelling preservation even under the claimed own build.
			if tc.foreign {
				probe = strings.Replace(probe, "vocab.BuildVersion=BuildVersion;", "", 1)
			}
			runGenerated(t, tc.reader, probe)
		})
	}
}
