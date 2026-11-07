package rusttable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRustIntegrityNative(t *testing.T) {
	slowtest.Gate(t, "rustc (declared CRC across file, fixed and message forms)")
	keyedRequireRustc(t)
	fixture := "../../../testdata/conformance/feature-witnesses/integrity-forms"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	model := tabletext.NewModel(u)
	var body strings.Builder
	body.WriteString(`assert_eq!(table_integrity_checksum(b"123456789"),0xe3069283);` + "\n")
	for _, input := range []string{`{}`, `{"n":7,"child":{"x":9}}`} {
		value := model.New(u.Tables["Root"])
		if !model.Read(value, []byte(input), &tabletext.Report{}) {
			t.Fatal("fixture JSON")
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, `{
let wire=vec!%s;
let value=root_load(&wire,&mut TableReport::default()).expect("sealed variable load");
assert_eq!(root_save(&value),wire);assert_eq!(root_save_checked(&value).unwrap(),wire);
assert!(root_load_measure(&wire)>0);
let text=root_to_json(&value);let back=root_from_json(&text,&mut TableReport::default()).unwrap();assert_eq!(root_save(&back),wire);
let mut store=TableRetain { bytes:Vec::with_capacity(4096), ids:Vec::with_capacity(64) };let mut report=TableReport::default();
let back=root_load_retain(&wire,&mut store,&mut report).unwrap();assert_eq!(root_save_retain(&back,&mut store,&mut report),wire);
for at in 1..wire.len(){let mut bad=wire.clone();bad[at]^=1;let mut r=TableReport::default();assert!(root_load(&bad,&mut r).is_none());assert!(r.damaged);assert_eq!(r.reason.name(),"integrity_mismatch");assert_eq!((r.unknown,r.mismatch),(0,0));assert_eq!(root_load_measure(&bad),-1);}
assert!(plain_root_load(&wire,&mut TableReport::default()).is_none());
assert!(root_load(&wire[..wire.len()-4],&mut TableReport::default()).is_none());
}
`, rustByteVector(wire))
	}

	// A tagged child does not seal an untagged parent.
	plain := model.New(u.Tables["PlainRoot"])
	if !model.Read(plain, []byte(`{"n":7,"child":{"x":9}}`), &tabletext.Report{}) {
		t.Fatal("plain parent fixture")
	}
	plainWire, err := tablewire.Encode(model, plain)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&body, `{let wire=vec!%s;let value=plain_root_load(&wire,&mut TableReport::default()).unwrap();assert_eq!(plain_root_save(&value),wire);assert_eq!(plain_root_save_checked(&value).unwrap(),wire);}`+"\n", rustByteVector(plainWire))
	var values []*tabletext.Instance
	for i, label := range []string{"ok", "", "eight ch"} {
		v := model.New(u.Tables["Reading"])
		if !model.Read(v, []byte(fmt.Sprintf(`{"x":%d,"y":%d,"label":%q}`, 1000+i, -7*i, label)), &tabletext.Report{}) {
			t.Fatal("reading JSON")
		}
		values = append(values, v)
	}
	message, err := tablewire.EncodeMessages(model, values)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := os.ReadFile(filepath.Join(fixture, "reading.fixed.hex"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := hex.DecodeString(strings.TrimSpace(string(pin)))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&body, `{
let fixed=vec!%s;let mut rows=vec![ReadingRow::default();3];let mut plan=vec![TableFixedEntry::default();256];let mut remap=vec![0u16;256];let mut report=TableFixedReport::default();
assert_eq!(reading_fixed_load(&mut rows,&fixed,&mut plan,&mut remap,&mut report),Some(3));
assert_eq!(reading_fixed_measure(3),fixed.len());let mut saved=vec![0u8;fixed.len()];assert_eq!(reading_fixed_save(&rows,&mut saved),Some(fixed.len()));assert_eq!(saved,fixed);let mut filled=vec![0xa5u8;fixed.len()+17];assert_eq!(reading_fixed_save(&rows,&mut filled),Some(fixed.len()));assert_eq!(&filled[..fixed.len()],fixed.as_slice());assert!(filled[fixed.len()..].iter().all(|&x|x==0xa5));
assert_eq!(reading_fixed_save(&rows,&mut saved[..fixed.len()-1]),None);
for at in 1..fixed.len(){let mut bad=fixed.clone();bad[at]^=1;let mut r=TableFixedReport::default();assert!(reading_fixed_load(&mut rows,&bad,&mut plan,&mut remap,&mut r).is_none());assert_eq!(r.reason.name(),"integrity_mismatch");}
let mut storage=vec![TableMessageEntry::default();64];let mut vocab=TableVocabulary::new(&mut storage);assert!(announce_read(&mut vocab,&TABLE_ANNOUNCEMENT,&mut TableReport::default()));
let message=vec!%s;let mut r=TableReport::default();assert_eq!(reading_load_messages(&mut rows,&vocab,&message,&mut r),(3,true));
assert_eq!(reading_measure_messages(&rows,&mut TableReport::default()),message.len() as i64);let mut saved=vec![0u8;message.len()];assert_eq!(reading_save_messages(&rows,&mut saved,&mut TableReport::default()),message.len() as i64);assert_eq!(saved,message);let mut filled=vec![0xa5u8;message.len()+17];assert_eq!(reading_save_messages(&rows,&mut filled,&mut TableReport::default()),message.len() as i64);assert_eq!(&filled[..message.len()],message.as_slice());assert!(filled[message.len()..].iter().all(|&x|x==0xa5));
assert_eq!(reading_save_messages(&rows,&mut saved[..message.len()-1],&mut TableReport::default()),-1);
for at in 1..message.len(){let mut bad=message.clone();bad[at]^=1;let mut r=TableReport::default();assert_eq!(reading_load_messages(&mut rows,&vocab,&bad,&mut r),(0,false));assert_eq!(r.reason.name(),"integrity_mismatch");assert_eq!((r.unknown,r.mismatch),(0,0));}
for cut in 1..6 { let mut r=TableReport::default();assert_eq!(reading_load_messages(&mut rows,&vocab,&message[..cut],&mut r),(0,false));assert_eq!(r.reason.name(),"integrity_mismatch"); }
let mut r=TableReport::default();assert_eq!(reading_load_messages(&mut rows,&vocab,&[1],&mut r),(0,false));assert_eq!(r.reason.name(),"newer_form");
}
println!("integrity native ok");
`, rustByteVector(fixed), rustByteVector(message))

	plainPin, err := os.ReadFile(filepath.Join(fixture, "plain-reading.fixed.hex"))
	if err != nil {
		t.Fatal(err)
	}
	plainFixed, err := hex.DecodeString(strings.TrimSpace(string(plainPin)))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&body, `{let wire=vec!%s;let mut rows=vec![PlainReadingRow::default();3];let mut plan=vec![TableFixedEntry::default();256];let mut remap=vec![0u16;256];assert_eq!(plain_reading_fixed_load(&mut rows,&wire,&mut plan,&mut remap,&mut TableFixedReport::default()),Some(3));assert_eq!(plain_reading_fixed_measure(3),wire.len());let mut out=vec![0u8;wire.len()];assert_eq!(plain_reading_fixed_save(&rows,&mut out),Some(out.len()));assert_eq!(out,wire);let plain_message=vec![rows[0].clone(),rows[1].clone(),rows[2].clone()];let size=plain_reading_measure_messages(&plain_message,&mut TableReport::default()) as usize;let mut zero=vec![0u8;size];let mut filled=vec![0xa5u8;size+17];assert_eq!(plain_reading_save_messages(&plain_message,&mut zero,&mut TableReport::default()),size as i64);assert_eq!(plain_reading_save_messages(&plain_message,&mut filled,&mut TableReport::default()),size as i64);assert_eq!(&filled[..size],zero.as_slice());assert!(filled[size..].iter().all(|&x|x==0xa5));}`+"\n", rustByteVector(plainFixed))
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil || !strings.Contains(out, "integrity native ok") {
		t.Fatalf("integrity native: %v\n%s", err, out)
	}
}
