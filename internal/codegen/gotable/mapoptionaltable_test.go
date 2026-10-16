package gotable

// A map whose VALUE is an optional table, `map[K]?Table`, through both of the
// Go implementations: the compiler's own engine (internal/tabletext reading and
// writing the text, internal/tablewire encoding and decoding the wire), which is
// the oracle every leg's conformance is compared against, and the generated Go
// leg. The r3 read of java-carries-maps-and-union-arms-on-the-table-wire-2
// found the oracle turning `{"2":{"n":1}}` into `{"2":{"n":0}}`: the text
// reader placed the value but never marked the optional present, so the
// encoder elided it, and the text writer wrote the absent value's defaults
// where `null` belongs.
//
// The rule both implementations are held to is docs/SPEC-TABLES.md §2.8 (a
// map's value is anything a table field can hold, `?T` included, carried as
// the `value` field of the generated `{ key, value }` entry) read through §2.3
// (an absent optional is not written; a present one ALWAYS rides, even when
// it is entirely default) and §16.2 (in the text, `null` is the absent
// optional and any other value is present).

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const mapOptionalTableSchema = `package probe
table Child {
 n int32
 s string(16)
 xs []int32
}
table Root {
 ints map[uint32]?Child
 names map[string(8)]?Child
}
`

// mapOptionalTableCases are the instances: the r3 instance itself, a present
// value with every kind of field non-zero beside an absent one in each key
// kind, an absent value alone, and a present value that is entirely default
// (which must ride, §2.3). `want` is the canonical text both writers produce,
// compacted: every field written, defaults included, and `null` for an absent
// value.
var mapOptionalTableCases = []struct {
	name, text, want string
}{
	{"r3", `{"ints":{"2":{"n":1}}}`,
		`{"ints":{"2":{"n":1,"s":"","xs":[]}},"names":{}}`},
	{"both", `{"ints":{"5":null,"2":{"n":7,"s":"hi","xs":[1,2]}},"names":{"b":{"n":-3,"s":"x","xs":[9]},"a":null}}`,
		`{"ints":{"2":{"n":7,"s":"hi","xs":[1,2]},"5":null},"names":{"a":null,"b":{"n":-3,"s":"x","xs":[9]}}}`},
	{"absent", `{"ints":{"4":null}}`,
		`{"ints":{"4":null},"names":{}}`},
	{"present-default", `{"names":{"z":{}}}`,
		`{"ints":{},"names":{"z":{"n":0,"s":"","xs":[]}}}`},
}

// mapOptionalTablePinned are wire prefixes derived by hand from the spec for
// two of the instances, so the test does not only compare the two
// implementations with each other. Each is the form byte, the root body (§3:
// local id, kind, payload; a 0 ends a body) and nothing of the id table that
// follows it.
//
// r3: 01 = the form byte; 01 = local id of `ints`, 0e = kind 14 (array),
// 14 = body length 20, 0d = element kind 13 (table), 01 = one entry; the
// entry: 11 = length 17, 02 08 02000000 = `key` as uint32 2, 03 0d 07 =
// `value` as a table of length 7, 04 04 01000000 = `n` as int32 1, 00 ends
// the value, 00 ends the entry; 00 ends the root. The value RIDES, because it
// is present (§2.3).
//
// absent: the same map holding key 4, body length 10, and an entry of length
// 7 that is `key` alone (02 08 04000000, then 00): the absent optional is not
// written (§2.3), and the entry still is, because the key is the entry (§2.8).
var mapOptionalTablePinned = map[string]string{
	"r3":     "01" + "010e140d01" + "11" + "0208" + "02000000" + "030d07" + "040401000000" + "00" + "00" + "00",
	"absent": "01" + "010e0a0d01" + "07" + "0208" + "04000000" + "00" + "00",
}

func compactJSON(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}

func TestMapOfOptionalTablesKeepsItsValuesGo(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, mapOptionalTableSchema)
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	wires := make([]string, len(mapOptionalTableCases))

	// THE ORACLE: text read, text write, wire encode, wire decode.
	for i, tc := range mapOptionalTableCases {
		inst := m.New(root)
		var report tabletext.Report
		if !m.Read(inst, []byte(tc.text), &report) || !report.Silent() {
			t.Errorf("%s: oracle text read %s: report %+v", tc.name, tc.text, report)
			continue
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Errorf("%s: oracle text write: %v", tc.name, err)
			continue
		}
		if got := compactJSON(t, text); got != tc.want {
			t.Errorf("%s: oracle text read then write lost the value:\n got  %s\n want %s", tc.name, got, tc.want)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Errorf("%s: oracle encode: %v", tc.name, err)
			continue
		}
		wires[i] = hex.EncodeToString(wire)
		if pin, ok := mapOptionalTablePinned[tc.name]; ok && !strings.HasPrefix(wires[i], pin) {
			t.Errorf("%s: oracle wire is not the bytes §2.3/§2.8 fix:\n got  %s\n want %s...", tc.name, wires[i], pin)
		}
		back := m.New(root)
		report = tabletext.Report{}
		if ok, err := tablewire.Decode(m, back, wire, &report); !ok || err != nil || !report.Silent() {
			t.Errorf("%s: oracle decode: %v %+v", tc.name, err, report)
			continue
		}
		text, err = m.Write(back)
		if err != nil {
			t.Errorf("%s: oracle text write after decode: %v", tc.name, err)
			continue
		}
		if got := compactJSON(t, text); got != tc.want {
			t.Errorf("%s: oracle text -> wire -> text lost the value: %s -> %s, want %s", tc.name, tc.text, got, tc.want)
		}
	}
	if t.Failed() {
		return
	}

	// THE GENERATED GO LEG, against the oracle's bytes and text: its JSON read
	// then wire save must write the oracle's bytes, its wire load of the
	// oracle's bytes then JSON write must write the canonical text, and its
	// JSON read of the canonical text must save the same bytes again.
	var cases strings.Builder
	for i, tc := range mapOptionalTableCases {
		fmt.Fprintf(&cases, "{%q,%q,%q,%q},\n", tc.name, tc.text, tc.want, wires[i])
	}
	runGenerated(t, mapOptionalTableSchema, `package probe
import("bytes";"encoding/hex";"encoding/json";"testing";"unsafe")
func compact(t *testing.T,text []byte)string {var out bytes.Buffer;if err:=json.Compact(&out,text);err!=nil{t.Fatalf("not JSON %v %s",err,text)};return out.String()}
func save(t *testing.T,name,text string)string {var b RootBuilder;b.Init();defer b.Shutdown();var report TableReport;if !RootFromJson(&b,[]byte(text),&report)||report!=(TableReport{})||!b.Lock(){t.Fatalf("%s: json read %s %+v",name,text,report)};wire:=make([]byte,RootMeasure(b.AsConst()));if RootSave(b.AsConst(),wire)!=int64(len(wire)){t.Fatalf("%s: save",name)};return hex.EncodeToString(wire)}
func TestMapOptionalTable(t *testing.T){for _,tc:=range []struct{name,text,want,wire string}{
`+cases.String()+`} {
 if got:=save(t,tc.name,tc.text);got!=tc.wire{t.Errorf("%s: json read then save\n got  %s\n want %s",tc.name,got,tc.wire)}
 if got:=save(t,tc.name,tc.want);got!=tc.wire{t.Errorf("%s: canonical json read then save\n got  %s\n want %s",tc.name,got,tc.wire)}
 wire,_:=hex.DecodeString(tc.wire);size:=RootLoadMeasure(wire);if size<0{t.Fatalf("%s: measure",tc.name)};raw:=make([]byte,size+16);off:=(-uintptr(unsafe.Pointer(&raw[0])))&15;var report TableReport;got:=RootLoad(raw[off:off+uintptr(size)],wire,&report);if got==nil||report!=(TableReport{}){t.Fatalf("%s: load %+v",tc.name,report)}
 text:=make([]byte,RootToJsonMeasure(got));if RootToJson(got,text)!=int64(len(text)){t.Fatalf("%s: json write",tc.name)};if c:=compact(t,text);c!=tc.want{t.Errorf("%s: load then json write\n got  %s\n want %s",tc.name,c,tc.want)}
}}
`)
}
