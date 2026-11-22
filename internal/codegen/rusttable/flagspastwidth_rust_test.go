package rusttable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A TABLE LOAD HOLDS A MASK TO ITS WIDTH (docs/SPEC-TABLES.md §4, R17): the
// newer reader reads old data, never the other way. A Rust reader that meets a
// flags bit at or above its W (the variant count, or K under | max = K)
// refuses the WHOLE load out_of_range in the variable form, the fixed form and
// the message form (a newer writer announces a wider mask, and each value is
// held to the reader's own W). The variable and fixed savers debug-assert
// such a mask, so a same-build round trip never writes what its own reader
// refuses; a release build writes it (the write contract is the caller's).
// The variable file and the message batch are the Go oracle's, written under
// a newer declaration with one more variant. A FUNCTIONAL TEST: it compiles
// and runs Rust, once with debug assertions and once without. Red on the
// emitter that kept the bit.
const flagsWidthReaderSrc = `package probe

flags Caps { Jump, Fly }

table Doc
{
    caps Caps
}

fixed table Rec
{
    caps Caps
}
`

func TestRustFlagsPastWidthRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust reader)")
	keyedRequireRustc(t)
	reader := unitFrom(t, flagsWidthReaderSrc)
	newer := unitFrom(t, strings.Replace(flagsWidthReaderSrc, "Jump, Fly", "Jump, Fly, Swim", 1))
	m := tabletext.NewModel(newer)
	place := func(table, text string) *tabletext.Instance {
		inst := m.New(m.Lookup(table))
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) {
			t.Fatalf("the oracle did not place %s: %+v", text, r)
		}
		return inst
	}
	file := func(text string) []byte {
		wire, err := tablewire.Encode(m, place("Doc", text))
		if err != nil {
			t.Fatalf("the oracle did not encode %s: %v", text, err)
		}
		return wire
	}
	batch := func(text string) []byte {
		wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{place("Rec", text)})
		if err != nil {
			t.Fatalf("the oracle did not encode the batch %s: %v", text, err)
		}
		return wire
	}
	fileIn, filePast := file(`{"caps": ["Fly"]}`), file(`{"caps": ["Jump", "Swim"]}`)
	ann := tablewire.Announce(newer)
	msgIn, msgPast := batch(`{"caps": ["Fly"]}`), batch(`{"caps": ["Swim"]}`)
	files, err := GenerateLineage(reader, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	head := fmt.Sprintf(`
    std::panic::set_hook(Box::new(|_| {}));
    const FILE_IN: [u8; %d] = [%s];
    const FILE_PAST: [u8; %d] = [%s];
    const ANN: [u8; %d] = [%s];
    const MSG_IN: [u8; %d] = [%s];
    const MSG_PAST: [u8; %d] = [%s];
`, len(fileIn), rustBytes(fileIn), len(filePast), rustBytes(filePast), len(ann), rustBytes(ann), len(msgIn), rustBytes(msgIn), len(msgPast), rustBytes(msgPast))
	body := head + `
    let mut rep = TableReport::default();
    println!("file_in {:?}", doc_load(&FILE_IN, &mut rep).map(|d| d.caps));
    let mut rep = TableReport::default();
    println!("file_past {} {}", doc_load(&FILE_PAST, &mut rep).is_some(), rep.damaged);
    let need = rec_fixed_measure(1);
    let mut buf = vec![0u8; need];
    let mut v = RecRow::default();
    v.caps = 2;
    let wrote = rec_fixed_save(&[v], &mut buf).expect("a mask inside W saves");
    let mut out = vec![RecRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut frep = TableFixedReport::default();
    let got = rec_fixed_load(&mut out, &buf[..wrote], &mut plan, &mut remap, &mut frep);
    println!("fixed_in {:?} {}", got, out[0].caps);
    buf[wrote - 8] |= 4;
    let mut frep = TableFixedReport::default();
    let got = rec_fixed_load(&mut out, &buf[..wrote], &mut plan, &mut remap, &mut frep);
    println!("fixed_past {:?} {:?}", got, frep.reason);
    let mut storage = vec![TableMessageEntry::default(); 4096];
    let mut vocab = TableVocabulary::new(&mut storage);
    let mut arep = TableReport::default();
    let _ = announce_read(&mut vocab, &ANN, &mut arep);
    let mut values = vec![RecRow::default(); 1];
    let mut lrep = TableReport::default();
    let (count, ok) = rec_load_messages(&mut values, &vocab, &MSG_IN, &mut lrep);
    println!("message_in {} {} {}", count, ok, values[0].caps);
    let mut lrep = TableReport::default();
    let (count, ok) = rec_load_messages(&mut values, &vocab, &MSG_PAST, &mut lrep);
    println!("message_past {} {} {:?}", count, ok, lrep.reason);
    let mut d = Doc::default();
    d.caps = 6;
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| doc_save(&d))) {
        Ok(_) => println!("file_save wrote"),
        Err(_) => println!("file_save panicked"),
    }
    let mut r = RecRow::default();
    r.caps = 6;
    let mut big = vec![0u8; need];
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| rec_fixed_save(&[r], &mut big).is_some())) {
        Ok(_) => println!("fixed_save wrote"),
        Err(_) => println!("fixed_save panicked"),
    }
`
	common := []string{
		"file_in Some(2)",
		"file_past false true",
		"fixed_in Some(1) 2",
		"fixed_past None OutOfRange",
		"message_in 1 true 2",
		"message_past 0 false OutOfRange",
	}
	for _, mode := range []struct {
		name    string
		asserts bool
		want    []string
	}{
		{"debug-asserts", true, append(append([]string{}, common...), "file_save panicked", "fixed_save panicked")},
		{"no-debug-asserts", false, append(append([]string{}, common...), "file_save wrote", "fixed_save wrote")},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			out, err := runCodecVerdictsProbe(t, files, body, mode.asserts)
			if err != nil {
				t.Fatalf("the probe failed: %v\n%s", err, out)
			}
			got := map[string]string{}
			for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
				k, _, _ := strings.Cut(line, " ")
				got[k] = line
			}
			for _, w := range mode.want {
				k, _, _ := strings.Cut(w, " ")
				if got[k] != w {
					t.Errorf("got %q, want %q", got[k], w)
				}
			}
		})
	}
}
