package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonMessageUnionKeyedArmShape(t *testing.T) {
	t.Parallel()
	illegal := `package msgarmshape
enum Slot { head, body }
union Choice { items [Slot]int32 }
fixed table Root { choice Choice }
`
	ast, perrs := parser.Parse("Core.schema", []byte(illegal))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs)
	}
	_, errs := check.Unit([]check.SourceFile{{Path: "Core.schema", Name: "Core.schema", Base: "Core", Bytes: []byte(illegal), AST: ast}})
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "an enum-keyed array is not an arm") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing keyed-arm refusal: %v", errs)
	}
	schema := func(variants string) string {
		return `package msgarmshape
 enum Slot { ` + variants + ` }
 fixed table Child { items [Slot]int32 }
 union Choice { nested Child }
 fixed table Root { choice Choice }
 `
	}
	writer := loadUnit(t, schema("head, body, extra"))
	reader := loadUnit(t, schema("head, body"))
	wm := tabletext.NewModel(writer)
	instance := wm.New(wm.Lookup("Root"))
	var rep tabletext.Report
	if !wm.Read(instance, []byte(`{"choice":{"nested":{"items":{"head":7,"body":9}}}}`), &rep) || rep.Refused {
		t.Fatalf("fixture: %+v", rep)
	}
	wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{instance})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the writer's real differing keyed entry shape while simulating the
	// historical build collision; a reader must not infer the writer's key order.
	realAnnouncement := tablewire.Announce(writer)
	announcement := append([]byte(nil), realAnnouncement...)
	copy(announcement[3:11], tablewire.Announce(reader)[3:11])
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from msgarmshape import Root, Child, MessageVocabulary, ANNOUNCEMENT, announce_read, load_messages, Refused
from msgarmshape._schema._message_unit import _local, FIELD_KEYS
from msgarmshape._schema.message import BitWriter
with open('peer','rb') as f: peer=f.read()
with open('real-peer','rb') as f: real_peer=f.read()
with open('wire','rb') as f: wire=f.read()
for announcement in (real_peer,peer):
 v=MessageVocabulary();announce_read(v,announcement)
 value=load_messages(Root,v,wire)[0]
 assert value.choice.nested.items[1]==7 and value.choice.nested.items[2]==9
# Same-build identity alone cannot establish the keyed writer's order/default
# when its actual announced keyed shape differs from this reader's shape.
v=MessageVocabulary();announce_read(v,peer)
r=v.ref_bits; field=v.slots[FIELD_KEYS[('Root','choice')]]; arm=v.slots[FIELD_KEYS[('Choice','nested')]]
items=next(i+1 for i,e in enumerate(v.entries) if e.field_id==Child.descriptor.fields[0].field_id and e.kind==16)
shape=v.entries[items-1].shape; keys=Child.descriptor.fields[0].keys
head,body=(v.names[k] for k in keys)
def foreign_wire(slots):
 w=BitWriter();w.put(2,8);w.put(0,8);w.put(field,r);w.put(arm,r);w.put(items,r)
 w.put(len(slots),shape.maximum.bit_length())
 for key,n in slots:w.put(key,r);w.put(n,32)
 w.put(0,r);w.put(0,r);return w.finish()
for name,slots in (('order',[(body,9),(head,7)]),('default',[(head,0)])):
 try:load_messages(Root,v,foreign_wire(slots))
 except Refused as e:raise AssertionError('foreign_shape_'+name) from e
own=MessageVocabulary();announce_read(own,ANNOUNCEMENT)
r=_local.ref_bits; field=_local.slots[FIELD_KEYS[('Root','choice')]]
arm=_local.slots[FIELD_KEYS[('Choice','nested')]]; items=_local.slots[FIELD_KEYS[('Child','items')]]
shape=_local.entries[items-1].shape; keys=Child.descriptor.fields[0].keys
head,body=(_local.names[k] for k in keys)
def forged(slots):
 w=BitWriter();w.put(2,8);w.put(0,8);w.put(field,r);w.put(arm,r);w.put(items,r)
 w.put(len(slots),shape.maximum.bit_length())
 for key,n in slots:w.put(key,r);w.put(n,32)
 w.put(0,r);w.put(0,r)
 return w.finish()
for name,slots in (('order',[(body,9),(head,7)]),('default',[(head,0)])):
 try:load_messages(Root,own,forged(slots))
 except Refused as e:assert e.reason=='wire_damaged'
 else:raise AssertionError('arm_keyed_'+name)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "peer": announcement, "real-peer": realAnnouncement, "wire": wire}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("arm-keyed: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgarmshape"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	// Independently remove each actual keyed check; each named probe must turn red.
	for _, fault := range []struct{ old, replacement, expect string }{
		{`if same_shape and index <= last_index:`, `if False:`, `arm_keyed_order`},
		{`same_shape = vocab.build_version == own_version and own_key == entry.key`, `same_shape = vocab.build_version == own_version`, `foreign_shape_order`},
		{`if same_shape and stats[2] == substituted and fd.elem.is_default(item):`, `if False:`, `arm_keyed_default`},
	} {
		mutated := make(map[string][]byte, len(files))
		found := false
		for name, content := range files {
			mutated[name] = content
			if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(content), fault.old) {
				src := strings.Replace(string(content), fault.old, fault.replacement, 1)
				if fault.expect == "arm_keyed_default" {
					src = strings.Replace(src, `if same_shape and stats[2] == substituted and field.is_default(value) and not getattr(field, "required", False):`, "if False:", 1)
				}
				mutated[name] = []byte(src)
				found = true
			}
		}
		if !found {
			t.Fatalf("control anchor missing %s", fault.old)
		}
		broken := t.TempDir()
		writeTree(t, broken, mutated)
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, err := pyrun.Try(t, pin, broken, "main.py", inputs)
			if err == nil || !strings.Contains(out+stderr, fault.expect) {
				t.Fatalf("control %s: %v\n%s\n%s", fault.expect, err, out, stderr)
			}
		}
	}
}
