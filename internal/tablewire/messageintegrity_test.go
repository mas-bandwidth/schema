package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func messageIntegrityFixture(t *testing.T) (*tabletext.Model, []*tabletext.Instance, []byte) {
	t.Helper()
	base := "../../testdata/conformance/feature-witnesses/message-integrity"
	src, err := os.ReadFile(filepath.Join(base, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []json.RawMessage
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("want3 corpus bodies, got%d", len(cases))
	}
	model := listModel(t, string(src))
	plain := listModel(t, strings.Replace(string(src), ` | integrity = "crc32c"`, "", 1))
	values := make([]*tabletext.Instance, len(cases))
	bare := make([]*tabletext.Instance, len(cases))
	for i, input := range cases {
		values[i] = place(t, model, "Root", string(input))
		bare[i] = place(t, plain, "Root", string(input))
	}
	body, err := tablewire.EncodeMessages(plain, bare)
	if err != nil {
		t.Fatal(err)
	}
	want := binary.LittleEndian.AppendUint32(bytes.Clone(body), crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
	pinText, err := os.ReadFile(filepath.Join(base, "batch.hex"))
	if err != nil {
		t.Fatal(err)
	}
	pin, err := hex.DecodeString(strings.TrimSpace(string(pinText)))
	if err != nil || !bytes.Equal(pin, want) {
		t.Fatalf("message corpus pin differs: %v got%x want%x", err, want, pin)
	}
	return model, values, want
}

func TestMessageIntegrityBatch(t *testing.T) {
	model, values, want := messageIntegrityFixture(t)
	wire, err := tablewire.EncodeMessages(model, values)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, want) {
		t.Errorf("message CRC missing or wrong: got%x want%x", wire, want)
	}
	var vocab tablewire.Vocabulary
	var announcement tabletext.Report
	if err = vocab.AnnounceRead(tablewire.Announce(model.Unit), &announcement); err != nil {
		t.Fatal(err)
	}
	fresh := func() []*tabletext.Instance {
		out := make([]*tabletext.Instance, len(values))
		for i := range out {
			out[i] = place(t, model, "Root", `{"value":99}`)
		}
		return out
	}
	dst := fresh()
	var report tabletext.Report
	n, ok, err := tablewire.DecodeMessages(model, dst, want, &vocab, &report)
	if err != nil || !ok || n != 3 || report != (tabletext.Report{}) {
		t.Errorf("sealed batch refused: n=%d ok=%t err=%v report=%+v", n, ok, err, report)
	} else {
		for i, v := range dst {
			if v.Fields[0].Cell.I != values[i].Fields[0].Cell.I {
				t.Errorf("body%d changed", i)
			}
		}
	}
	if spots := tablewire.MessageSpots(model, model.Lookup("Root"), want, &vocab); len(spots) == 0 {
		t.Error("valid sealed message has no reference spots")
	}
	for pos := 1; pos < len(want); pos++ {
		t.Run(fmt.Sprintf("damage-byte-%d", pos), func(t *testing.T) {
			bad := bytes.Clone(want)
			bad[pos] ^= 1
			dst := fresh()
			var rep tabletext.Report
			n, ok, err := tablewire.DecodeMessages(model, dst, bad, &vocab, &rep)
			if n != 0 || ok || tablewire.RefusalReason(err) != tablewire.ReasonIntegrityMismatch || !rep.Refused {
				t.Fatalf("CRC accepted or decoded: n=%d ok=%t err=%v report=%+v", n, ok, err, rep)
			}
			for _, v := range dst {
				if v.Fields[0].Cell.I != 0 {
					t.Fatal("damaged batch retained a value")
				}
			}
			if spots := tablewire.MessageSpots(model, model.Lookup("Root"), bad, &vocab); len(spots) != 0 {
				t.Fatal("scanner decoded before checking integrity")
			}
		})
	}
	for cut := 1; cut < 6; cut++ {
		var rep tabletext.Report
		n, ok, err := tablewire.DecodeMessages(model, fresh(), want[:cut], &vocab, &rep)
		if n != 0 || ok || tablewire.RefusalReason(err) != tablewire.ReasonIntegrityMismatch || rep.Reason != "integrity_mismatch" {
			t.Errorf("short%d: n=%d ok=%t err=%v report=%+v", cut, n, ok, err, rep)
		}
	}
	var wrong tabletext.Report
	_, ok, err = tablewire.DecodeMessages(model, fresh(), []byte{1}, &vocab, &wrong)
	if ok || err == nil || !strings.Contains(err.Error(), "newer_form") || !wrong.Refused || wrong.Reason != "newer_form" {
		t.Errorf("form must precede CRC: ok=%t err=%v report=%+v", ok, err, wrong)
	}
}

func TestMessageIntegrityIndependentJavaScript(t *testing.T) {
	slowtest.Gate(t, "node (independent message integrity writer)")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	model, _, want := messageIntegrityFixture(t)
	files, err := compiler.New().Generate(model.Unit, "js", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	entry := ""
	storage := ""
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.js") && strings.Contains(string(data), "export class Root {") && strings.Contains(string(data), "export function InitRoot(") {
			storage = name
		}
		if strings.HasSuffix(name, "Table.js") && strings.Contains(string(data), "export function RootSaveMessages(") {
			entry = name
		}
	}
	if entry == "" || storage == "" {
		t.Fatal("generated RootSaveMessages export missing")
	}
	script := fmt.Sprintf(`import * as m from %q;
import * as s from %q;
const rows=[0,7,-9].map(value=>{const x=new s.Root();s.InitRoot(x);x.value=value;return x});
const bytes=new Uint8Array(m.RootMeasureMessages(rows,{}));
if(m.RootSaveMessages(rows,bytes,{})!==bytes.length)throw new Error("writer length");
console.log(Buffer.from(bytes).toString("hex"));
`, "./"+entry, "./"+storage)
	if err = os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "probe.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native JS: %v\n%s", err, out)
	}
	got, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("independent JS message: got%x want%x", got, want)
	}
	t.Logf("independent message pin %x", got)
}

// A reused destination must not preserve a value omitted by a later body.
func TestMessageDecodeResetsDefaults(t *testing.T) {
	model := listModel(t, "package probe\nfixed table Root { value int32 = 7 }\n")
	value := place(t, model, "Root", `{}`)
	wire, err := tablewire.EncodeMessage(model, value)
	if err != nil {
		t.Fatal(err)
	}
	var vocab tablewire.Vocabulary
	if err = vocab.AnnounceRead(tablewire.Announce(model.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	dst := place(t, model, "Root", `{"value":99}`)
	var report tabletext.Report
	ok, err := tablewire.DecodeMessage(model, dst, wire, &vocab, &report)
	if !ok || err != nil || report != (tabletext.Report{}) || dst.Fields[0].Cell.I != 7 {
		t.Fatalf("reused destination: ok=%t err=%v report=%+v value=%d", ok, err, report, dst.Fields[0].Cell.I)
	}
}
