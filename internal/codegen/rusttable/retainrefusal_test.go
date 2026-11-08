package rusttable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRetainedUnknownBeforeNonFiniteRefusalRust(t *testing.T) {
	slowtest.Gate(t, "native Rust retained-state regression")
	writer := unitFrom(t, "package late\nfixed table Root {\n extra int32\n value float32\n}\n")
	reader := unitFrom(t, "package late\nfixed table Root {\n value float32\n}\n")
	m := tabletext.NewModel(writer)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(`{"extra":77,"value":1.25}`), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0, 0, 160, 63}
	at := bytes.Index(wire, needle)
	extra := bytes.Index(wire, []byte{77, 0, 0, 0})
	if at < 0 || extra < 0 || extra >= at {
		t.Fatalf("fixture requires unknown before float: %x", wire)
	}
	binary.LittleEndian.PutUint32(wire[at:at+4], 0x7fc00000)
	body := fmt.Sprintf(`
    let wire: [u8; %d] = [%s];
    let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::new() };
    let mut report = TableReport::default();
    assert!(root_load_retain(&wire, &mut store, &mut report).is_none());
    assert!(report.refused && !report.non_finite_float.is_empty());
    assert!(store.bytes.is_empty() && store.ids.is_empty(), "refused load kept {} retained bytes and {} ids", store.bytes.len(), store.ids.len());
    println!("RETAIN RESET ok");
`, len(wire), rustBytes(wire))
	out, err := runRustDiscardProbe(t, reader, body)
	if err != nil || !strings.Contains(out, "RETAIN RESET ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
