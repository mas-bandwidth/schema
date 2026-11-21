package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestCfTopProbe(t *testing.T) {
	m := covModel(t, "package probe\n\nfixed table Root\n{\n    cf float32 | min = -1.0, max = 0.1, resolution = 0.001\n}\n")
	var voc Vocabulary
	if err := voc.AnnounceRead(Announce(m.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	msg, err := EncodeMessage(m, covPlace(t, m, "Root", `{"cf":0.1}`))
	t.Logf("encode 0.1: err=%v", err)
	back := m.New(m.Lookup("Root"))
	v := voc
	var rep tabletext.Report
	ok, err := DecodeMessage(m, back, msg, &v, &rep)
	t.Logf("decode: ok=%v err=%v rep=%+v", ok, err, rep)
	text, err := m.Write(back)
	t.Logf("decoded json: %s err=%v", text, err)
	_, err = EncodeMessage(m, back)
	t.Logf("re-encode message: err=%v", err)
	_, err = EncodeFixed(m, back)
	t.Logf("re-encode fixed: err=%v", err)
	_, err = Encode(m, back)
	t.Logf("re-encode variable: err=%v", err)
}
