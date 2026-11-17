package rusttable

// TestRustMessageStringDefaultElidedOnlyWhenEqual holds the Rust message
// writer's text elision to the reference (docs/SPEC-TABLES.md §3.3): a string
// or bytes field is left out only when its length AND its bytes are the
// declared default's. A value of the default's length ("xyz" under "abc") and
// the empty value both ride and round-trip unchanged; the default itself is
// still elided, so its body is the empty body's size and its bytes never reach
// the wire. Every saved body is the engine's EncodeMessages of the same value.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRustMessageStringDefaultElidedOnlyWhenEqual(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)
	u := unitFrom(t, `package messagestringdefault

fixed table Probe
{
    s string(8) = "abc"
    b bytes(4) = "ab"
}
`)
	m := tabletext.NewModel(u)
	cases := []struct {
		name string
		s, b string
		text string
	}{
		{"xyz_zz", "xyz", "zz", `{"s":"xyz","b":"eno="}`},
		{"empty_empty", "", "", `{"s":"","b":""}`},
		{"default_default", "abc", "ab", `{"s":"abc","b":"YWI="}`},
	}
	announce := tablewire.Announce(u)
	var body strings.Builder
	for _, c := range cases {
		inst := m.New(m.Lookup("Probe"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(c.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the engine did not read the probe text cleanly: %+v", c.name, rep)
		}
		wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("%s: the engine did not encode: %v", c.name, err)
		}
		fmt.Fprintf(&body, `
    {
        let mut values = vec![ProbeRow::default(); 1];
        values[0].s[..%d].copy_from_slice(b%q);
        values[0].s_length = %d;
        values[0].b[..%d].copy_from_slice(b%q);
        values[0].b_length = %d;
        let mut mrep = TableReport::default();
        let measured = probe_measure_messages(&values, &mut mrep);
        let mut buf = vec![0xa5u8; 256];
        let mut srep = TableReport::default();
        let wrote = probe_save_messages(&values, &mut buf, &mut srep);
        const WANT: [u8; %d] = [%s];
        const ANN: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut back = vec![ProbeRow::default(); 1];
        let mut lrep = TableReport::default();
        let (count, ok) = probe_load_messages(&mut back, &vocab, &buf[..wrote.max(0) as usize], &mut lrep);
        let round = announced && ok && count == 1 && !lrep.damaged && !lrep.refused
            && back[0].s_length == %d && &back[0].s[..back[0].s_length as usize] == b%q
            && back[0].b_length == %d && &back[0].b[..back[0].b_length as usize] == b%q;
        say(%q, measured == WANT.len() as i64 && wrote == WANT.len() as i64 && buf[..WANT.len()] == WANT && round,
            format!("measured={measured} wrote={wrote} want={} got={:02x?} want={:02x?} announced={announced} ok={ok} count={count} report={lrep:?}", WANT.len(), &buf[..WANT.len()], WANT));
    }
`, len(c.s), c.s, len(c.s), len(c.b), c.b, len(c.b),
			len(wire), rustBytes(wire), len(announce), rustBytes(announce),
			len(c.s), c.s, len(c.b), c.b, c.name)
	}
	out := runMessageBatchProbe(t, u, body.String())
	for _, c := range cases {
		if line := messageBatchLine(out, c.name); line != "BATCH "+c.name+" ok" {
			t.Errorf("%s: the Rust message writer does not match the engine: %s", c.name, line)
		}
	}

	// The default's body is the empty body's: its size proves the elision, not
	// a search for its bytes in a larger batch.
	defaultInst := m.New(m.Lookup("Probe"))
	var defaultRep tabletext.Report
	if !m.Read(defaultInst, []byte(`{}`), &defaultRep) || defaultRep != (tabletext.Report{}) {
		t.Fatalf("the engine did not read the empty probe text cleanly: %+v", defaultRep)
	}
	emptyWire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{defaultInst})
	if err != nil {
		t.Fatalf("the engine did not encode the empty value: %v", err)
	}
	defInst := m.New(m.Lookup("Probe"))
	var defRep tabletext.Report
	if !m.Read(defInst, []byte(cases[2].text), &defRep) || defRep != (tabletext.Report{}) {
		t.Fatalf("the engine did not read the default probe text cleanly: %+v", defRep)
	}
	defWire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{defInst})
	if err != nil {
		t.Fatalf("the engine did not encode the default value: %v", err)
	}
	if !bytes.Equal(defWire, emptyWire) {
		t.Fatalf("the engine does not elide the declared default: default %x, empty %x", defWire, emptyWire)
	}
}
