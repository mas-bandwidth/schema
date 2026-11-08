package rusttable

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Explicit decimal counts and canonical spellings cover the complete int64
// nanosecond domain, offsets, truncation and strict RFC3339 refusal boundaries.
func TestRustTimestampNative(t *testing.T) {
	slowtest.Gate(t, "rustc (strict timestamp text, wire and cook)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/timestamp/rust-boundaries")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input, Value, Canonical string
		Accepted                      bool
		Mismatch                      int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 60 {
		t.Fatalf("timestamp corpus has %d cases, want 60", len(cases))
	}
	u := unitFrom(t, string(schema))
	model := tabletext.NewModel(u)
	work := t.TempDir()
	var body strings.Builder
	body.WriteString(`let fraction = format!("1970-01-01T00:00:00.123456789{}Z", "9".repeat(65536)); assert_eq!(table_timestamp_parse(&fraction), Some(123456789));` + "\n")
	for i, tc := range cases {
		value := model.New(u.Tables["Clock"])
		var report tabletext.Report
		accepted := model.Read(value, []byte(tc.Input), &report)
		want, err := strconv.ParseInt(tc.Value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		// STRICT-ONLY (docs/plans/strict-only.md): only the verdict is pinned,
		// with the Go engine as its oracle; reason names stay per-language.
		if accepted && value.Fields[0].Cell.I != want {
			t.Fatalf("timestamp %s: Go read %d, want %d", tc.Name, value.Fields[0].Cell.I, want)
		}
		fmt.Fprintf(&body, "{ let mut report=TableReport::default(); let value=clock_from_json(%q,&mut report);\n", tc.Input)
		fmt.Fprintf(&body, "assert_eq!((value.is_some(),report.damaged),(%t,%t),\"timestamp case %s\");\n", accepted, !accepted, tc.Name)
		if accepted {
			fmt.Fprintf(&body, "let canonical=%q; for end in 0..canonical.len() { assert!(table_timestamp_parse(&canonical[..end]).is_none()); }\n", tc.Canonical)
			wire, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "let value=value.unwrap(); assert_eq!(value.at,%s_i64);\n", tc.Value)
			fmt.Fprintf(&body, "let wire=vec!%s; assert_eq!(clock_save(&value),wire,\"timestamp wire %s\");\n", rustByteVector(wire), tc.Name)
			fmt.Fprintf(&body, "let mut r=TableReport::default(); let back=clock_load(&wire,&mut r).expect(\"timestamp wire read\"); assert_eq!(back.at,%s_i64); assert!(!r.damaged);\n", tc.Value)
			fmt.Fprintf(&body, "let text=clock_to_json(&value); assert!(text.contains(%q),\"timestamp canonical {}\",text); let back=clock_from_json(&text,&mut TableReport::default()).unwrap(); assert_eq!(back.at,%s_i64);\n", tc.Canonical, tc.Value)
			for _, big := range []bool{false, true} {
				cooked, err := tablecook.Cook(model, value, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				order := 1
				if big {
					order = 2
				}
				path := filepath.Join(work, fmt.Sprintf("%d-%d.cook", i, order))
				if err := os.WriteFile(path, cooked, 0600); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&body, "assert_eq!(clock_cook_write(&value,%d).unwrap(),std::fs::read(%q).unwrap());\n", order, path)
			}
			if want != 0 {
				fmt.Fprintf(&body, "for kind in [5u8,9u8] { let mut damaged=wire.clone(); assert_eq!(damaged[2],36); damaged[2]=kind; let mut r=TableReport::default(); assert!(clock_load(&damaged,&mut r).is_none(),\"a kind change refuses\"); assert!(r.damaged); }\n")
			}
		}
		fmt.Fprintf(&body, "println!(\"case %d ok\"); }\n", i)
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("timestamp native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(cases) {
		t.Fatalf("missing timestamp cases: %s", out)
	}
}

func TestRustTimestampFixedAndMessageNative(t *testing.T) {
	slowtest.Gate(t, "rustc and Go (timestamp fixed and message byte agreement)")
	keyedRequireRustc(t)
	schema, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/timestamp/rust-boundaries/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	work := t.TempDir()
	var goBody, body strings.Builder
	goBody.WriteString("_ = binary.LittleEndian\n")
	announce := filepath.Join(work, "announce.bin")
	fmt.Fprintf(&goBody, "ann:=make([]byte,AnnounceMeasure()); if Announce(ann)!=int64(len(ann)){t.Fatal(\"announce\")}; if err:=os.WriteFile(%q,ann,0600);err!=nil{t.Fatal(err)}\n", announce)
	fmt.Fprintf(&body, "let ann=std::fs::read(%q).unwrap(); assert_eq!(TABLE_ANNOUNCEMENT.as_slice(),ann); let mut storage=vec![TableMessageEntry::default();64]; let mut vocab=TableVocabulary::new(&mut storage); let mut ar=TableReport::default(); assert!(announce_read(&mut vocab,&ann,&mut ar),\"timestamp announcement {:?}\",ar);\n", announce)
	values := []string{"0", "1", "-1", "-9223372036854775808", "9223372036854775807"}
	for i, value := range values {
		fixed := filepath.Join(work, fmt.Sprintf("%d.fixed", i))
		msg := filepath.Join(work, fmt.Sprintf("%d.message", i))
		fmt.Fprintf(&goBody, "{ v:=Tick{At:%s}; fixed:=make([]byte,TickFixedMeasure(1)); if TickFixedSave([]Tick{v},fixed)!=int64(len(fixed)){t.Fatal(\"fixed save\")}; if err:=os.WriteFile(%q,fixed,0600);err!=nil{t.Fatal(err)}; values:=[]*Tick{&v}; msg:=make([]byte,TickMeasureMessages(values,nil)); var r TableReport; if TickSaveMessages(values,msg,&r)!=int64(len(msg)){t.Fatalf(\"message save %%+v\",r)};if err:=os.WriteFile(%q,msg,0600);err!=nil{t.Fatal(err)} }\n", value, fixed, msg)
		fmt.Fprintf(&body, "{ let fixed=std::fs::read(%q).unwrap(); let mut out=vec![TickRow::default();1]; let mut plan=vec![TableFixedEntry::default();64]; let mut remap=vec![0u16;64]; let mut r=TableFixedReport::default(); assert_eq!(tick_fixed_load(&mut out,&fixed,&mut plan,&mut remap,&mut r),Some(1)); assert_eq!(out[0].at,%s_i64); let mut saved=vec![0u8;tick_fixed_measure(1)]; assert_eq!(tick_fixed_save(&out,&mut saved),Some(saved.len())); assert_eq!(saved,fixed);\n", fixed, value)
		fmt.Fprintf(&body, "let msg=std::fs::read(%q).unwrap(); let mut r=TableReport::default(); let (count,ok)=tick_load_messages(&mut out,&vocab,&msg,&mut r); assert!(ok,\"timestamp message {:?}\",r);assert_eq!(count,1);assert_eq!(out[0].at,%s_i64);assert_eq!(r,TableReport::default());let mut saved=vec![0u8;msg.len()];assert_eq!(tick_save_messages(&out,&mut saved,&mut TableReport::default()),msg.len() as i64);assert_eq!(saved,msg);println!(\"fixed %d ok\");}\n", msg, value, i)
	}
	rustRangeFixedOracle(t, u, goBody.String())
	annBytes, err := os.ReadFile(announce)
	if err != nil {
		t.Fatal(err)
	}
	var id [8]byte
	binary.LittleEndian.PutUint64(id[:], ir.TableFieldWireId(u.Tables["Tick"].Fields[0]))
	pos := bytes.Index(annBytes, id[:])
	if pos < 0 || pos+8 >= len(annBytes) || annBytes[pos+8] != 36 {
		t.Fatal("timestamp announcement entry not found")
	}
	fmt.Fprintf(&body, "for kind in [34u8,35u8,37u8] { let mut bad=ann.clone(); bad[%d]=kind; let mut storage=vec![TableMessageEntry::default();64];let mut vocab=TableVocabulary::new(&mut storage);let mut r=TableReport::default();assert!(!announce_read(&mut vocab,&bad,&mut r));assert!(r.damaged); }\n", pos+8)
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("timestamp fixed/message native: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(values) {
		t.Fatalf("missing fixed/message cases: %s", out)
	}
}

func TestRustTimestampUnknownMessageNative(t *testing.T) {
	slowtest.Gate(t, "rustc and Go (unknown timestamp message field)")
	keyedRequireRustc(t)
	schema, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/timestamp/rust-boundaries/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	old := unitFrom(t, strings.ReplaceAll(string(schema), "at timestamp", "old timestamp"))
	work := t.TempDir()
	ann := filepath.Join(work, "unknown.ann")
	msg := filepath.Join(work, "unknown.msg")
	goBody := fmt.Sprintf("_ = binary.LittleEndian; v:=Tick{Old:42}; a:=make([]byte,AnnounceMeasure()); if Announce(a)!=int64(len(a)){t.Fatal(\"announce\")};if err:=os.WriteFile(%q,a,0600);err!=nil{t.Fatal(err)}; values:=[]*Tick{&v}; b:=make([]byte,TickMeasureMessages(values,nil)); if TickSaveMessages(values,b,nil)!=int64(len(b)){t.Fatal(\"message\")};if err:=os.WriteFile(%q,b,0600);err!=nil{t.Fatal(err)}", ann, msg)
	rustRangeFixedOracle(t, old, goBody)
	body := fmt.Sprintf("let ann=std::fs::read(%q).unwrap();let msg=std::fs::read(%q).unwrap();let mut storage=vec![TableMessageEntry::default();64];let mut vocab=TableVocabulary::new(&mut storage);let mut r=TableReport::default();assert!(announce_read(&mut vocab,&ann,&mut r));let mut values=vec![TickRow::default();1];let (count,ok)=tick_load_messages(&mut values,&vocab,&msg,&mut r);assert!(ok,\"unknown timestamp report {:?}\",r);assert_eq!(count,1);assert_eq!(values[0].at,0);assert!(!r.damaged);assert_eq!(r.unknown,1);println!(\"unknown ok\");", ann, msg)
	out, err := rustConstructProbe(t, u, body)
	if err != nil || !strings.Contains(out, "unknown ok") {
		t.Fatalf("unknown timestamp: %v\n%s", err, out)
	}
}
