package gotable

import (
	"strings"
	"testing"
)

// An explicit birth-default field is still refused for the announced own build.
func TestGoBirthMessageRefusesExplicitDefault(t *testing.T) {
	out, err := runGeneratedEdited(t, `package probe
fixed table Root {items [2..3]int8}`, `package probe
import "testing"
func TestLegacySpelling(t *testing.T){var v Root;RootReset(&v);var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);var report TableReport;announcement:=make([]byte,AnnounceMeasure());Announce(announcement);if !AnnounceRead(&vocab,announcement,&report){t.Fatal(report)};out:=make([]byte,RootMeasureMessages([]*Root{&v},nil));if RootSaveMessages([]*Root{&v},out,nil)!=int64(len(out))||len(out)<=3{t.Fatal("legacy writer mutation did not emit a field")};var back [1]Root;if n,ok:=RootLoadMessages(back[:],&vocab,out,&report);ok||n!=0||report.Err()==nil{t.Fatalf("own birth-default spelling admitted: %+v",report)}}
`, func(files map[string][]byte) {
		changed := false
		for name, data := range files {
			if !strings.HasSuffix(name, "TableMessage.go") {
				continue
			}
			s := string(data)
			start := strings.Index(s, "func RootSaveMessageBody(")
			if start < 0 {
				continue
			}
			end := strings.Index(s[start:], "\n}\n")
			if end < 0 {
				t.Fatal("writer body end")
			}
			end += start + 3
			body := s[start:end]
			needle := "allBirth := value.ItemsCount == 2"
			if strings.Count(body, needle) != 1 {
				t.Fatal("birth writer hook drift")
			}
			body = strings.Replace(body, needle, "allBirth := false", 1)
			files[name] = []byte(s[:start] + body + s[end:])
			changed = true
		}
		if !changed {
			t.Fatal("legacy writer mutation missed")
		}
	})
	if err != nil {
		t.Fatalf("generated legacy-spelling probe: %v\n%s", err, out)
	}
}
