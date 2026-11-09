package squirreltable_test

// LoadMessages IS THE STRICT READ (docs/SPEC-TABLES.md §3.3, §6.5): every
// wire-alone condition, in a field this reader declares or in one it skips,
// and every declaration-dependent condition refuses the WHOLE batch, and no
// body is delivered. Each row below is one condition on a hand-built batch,
// read through the plain <Root>LoadMessages into storage the probe poisons
// first; each row's NEGATIVE CONTROL takes that one check out of the runtime
// and requires the row's verdict to move. The rows the read resets are the Go
// engine's: every row it wrote (the refused one included) returns to its
// declared defaults, and a row it never reached is untouched.
//
// Functional tests, not unit tests: both run the pinned Squirrel interpreter
// (sqrun). The negative control plants its one fault in the generated runtime
// artifact (schema_table_message.nut), not in a stand-in. They take no sockets
// and no network.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const messageStrictSchema = `package strictmsg

enum Color { Red, Green, Blue }

union Pick
{
    count int32
    word string(4)
}

fixed table Root
{
    a   int32
    r   uint8 = 1 | min = 1, max = 10
    aim float32 | min = -1, max = 1, resolution = 0.5
    s   string(4)
    xs  [..3]uint16
    k   [Color]int32
    p   Pick
}
`

// msgBits is a batch under construction, low bit first (§3.3).
type msgBits struct {
	out     []byte
	bits    int
	refBits int
}

func newMsgBits(count, refBits int) *msgBits {
	return &msgBits{out: []byte{ir.TableWireMessageForm, byte(count - 1)}, bits: 16, refBits: refBits}
}
func (b *msgBits) put(value uint64, width int) {
	for i := range width {
		if b.bits/8 == len(b.out) {
			b.out = append(b.out, 0)
		}
		if (value>>i)&1 != 0 {
			b.out[b.bits/8] |= 1 << (b.bits % 8)
		}
		b.bits++
	}
}
func (b *msgBits) ref(slot int) { b.put(uint64(slot), b.refBits) }
func (b *msgBits) align() {
	for b.bits%8 != 0 {
		b.put(0, 1)
	}
}
func (b *msgBits) bytes(data ...byte) {
	for _, x := range data {
		b.put(uint64(x), 8)
	}
}
func (b *msgBits) done() []byte { b.align(); return b.out }

func msgBitsFor(n int64) int {
	w := 0
	for ; n > 0; n >>= 1 {
		w++
	}
	return w
}

// messageStrictVocab is the unit's own vocabulary with the peer's changes and
// additions, and the slot of each entry it names.
type messageStrictVocab struct {
	u       *ir.Unit
	entries []ir.TableVocabularyEntry
}

func (v *messageStrictVocab) field(name string) ir.TableVocabularyEntry {
	for _, f := range v.u.Tables["Root"].Fields {
		if f.Name == name {
			return ir.TableFieldEntry(f)
		}
	}
	panic("no field " + name)
}
func (v *messageStrictVocab) arm(name string) ir.TableVocabularyEntry {
	un := v.u.Unions["Pick"]
	if un == nil {
		un = v.u.TableUnions["Pick"]
	}
	for _, a := range un.Variants {
		if a.Name == name {
			return ir.TableArmEntry(a)
		}
	}
	panic("no arm " + name)
}
func (v *messageStrictVocab) variant(i int) ir.TableVocabularyEntry {
	return ir.TableVocabularyEntry{Id: ir.TableWireId(v.u.Enums["Color"].VariantWireName(i))}
}
func (v *messageStrictVocab) slot(e ir.TableVocabularyEntry) int {
	for i, x := range v.entries {
		if x.Id == e.Id && x.Kind == e.Kind {
			return i + 1
		}
	}
	panic(fmt.Sprintf("entry %#x kind %d is not announced", e.Id, e.Kind))
}
func (v *messageStrictVocab) change(target, to ir.TableVocabularyEntry) {
	for i := range v.entries {
		if v.entries[i].Id == target.Id && v.entries[i].Kind == target.Kind {
			v.entries[i] = to
			return
		}
	}
	panic("no entry to change")
}
func (v *messageStrictVocab) add(e ir.TableVocabularyEntry) { v.entries = append(v.entries, e) }
func (v *messageStrictVocab) refBits() int                  { return ir.TableMessageRefBits(len(v.entries)) }

type messageStrictRow struct {
	name string
	// peer edits the announcement; batch writes the bytes under it
	peer  func(v *messageStrictVocab)
	batch func(v *messageStrictVocab) []byte
	// want is "<count> <reason or accepted> <A of rows 0, 1, 2>" after the
	// probe poisons A to 111, 222 and 333
	want string
	// control is the one edit that takes this row's check out
	control [2]string
}

// the unknown entries a peer adds: a float32, a string and a table
const (
	strictUnknownF32    = 0x5150515051505150
	strictUnknownString = 0x5151515151515151
	strictUnknownTable  = 0x5152515251525152
)

func messageStrictRows() []messageStrictRow {
	// one body that sets a to 5
	bodyA := func(v *messageStrictVocab, b *msgBits, value uint64) {
		b.ref(v.slot(v.field("a")))
		b.put(value, 32)
	}
	return []messageStrictRow{{
		name: "good",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			bodyA(v, b, 5)
			b.ref(v.slot(v.field("r")))
			b.put(3-1, 4)
			b.ref(v.slot(v.field("aim")))
			b.put(3, 3) // -1 + 3 * 0.5
			b.ref(v.slot(v.field("s")))
			b.put(2, 3)
			b.align()
			b.bytes('h', 'i')
			b.ref(v.slot(v.field("xs")))
			b.put(2, 2)
			b.put(1, 16)
			b.put(2, 16)
			k := v.field("k")
			b.ref(v.slot(k))
			b.put(1, msgBitsFor(k.Shape.Max))
			b.ref(v.slot(v.variant(1)))
			b.put(9, 32)
			b.ref(v.slot(v.field("p")))
			b.ref(v.slot(v.arm("count")))
			b.put(7, 32)
			b.ref(0)
			return b.done()
		},
		want: "1 accepted 5 222 333",
	}, {
		name: "stale_row",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(0)
			return b.done()
		},
		want:    "1 accepted 0 222 333",
		control: [2]string{"storage[k]=d.make();r.written=k+1;", "r.written=k+1;"},
	}, {
		name: "kind_mismatch_field",
		peer: func(v *messageStrictVocab) {
			v.change(v.field("a"), ir.TableVocabularyEntry{Id: v.field("a").Id, Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 4}})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(ir.TableVocabularyEntry{Id: v.field("a").Id, Kind: ir.TableKindString}))
			b.put(0, 3)
			b.align()
			b.ref(0)
			return b.done()
		},
		want:    "-1 kind_mismatch 0 222 333",
		control: [2]string{"if(c<0){refuseRead(r,\"kind_mismatch\");return;}\n    if(c>0)r.widened++;\n    if(fd.optional)", "if(c<0)c=0;\n    if(c>0)r.widened++;\n    if(fd.optional)"},
	}, {
		name: "kind_mismatch_arm",
		peer: func(v *messageStrictVocab) {
			num := v.arm("count")
			v.change(num, ir.TableVocabularyEntry{Id: num.Id, Kind: ir.TableKindU16})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("p")))
			b.ref(v.slot(ir.TableVocabularyEntry{Id: v.arm("count").Id, Kind: ir.TableKindU16}))
			b.put(7, 16)
			b.ref(0)
			return b.done()
		},
		want:    "-1 kind_mismatch 0 222 333",
		control: [2]string{"if(c<0){refuseRead(r,\"kind_mismatch\");return;}\n    if(c>0)r.widened++;\n    u.Type", "if(c<0)c=0;\n    if(c>0)r.widened++;\n    u.Type"},
	}, {
		name: "out_of_range_int",
		peer: func(v *messageStrictVocab) {
			v.change(v.field("r"), ir.TableVocabularyEntry{Id: v.field("r").Id, Kind: ir.TableKindU8})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("r")))
			b.put(50, 8)
			b.ref(0)
			return b.done()
		},
		want:    "-1 out_of_range 0 222 333",
		control: [2]string{"} else if (x < fd.lo || x > fd.hi) { refuseRead(r, \"out_of_range\"); return 0; }", "}"},
	}, {
		name: "out_of_range_float",
		peer: func(v *messageStrictVocab) {
			aim := v.field("aim")
			wide := aim
			wide.Shape.QMin, wide.Shape.QMax = -4, 4
			v.change(aim, wide)
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("aim")))
			b.put(16, 5) // -4 + 16 * 0.5 = 4, past the reader's max of 1
			b.ref(0)
			return b.done()
		},
		want:    "-1 out_of_range 0 222 333",
		control: [2]string{"  if (v < fd.fmin || v > fd.fmax) { refuseRead(r, \"out_of_range\"); return 0.0; }\n", ""},
	}, {
		name: "length_over_bound",
		peer: func(v *messageStrictVocab) {
			s := v.field("s")
			wide := s
			wide.Shape.Max = 8
			v.change(s, wide)
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("s")))
			b.put(6, 4)
			b.align()
			b.bytes('a', 'b', 'c', 'd', 'e', 'f')
			b.ref(0)
			return b.done()
		},
		want:    "-1 length_over_bound 0 222 333",
		control: [2]string{"if(n>fd.bound){refuseRead(r,\"length_over_bound\");return;}\n    local s=\"\"", "local s=\"\""},
	}, {
		name: "count_over_bound",
		peer: func(v *messageStrictVocab) {
			xs := v.field("xs")
			wide := xs
			wide.Shape.Max = 7
			v.change(xs, wide)
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("xs")))
			b.put(5, 3)
			for i := range 5 {
				b.put(uint64(i), 16)
			}
			b.ref(0)
			return b.done()
		},
		want:    "-1 count_over_bound 0 222 333",
		control: [2]string{"if(n>fd.bound){refuseRead(r,\"count_over_bound\");return;}", ""},
	}, {
		name: "repeated_field_id",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			bodyA(v, b, 5)
			bodyA(v, b, 6)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"if(reservedId(id)||seen.find(id)!=null){r.bad=true;return;}", "if(reservedId(id)){r.bad=true;return;}"},
	}, {
		name: "repeated_field_id_skipped",
		peer: func(v *messageStrictVocab) {
			v.add(ir.TableVocabularyEntry{Id: strictUnknownTable, Kind: ir.TableKindTable})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(ir.TableVocabularyEntry{Id: strictUnknownTable, Kind: ir.TableKindTable}))
			bodyA(v, b, 5)
			bodyA(v, b, 6)
			b.ref(0)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"if(reservedId(entry.Id)||seen.find(entry.Id)!=null){r.bad=true;return;}", "if(reservedId(entry.Id)){r.bad=true;return;}"},
	}, {
		name: "repeated_keyed_slot",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			k := v.field("k")
			b.ref(v.slot(k))
			b.put(2, msgBitsFor(k.Shape.Max))
			b.ref(v.slot(v.variant(0)))
			b.put(1, 32)
			b.ref(v.slot(v.variant(0)))
			b.put(2, 32)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"||keys.find(ke.Id)!=null){r.bad=true;return;}\n      keys.append(ke.Id);", "){r.bad=true;return;}\n      keys.append(ke.Id);"},
	}, {
		name: "ill_formed_text",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(v.field("s")))
			b.put(2, 3)
			b.align()
			b.bytes(0xc3, 0x28)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"if(!messageTextOk(r.b,at,n)){r.bad=true;return;}", ""},
	}, {
		name: "ill_formed_text_skipped",
		peer: func(v *messageStrictVocab) {
			v.add(ir.TableVocabularyEntry{Id: strictUnknownString, Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 4}})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(ir.TableVocabularyEntry{Id: strictUnknownString, Kind: ir.TableKindString}))
			b.put(2, 3)
			b.align()
			b.bytes(0xc3, 0x28)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"if(!messageTextOk(r.b,r.bits/8,n)){r.bad=true;return;}", ""},
	}, {
		name: "non_finite_skipped",
		peer: func(v *messageStrictVocab) {
			v.add(ir.TableVocabularyEntry{Id: strictUnknownF32, Kind: ir.TableKindF32})
		},
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			b.ref(v.slot(ir.TableVocabularyEntry{Id: strictUnknownF32, Kind: ir.TableKindF32}))
			b.put(0x7fc00000, 32)
			b.ref(0)
			return b.done()
		},
		want:    "-1 non_finite_float 0 222 333",
		control: [2]string{"if(k==10){local raw=get(r,32);if(!r.bad&&(raw&0x7f800000)==0x7f800000)refuseRead(r,\"non_finite_float\");return;}", "if(k==10){local raw=get(r,32);return;}"},
	}, {
		name: "bytes_after_pad",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			bodyA(v, b, 5)
			b.ref(0)
			return append(b.done(), 0)
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"return alignRead(r)&&r.bits==r.b.len()*8;", "return alignRead(r)&&r.bits<=r.b.len()*8;"},
	}, {
		name: "nonzero_pad",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(1, v.refBits())
			bodyA(v, b, 5)
			b.ref(0)
			if b.bits%8 == 0 {
				panic("the body ends on a byte: no pad to spoil")
			}
			out := b.done()
			out[len(out)-1] |= 0x80
			return out
		},
		want:    "0 wire_damaged 0 222 333",
		control: [2]string{"return alignRead(r)&&r.bits==r.b.len()*8;", "return (alignRead(r)||true)&&r.bits==r.b.len()*8;"},
	}, {
		// the refusal in body 1 resets body 0, which the read had written
		name: "second_body_refused",
		batch: func(v *messageStrictVocab) []byte {
			b := newMsgBits(2, v.refBits())
			bodyA(v, b, 5)
			b.ref(0)
			bodyA(v, b, 6)
			bodyA(v, b, 7)
			b.ref(0)
			return b.done()
		},
		want:    "0 wire_damaged 0 0 333",
		control: [2]string{"  for(local i=0;i<r.written;i++)storage[i]=d.make();\n", ""},
	}}
}

// runMessageStrict reads every row through <Root>LoadMessages under one
// runtime, edited by control when it is set, and answers each row's line.
func runMessageStrict(t *testing.T, rows []messageStrictRow, control [2]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	u := loadInlineUnit(t, filepath.Join(dir, "strictmsg.schema"), messageStrictSchema)
	files := squirrelMessageFiles(t, u, nil)
	if control[0] != "" {
		const name = "schema_table_message.nut"
		body := string(files[name])
		if n := strings.Count(body, control[0]); n != 1 {
			t.Fatalf("control anchor %q occurs %d times, want once", control[0], n)
		}
		files[name] = []byte(strings.Replace(body, control[0], control[1], 1))
	}
	for name, b := range files {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	inputs := map[string][]byte{}
	var script strings.Builder
	fmt.Fprintf(&script, "local I=dofile(\"inputs.nut\",true);local T=dofile(\"schema_module.nut\",true).require(%q);\n", squirreltable.ModuleName(u))
	for _, row := range rows {
		v := &messageStrictVocab{u: u, entries: ir.TableVocabulary(u)}
		if row.peer != nil {
			row.peer(v)
		}
		inputs[row.name+"_ann"] = announcementWithEntries(u, v.entries)
		inputs[row.name+"_batch"] = row.batch(v)
		fmt.Fprintf(&script, `{ local v={};local ar={};if(!T.AnnounceRead(v,I[%q],ar))print(%q+": announce "+ar.reason+"\n");else{local out=[T.Root(),T.Root(),T.Root()];out[0].A=111;out[1].A=222;out[2].A=333;local rr={};local n=T.RootLoadMessages(out,v,I[%q],rr);print(%q+": "+n+" "+(rr.refused?rr.reason:"accepted")+" "+out[0].A+" "+out[1].A+" "+out[2].A+"\n");if(%q=="good")print("good values: "+out[0].R+" "+out[0].Aim+" "+out[0].S+" "+out[0].Xs.len()+":"+out[0].Xs[0]+","+out[0].Xs[1]+" "+out[0].K[1]+" "+out[0].P.Type+":"+out[0].P.Count+"\n");} }
`, row.name+"_ann", row.name, row.name+"_batch", row.name, row.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(string(sqrun.Run(t, dir, "main.nut", inputs)), "\n") {
		if name, rest, ok := strings.Cut(line, ": "); ok {
			got[name] = rest
		}
	}
	return got
}

func TestSquirrelLoadMessagesIsTheStrictRead(t *testing.T) {
	t.Parallel()
	rows := messageStrictRows()
	got := runMessageStrict(t, rows, [2]string{})
	for _, row := range rows {
		if got[row.name] != row.want {
			t.Errorf("%s: got %q, want %q", row.name, got[row.name], row.want)
		}
	}
	if want := "3 0.5 hi 2:1,2 9 1:7"; got["good values"] != want {
		t.Errorf("the accepted batch read %q, want %q", got["good values"], want)
	}
}

func TestSquirrelLoadMessagesStrictNegativeControl(t *testing.T) {
	t.Parallel()
	for _, row := range messageStrictRows() {
		if row.control[0] == "" {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got := runMessageStrict(t, []messageStrictRow{row}, row.control)
			if got[row.name] == row.want {
				t.Errorf("%s: the check taken out, the row still reads %q", row.name, got[row.name])
			}
		})
	}
}
