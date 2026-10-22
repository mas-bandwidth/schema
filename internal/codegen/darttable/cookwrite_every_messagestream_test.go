package darttable

// THE MESSAGE AND STREAM UNION-ARM FAMILIES (docs/SPEC-TABLES.md §7.2, §7.6):
// a union's storage is the tag at the field's offset and the set arm's slot at
// the arms' overlay offset, and `schema cook` writes exactly that storage. The
// families here carry messagedemo's ToolBody arms and streamdemo's Frame arms
// one per subtest, each cooked by this leg's variable runtime writer and
// compared byte for byte with tablecook.Cook in both byte orders.
//
// This file is this card's own: it appends to cookEveryFamilies from init() and
// never edits another card's families (cookwrite_every_test.go owns the type and
// the guard).

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// cookEveryMessageSchema is messagedemo's message set, with Root standing in
// for the corpus root ToolMessage so the shared guard can bind it. Every arm
// of ToolBody is present (§2.6): table bodies, a flags mask, a fixed array, a
// nested union and the payload-free arm.
const cookEveryMessageSchema = `table Cursor {
  line uint32
  column uint32
}
enum Mode { Read, Write }
flags Capabilities { Trace, Verbose }
table OpenDocument {
  path string(64)
  mode Mode
  cursor Cursor
}
table SaveDocument {
  path string(64)
  force bool
}
table Ping { nonce uint32 }
table User { name string(16) }
table Script {
  path string(64)
  line uint32
}
union Origin {
  user User
  script Script
  pid uint32
  note string(24)
}
table Selection {
  start Cursor
  finish Cursor
}
table InsertText {
  pos Cursor
  text string(32)
  origin Origin
}
table RemoveText { span Selection }
union EditBody {
  insert InsertText
  remove RemoveText
  tally int32
  marks [..3]uint16
  blob bytes(4)
  mode Mode
}
table Edit {
  revision uint32
  body EditBody
}
table Transaction {
  reason string(16)
  edits [..3]Edit
}
union ToolBody {
  open OpenDocument
  save SaveDocument
  transact Transaction
  ping Ping
  caps Capabilities
  spans [2]int32
  origin Origin
  ack
}
table Root {
  sequence uint32
  body ToolBody
}`

// cookEveryStreamSchema is streamdemo's frame, with Root standing in for Feed.
// Frame holds a fixed table arm, a VARIABLE table arm, a POINTER arm and a
// scalar arm (§2.6).
const cookEveryStreamSchema = `table Header { name string(16) }
table Chunk {
  data bytes(8)
  next *Chunk
  links [..2]*Chunk
}
union Frame {
  header Header
  chunk Chunk
  link *Chunk
  tag uint32
}
table Root {
  id uint32
  frame Frame
}`

func init() {
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{
			name:   "message-open",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 1, `b.open.path = 'docs/a.md'; b.open.mode = 2; b.open.cursor.line = 3; b.open.cursor.column = 4;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				arm := cookEveryArm(t, m, cookEveryField(t, v, "body"), "open")
				b := arm.Cell.Tab
				cookEveryField(t, b, "path").Cell.Str = []byte("docs/a.md")
				cookEveryField(t, b, "mode").Cell.U = 2
				cursor := cookEveryField(t, b, "cursor").Cell.Tab
				cookEveryField(t, cursor, "line").Cell.U = 3
				cookEveryField(t, cursor, "column").Cell.U = 4
			},
		},
		cookEveryFamily{
			name:   "message-save",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 2, `b.save.path = 'out.bin'; b.save.force = true;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				b := cookEveryArm(t, m, cookEveryField(t, v, "body"), "save").Cell.Tab
				cookEveryField(t, b, "path").Cell.Str = []byte("out.bin")
				cookEveryField(t, b, "force").Cell.B = true
			},
		},
		cookEveryFamily{
			name:   "message-transact",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 3, `b.transact.reason = 'refactor';`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				b := cookEveryArm(t, m, cookEveryField(t, v, "body"), "transact").Cell.Tab
				cookEveryField(t, b, "reason").Cell.Str = []byte("refactor")
			},
		},
		cookEveryFamily{
			name:   "message-ping",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 4, `b.ping.nonce = 7;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				b := cookEveryArm(t, m, cookEveryField(t, v, "body"), "ping").Cell.Tab
				cookEveryField(t, b, "nonce").Cell.U = 7
			},
		},
		cookEveryFamily{
			name:   "message-caps",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 5, `b.caps = 3;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				cookEveryArm(t, m, cookEveryField(t, v, "body"), "caps").Cell.Arm.Cell.U = 3
			},
		},
		cookEveryFamily{
			name:   "message-spans",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 6, `b.spans = [3, 9];`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				arm := cookEveryArm(t, m, cookEveryField(t, v, "body"), "spans").Cell.Arm
				arm.Elems[0].U = 3
				arm.Elems[1].U = 9
			},
		},
		cookEveryFamily{
			name:   "message-origin",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 7, `b.origin.armTag = 3; b.origin.pid = 41;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				origin := cookEveryArm(t, m, cookEveryField(t, v, "body"), "origin").Cell.Arm
				cookEveryArm(t, m, origin, "pid").Cell.Arm.Cell.U = 41
			},
		},
		cookEveryFamily{
			name:   "message-ack",
			schema: cookEveryMessageSchema,
			probe:  cookEveryUnionProbe("body", 8, ``),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				cookEveryArm(t, m, cookEveryField(t, v, "body"), "ack")
			},
		},
		cookEveryFamily{
			name:   "stream-header",
			schema: cookEveryStreamSchema,
			probe:  cookEveryUnionProbe("frame", 1, `b.header.name = 'hdr';`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				b := cookEveryArm(t, m, cookEveryField(t, v, "frame"), "header").Cell.Tab
				cookEveryField(t, b, "name").Cell.Str = []byte("hdr")
			},
		},
		cookEveryFamily{
			name:   "stream-chunk",
			schema: cookEveryStreamSchema,
			probe:  cookEveryUnionProbe("frame", 2, `b.chunk.data = '\x01';`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				b := cookEveryArm(t, m, cookEveryField(t, v, "frame"), "chunk").Cell.Tab
				cookEveryField(t, b, "data").Cell.Str = []byte{1}
			},
		},
		cookEveryFamily{
			name:   "stream-link",
			schema: cookEveryStreamSchema,
			probe:  cookEveryUnionProbe("frame", 3, `b.link = w.Chunk()..data = '\x07';`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				arm := cookEveryArm(t, m, cookEveryField(t, v, "frame"), "link").Cell.Arm
				arm.Cell.Node = m.New(m.Unit.Tables["Chunk"])
				cookEveryField(t, arm.Cell.Node, "data").Cell.Str = []byte{7}
			},
		},
		cookEveryFamily{
			name:   "stream-tag",
			schema: cookEveryStreamSchema,
			probe:  cookEveryUnionProbe("frame", 4, `b.tag = 7;`),
			mutate: func(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {
				cookEveryArm(t, m, cookEveryField(t, v, "frame"), "tag").Cell.Arm.Cell.U = 7
			},
		},
	)
}

// cookEveryUnionProbe is the shared Dart probe for one union arm: set the tag,
// set the arm's slot, then measure before write and cook in both byte orders
// (§7.6). The guard compares the printed bytes with tablecook.Cook's.
func cookEveryUnionProbe(holder string, tag int, setup string) string {
	return fmt.Sprintf(`import 'dart:io';
import 'dart:typed_data';
import 'ProbeWire.dart' as w;
import 'ProbeCook.dart' as c;
String hex(Uint8List b,int length)=>b.take(length).map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void main(){
 final value=w.Root();
 final b=value.%s;
 b.armTag=%d;
 %s
 final size=c.RootCook.cookMeasure(value);
 for(final order in [Endian.little,Endian.big]){
  final bytes=Uint8List(size)..fillRange(0,size,0xa5);
  if(!c.RootCook.cook(value,bytes,order)) throw StateError('cook refused');
  stdout.writeln('${order==Endian.little?'cook-le':'cook-be'} ${hex(bytes,size)}');
 }
}
`, holder, tag, setup)
}

// cookEveryField finds one field of an instance by its declared name.
func cookEveryField(t *testing.T, v *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	if v == nil {
		t.Fatalf("no instance holds %q", name)
	}
	for i := range v.Fields {
		if v.Fields[i].Def.Name == name {
			return &v.Fields[i]
		}
	}
	t.Fatalf("%s has no field %q", v.Def.Name, name)
	return nil
}

// cookEveryArm selects one arm by name and returns the union field holding it:
// a table-body arm's instance is Cell.Tab, an arm line (a scalar, flags, array,
// string, pointer or nested union) is Cell.Arm, and a payload-free arm has
// neither (§2.6, §7.2).
func cookEveryArm(t *testing.T, m *tabletext.Model, holder *tabletext.Field, name string) *tabletext.Field {
	t.Helper()
	un := tabletext.UnionOf(holder.Def)
	if un == nil {
		t.Fatalf("%s is not a union field", holder.Def.Name)
	}
	for i, arm := range un.Variants {
		if arm.Name != name {
			continue
		}
		holder.Cell.U = uint64(i + 1)
		holder.Cell.Arm, holder.Cell.Tab = nil, nil
		if arm.Void() {
			return holder
		}
		if arm.Body() {
			holder.Cell.Tab = m.New(arm.Ref)
			return holder
		}
		holder.Cell.Arm = m.NewArm(arm)
		return holder
	}
	t.Fatalf("union %s has no arm %q", un.Name, name)
	return nil
}
