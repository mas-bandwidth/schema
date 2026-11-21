package rusttable

// The Rust leg's share of the 2026-11-21 review's cross-leg rules
// (review-owed/cross-leg):
//
//   - float32-range: a float32 field's declared range is compared at float32
//     (docs/SPEC.md §4.6), so float32(max) is in range on the variable read
//     and the JSON read.
//   - negative-zero: a compressed float's -0 lands as +0 on the variable,
//     JSON and message reads, and the variable, fixed and JSON writers store
//     +0 (docs/rulings.json fixed-compressed-negzero).
//   - counted-array-min: a fresh row is born with a [Min..N]T array at Min
//     and that birth elides on the message form; a count below Min is
//     refused on the fixed and message reads (docs/SPEC.md §4.2, §5.1 row 3).
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. No clock, no
// socket, no network.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const crossFloatSrc = `package crossfloat

table R
{
    x float32 = 0.5 | min = -1, max = 1, resolution = 0.001
    y float32 | min = -1, max = 0.1, resolution = 0.001
}
`

// crossFloatPlainSrc is the same record with plain floats, which carry any
// bit pattern a compressed reader must hold to its own rules.
const crossFloatPlainSrc = `package crossfloat

table R
{
    x float32 = 0.5
    y float32
}
`

func TestRustVariableFloat32RangeAndNegZero(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	u := unitFrom(t, crossFloatSrc)
	m := tabletext.NewModel(u)
	plain := unitFrom(t, crossFloatPlainSrc)
	pm := tabletext.NewModel(plain)
	encode := func(model *tabletext.Model, text string) []byte {
		t.Helper()
		v := model.New(model.Lookup("R"))
		var rep tabletext.Report
		if !model.Read(v, []byte(text), &rep) {
			t.Fatalf("the engine did not read %s: %+v", text, rep)
		}
		wire, err := tablewire.Encode(model, v)
		if err != nil {
			t.Fatalf("the engine did not encode %s: %v", text, err)
		}
		return wire
	}
	negZero := encode(pm, `{"x":-0.0}`)
	atMax := encode(pm, `{"y":0.1}`)
	zero := encode(m, `{"x":0}`)
	body := fmt.Sprintf(`
    let max = f32::from_bits(0x%x);
    {
        // float32(0.1) is the declared max at float32: in range, landed as is
        let mut r = TableReport::default();
        let v = r_load(&vec!%s, &mut r);
        println!("CROSS var_f32_max {}", v.map(|v| v.y.to_bits() == max.to_bits()).unwrap_or(false) && !r.damaged);
        let mut r = TableReport::default();
        let v = r_from_json("{\"y\":0.1}", &mut r);
        println!("CROSS json_f32_max {}", v.map(|v| v.y.to_bits() == max.to_bits()).unwrap_or(false) && !r.damaged);
    }
    {
        // a compressed -0 lands as +0, uncounted
        let mut r = TableReport::default();
        let v = r_load(&vec!%s, &mut r);
        println!("CROSS var_negzero_read {}", v.map(|v| v.x.to_bits() == 0).unwrap_or(false) && !r.damaged);
        let mut r = TableReport::default();
        let v = r_from_json("{\"x\":-0.0}", &mut r);
        println!("CROSS json_negzero_read {}", v.map(|v| v.x.to_bits() == 0).unwrap_or(false) && !r.damaged);
        // and is stored as +0: the bytes the engine writes for 0
        let v = R { x: -0.0, y: 0.0 };
        println!("CROSS var_negzero_write {}", r_save_checked(&v).map(|b| b == vec!%s).unwrap_or(false));
        println!("CROSS json_negzero_write {}", r_to_json_checked(&v).map(|s| !s.contains("-0")).unwrap_or(false));
    }
`, math.Float32bits(0.1), rustByteVector(atMax), rustByteVector(negZero), rustByteVector(zero))
	out, err := rustConstructProbe(t, u, body)
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, id := range []string{"var_f32_max", "json_f32_max", "var_negzero_read", "json_negzero_read", "var_negzero_write", "json_negzero_write"} {
		if !strings.Contains(out, "CROSS "+id+" true") {
			t.Errorf("%s: the Rust variable form does not hold the rule:\n%s", id, out)
		}
	}
}

const crossCountedSrc = `package crosscount

fixed table Root
{
    ticks uint32
    xs    [2..4]uint8
    z     float32 = 0.5 | min = -1, max = 1, resolution = 0.01
}
`

// crossCountedWriterSrc is the record at another build: no minimum, so it
// can send one element, and a plain float that carries -0.
const crossCountedWriterSrc = `package crosscount

fixed table Root
{
    ticks uint32
    xs    [..4]uint8
    z     float32 = 0.5
}
`

func TestRustFixedAndMessageCountedMinAndNegZero(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)
	u := unitFrom(t, crossCountedSrc)
	m := tabletext.NewModel(u)
	writer := unitFrom(t, crossCountedWriterSrc)
	mw := tabletext.NewModel(writer)
	wvoc := repeatedIdVocabulary(t, writer)

	// A fresh value elides whole: the engine's batch for one fresh body.
	fresh, err := tablewire.EncodeMessages(m, []*tabletext.Instance{m.New(m.Lookup("Root"))})
	if err != nil {
		t.Fatalf("the engine did not encode a fresh body: %v", err)
	}
	short := repeatedIdEncode(t, mw, "Root", `{"ticks":5,"xs":[7]}`)
	negZero := repeatedIdEncode(t, mw, "Root", `{"ticks":5,"xs":[1,2],"z":-0.0}`)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if _, ok, _ := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, short, &wvoc, &rep); ok {
		t.Fatalf("the oracle accepted a count below the reader's minimum")
	}
	ann := tablewire.Announce(writer)
	body := fmt.Sprintf(`
    const FRESH: [u8; %d] = [%s];
    const WRITER: [u8; %d] = [%s];
    const SHORT: [u8; %d] = [%s];
    const NEGZERO: [u8; %d] = [%s];
    {
        // born at Min (the zero form and the declared defaults both), and the
        // birth elides
        let values = vec![root_message_default(); 1];
        let born = RootRow::default().xs_count;
        let mut buf = vec![0u8; 64];
        let wrote = root_save_messages(&values, &mut buf, &mut TableReport::default());
        let mut storage = vec![TableMessageEntry::default(); 64];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut TableReport::default());
        let mut back = vec![RootRow::default(); 1];
        back[0].xs_count = 4;
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut back, &vocab, &buf[..wrote.max(0) as usize], &mut lrep);
        say("born_at_min", born == 2 && values[0].xs_count == 2 && wrote == FRESH.len() as i64 && buf[..FRESH.len()] == FRESH
            && announced && ok && count == 1 && back[0].xs_count == 2,
            format!("born={} wrote={wrote} got={:02x?} want={:02x?} ok={ok} report={lrep:?}", values[0].xs_count, &buf[..wrote.max(0) as usize], FRESH));
    }
    {
        let mut storage = vec![TableMessageEntry::default(); 64];
        let mut vocab = TableVocabulary::new(&mut storage);
        let announced = announce_read(&mut vocab, &WRITER, &mut TableReport::default());
        let mut back = vec![RootRow::default(); 1];
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut back, &vocab, &SHORT, &mut lrep);
        say("message_below_min", announced && !ok && count == 0 && lrep.damaged, format!("ok={ok} count={count} report={lrep:?}"));
        let mut lrep = TableReport::default();
        let (count, ok) = root_load_messages(&mut back, &vocab, &NEGZERO, &mut lrep);
        say("message_negzero", ok && count == 1 && back[0].z.to_bits() == 0, format!("ok={ok} count={count} z={:#x} report={lrep:?}", back[0].z.to_bits()));
    }
    {
        // the fixed form: a fresh row round-trips; a count below Min refuses
        let values = vec![RootRow::default(); 1];
        let mut buf = vec![0u8; root_fixed_measure(1)];
        let n = root_fixed_save(&values, &mut buf).unwrap_or(0);
        let mut plan = vec![TableFixedEntry::default(); 64];
        let mut remap = vec![0u16; 64];
        let mut back = vec![RootRow::default(); 1];
        let mut report = TableFixedReport::default();
        let read = root_fixed_load(&mut back, &buf[..n], &mut plan, &mut remap, &mut report);
        say("fixed_born_round_trip", read == Some(1) && back[0].xs_count == 2 && !report.refused, format!("read={read:?} report={report:?}"));
        let at = n - ROOT_FIXED_RECORD_BYTES + 8 + 4;
        let mut low = buf[..n].to_vec();
        low[at] = 1;
        let mut report = TableFixedReport::default();
        let read = root_fixed_load(&mut back, &low, &mut plan, &mut remap, &mut report);
        say("fixed_below_min", read.is_none() && report.refused && report.reason == TableFixedReason::OutOfRange, format!("read={read:?} report={report:?}"));
        // a compressed -0 is stored as +0
        let mut neg = vec![RootRow::default(); 1];
        neg[0].z = -0.0;
        let mut pos = vec![RootRow::default(); 1];
        pos[0].z = 0.0;
        let mut a = vec![0u8; root_fixed_measure(1)];
        let mut b = vec![0u8; root_fixed_measure(1)];
        let na = root_fixed_save(&neg, &mut a);
        let nb = root_fixed_save(&pos, &mut b);
        say("fixed_negzero_write", na.is_some() && na == nb && a == b, format!("na={na:?} nb={nb:?}"));
    }
`, len(fresh), rustBytes(fresh), len(ann), rustBytes(ann), len(short), rustBytes(short), len(negZero), rustBytes(negZero))
	out := runMessageBatchProbe(t, u, body)
	for _, id := range []string{"born_at_min", "message_below_min", "message_negzero", "fixed_born_round_trip", "fixed_below_min", "fixed_negzero_write"} {
		if line := messageBatchLine(out, id); line != "BATCH "+id+" ok" {
			t.Errorf("%s: %s", id, line)
		}
	}
}
