package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestPythonMessageSubstitutedEnum(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, decl, value string
		unknown           int
		keyed, nested     bool
	}{
		{"deprecated", "Idle, Run | deprecated, Fly", "Run", 0, false, false},
		{"unknown", "Idle, Run", "Fly", 1, false, false},
		{"keyed_deprecated", "Idle, Run | deprecated, Fly", "Run", 0, true, false},
		{"keyed_unknown", "Idle, Run", "Fly", 1, true, false},
		{"nested_deprecated", "Idle, Run | deprecated, Fly", "Run", 0, false, true},
		{"nested_unknown", "Idle, Run", "Fly", 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			field, value := "f Mode", `"`+tc.value+`"`
			check := "value.f==0"
			if tc.keyed {
				field, value, check = "f [Slot]Mode", `{"head":"`+tc.value+`"}`, "value.f[1]==0"
			}
			prefix := "package msgsub\nenum Slot { head, body }\n"
			body := "fixed table Root { tick uint8\n " + field + " }\n"
			if tc.nested {
				body = "fixed table Child { f Mode }\nfixed table Root { tick uint8\n child Child }\n"
				value = `{"f":"` + tc.value + `"}`
				check = "value.child.f==0"
			}
			writer := loadUnit(t, prefix+"enum Mode { Idle, Run, Fly }\n"+body)
			reader := loadUnit(t, prefix+"enum Mode {\n"+strings.ReplaceAll(tc.decl, ", ", "\n")+"\n}\n"+body)
			wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
			inst := wm.New(wm.Lookup("Root"))
			var report tabletext.Report
			jsonField := "f"
			if tc.nested {
				jsonField = "child"
			}
			if !wm.Read(inst, []byte(`{"tick":1,"`+jsonField+`":`+value+`}`), &report) || report.Refused {
				t.Fatalf("engine fixture: %+v", report)
			}
			wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{inst})
			if err != nil {
				t.Fatal(err)
			}
			announcement := append([]byte(nil), tablewire.Announce(writer)...)
			copy(announcement[3:11], tablewire.Announce(reader)[3:11])
			var v tablewire.Vocabulary
			var announceReport tabletext.Report
			if err = v.AnnounceRead(announcement, &announceReport); err != nil {
				t.Fatal(err)
			}
			result := rm.New(rm.Lookup("Root"))
			var readReport tabletext.Report
			_, ok, err := tablewire.DecodeMessages(rm, []*tabletext.Instance{result}, wire, &v, &readReport)
			if !ok || err != nil || readReport.Refused || readReport.Unknown != tc.unknown {
				t.Fatalf("Go oracle: ok=%v err=%v report=%+v", ok, err, readReport)
			}
			files, err := Generate(reader)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + `from msgsub import Root, MessageVocabulary, announce_read, load_messages, LoadReport
with open('announce','rb') as f: announcement=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,announcement);report=LoadReport()
value=load_messages(Root,v,wire,report=report)[0]
assert value.tick==1 and VALUE_CHECK and report.unknown==EXPECT
print('ok')
`
			if !tc.keyed && !tc.nested {
				probe = strings.Replace(probe, "print('ok')", `from msgsub import ANNOUNCEMENT, Refused
from msgsub._schema._message_unit import _local, FIELD_KEYS
from msgsub._schema.message import BitWriter
own=MessageVocabulary();announce_read(own,ANNOUNCEMENT)
slot=_local.slots[FIELD_KEYS[('Root','f')]];w=BitWriter();w.put(2,8);w.put(0,8)
w.put(slot,_local.ref_bits);w.put(0,_local.ref_bits);w.put(0,_local.ref_bits)
try:load_messages(Root,own,w.finish())
except Refused as e:assert e.reason=='wire_damaged'
else:raise AssertionError('explicit_default_control')
print('ok')`, 1)
			}
			probe = strings.ReplaceAll(probe, "VALUE_CHECK", check)
			// A literal expectation makes the two distinct substitution reasons observable.
			if tc.unknown == 0 {
				probe = string(bytes.ReplaceAll([]byte(probe), []byte("EXPECT"), []byte("0")))
			} else {
				probe = string(bytes.ReplaceAll([]byte(probe), []byte("EXPECT"), []byte("1")))
			}
			inputs := map[string][]byte{"main.py": []byte(probe), "announce": announcement, "wire": wire}
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
				if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
					t.Fatalf("Python substitution: %v\n%s\n%s", err, out, stderr)
				}
			}
			if out, err := pyrun.Analyze(t, dir, "msgsub"); err != nil {
				t.Fatalf("mypy: %v\n%s", err, out)
			}
			if !tc.keyed && !tc.nested {
				mutated := make(map[string][]byte, len(files))
				found := false
				for name, content := range files {
					mutated[name] = content
					const guard = `if same_shape and stats[2] == substituted and field.is_default(value) and not getattr(field, "required", False):`
					if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(content), guard) {
						mutated[name] = []byte(strings.Replace(string(content), guard, "if False:", 1))
						found = true
					}
				}
				if !found {
					t.Fatal("default refusal control anchor absent")
				}
				d := t.TempDir()
				writeTree(t, d, mutated)
				for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
					out, stderr, err := pyrun.Try(t, pin, d, "main.py", inputs)
					if err == nil || !strings.Contains(out+stderr, "explicit_default_control") {
						t.Fatalf("default control not detected: %v\n%s\n%s", err, out, stderr)
					}
				}
			}
			broken := make(map[string][]byte, len(files))
			found := false
			for name, content := range files {
				broken[name] = content
				if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(content), "stats[2] += 1") {
					broken[name] = []byte(strings.ReplaceAll(string(content), "stats[2] += 1", "stats[2] += 0"))
					found = true
				}
			}
			if !found {
				t.Fatal("substitution control anchor absent")
			}
			mutant := t.TempDir()
			writeTree(t, mutant, broken)
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, mutant, "main.py", inputs)
				if err == nil || !strings.Contains(out+stderr, "message field is explicitly at its default") && !strings.Contains(out+stderr, "message keyed slot is explicitly at its default") {
					t.Fatalf("substitution control was not detected: %v\n%s\n%s", err, out, stderr)
				}
			}
		})
	}
}

func TestPythonMessageSubstitutedUnion(t *testing.T) {
	t.Parallel()
	writer := loadUnit(t, "package msgsubunion\nunion Choice { number int32\n ping }\nfixed table Root { tick uint8\n choice Choice }\n")
	reader := loadUnit(t, "package msgsubunion\nunion Choice { ping }\nfixed table Root { tick uint8\n choice Choice }\n")
	wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
	inst := wm.New(wm.Lookup("Root"))
	var report tabletext.Report
	if !wm.Read(inst, []byte(`{"tick":1,"choice":{"number":7}}`), &report) || report.Refused {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	announcement := append([]byte(nil), tablewire.Announce(writer)...)
	copy(announcement[3:11], tablewire.Announce(reader)[3:11])
	var v tablewire.Vocabulary
	var ar tabletext.Report
	if err = v.AnnounceRead(announcement, &ar); err != nil {
		t.Fatal(err)
	}
	result := rm.New(rm.Lookup("Root"))
	var rr tabletext.Report
	_, ok, err := tablewire.DecodeMessages(rm, []*tabletext.Instance{result}, wire, &v, &rr)
	if !ok || err != nil || rr.Unknown != 1 {
		t.Fatalf("Go oracle: %v %v %+v", ok, err, rr)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from msgsubunion import Root, MessageVocabulary, announce_read, load_messages, LoadReport
with open('announce','rb') as f: announcement=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,announcement);report=LoadReport()
value=load_messages(Root,v,wire,report=report)[0]
assert value.tick==1 and value.choice.type==0 and report.unknown==1
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "announce": announcement, "wire": wire}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("Python union substitution: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgsubunion"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	broken := make(map[string][]byte, len(files))
	found := false
	for name, content := range files {
		broken[name] = content
		if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(content), "stats[2] += 1") {
			broken[name] = []byte(strings.ReplaceAll(string(content), "stats[2] += 1", "stats[2] += 0"))
			found = true
		}
	}
	if !found {
		t.Fatal("substitution control anchor absent")
	}
	mutant := t.TempDir()
	writeTree(t, mutant, broken)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, mutant, "main.py", inputs)
		if err == nil || !strings.Contains(out+stderr, "message field is explicitly at its default") {
			t.Fatalf("union substitution control not detected: %v\n%s\n%s", err, out, stderr)
		}
	}
}

func TestPythonMessageSubstitutedKey(t *testing.T) {
	t.Parallel()
	writer := loadUnit(t, "package msgsubkey\nenum Slot { head, other }\nfixed table Root { tick uint8\n ranks [Slot]int32 }\n")
	reader := loadUnit(t, "package msgsubkey\nenum Slot { head, body }\nfixed table Root { tick uint8\n ranks [Slot]int32 }\n")
	wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
	inst := wm.New(wm.Lookup("Root"))
	var report tabletext.Report
	if !wm.Read(inst, []byte(`{"tick":1,"ranks":{"other":7}}`), &report) || report.Refused {
		t.Fatalf("fixture: %+v", report)
	}
	wire, err := tablewire.EncodeMessages(wm, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	announcement := append([]byte(nil), tablewire.Announce(writer)...)
	copy(announcement[3:11], tablewire.Announce(reader)[3:11])
	var v tablewire.Vocabulary
	var ar tabletext.Report
	if err = v.AnnounceRead(announcement, &ar); err != nil {
		t.Fatal(err)
	}
	result := rm.New(rm.Lookup("Root"))
	var rr tabletext.Report
	_, ok, err := tablewire.DecodeMessages(rm, []*tabletext.Instance{result}, wire, &v, &rr)
	if !ok || err != nil || rr.Unknown != 1 {
		t.Fatalf("Go oracle: %v %v %+v", ok, err, rr)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from msgsubkey import Root, MessageVocabulary, announce_read, load_messages, LoadReport
with open('announce','rb') as f: announcement=f.read()
with open('wire','rb') as f: wire=f.read()
v=MessageVocabulary();announce_read(v,announcement);report=LoadReport()
value=load_messages(Root,v,wire,report=report)[0]
assert value.tick==1 and value.ranks[1]==0 and value.ranks[2]==0 and report.unknown==1
from msgsubkey import ANNOUNCEMENT, Refused
from msgsubkey._schema._message_unit import _local, FIELD_KEYS
from msgsubkey._schema.message import BitWriter
own=MessageVocabulary();announce_read(own,ANNOUNCEMENT)
slot=_local.slots[FIELD_KEYS[('Root','ranks')]];shape=_local.entries[slot-1].shape
field=Root.descriptor.fields[1]; key=_local.names[field.keys[0]]
w=BitWriter();w.put(2,8);w.put(0,8);w.put(slot,_local.ref_bits)
w.put(1,shape.maximum.bit_length());w.put(key,_local.ref_bits);w.put(0,32);w.put(0,_local.ref_bits)
try:load_messages(Root,own,w.finish())
except Refused as e:assert e.reason=='wire_damaged'
else:raise AssertionError('explicit_keyed_default_control')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "announce": announcement, "wire": wire}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || string(bytes.TrimSpace([]byte(out))) != "ok" {
			t.Fatalf("Python key substitution: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "msgsubkey"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	// Remove both redundant default guards to prove the original strictness
	// remains live after adding the substitution exception.
	defaultMutant := make(map[string][]byte, len(files))
	defaultFound := false
	for name, content := range files {
		defaultMutant[name] = content
		if strings.HasSuffix(name, "_schema/message.py") {
			text := string(content)
			const fieldGuard = `if same_shape and stats[2] == substituted and field.is_default(value) and not getattr(field, "required", False):`
			const slotGuard = `if same_shape and stats[2] == substituted and fd.elem.is_default(item):`
			if strings.Count(text, fieldGuard) != 1 || strings.Count(text, slotGuard) != 1 {
				t.Fatal("keyed default controls absent")
			}
			text = strings.Replace(text, fieldGuard, "if False:", 1)
			text = strings.Replace(text, slotGuard, "if False:", 1)
			defaultMutant[name] = []byte(text)
			defaultFound = true
		}
	}
	if !defaultFound {
		t.Fatal("message runtime absent")
	}
	controlDir := t.TempDir()
	writeTree(t, controlDir, defaultMutant)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, controlDir, "main.py", inputs)
		if err == nil || !strings.Contains(out+stderr, "explicit_keyed_default_control") {
			t.Fatalf("keyed default control not detected: %v\n%s\n%s", err, out, stderr)
		}
	}

	broken := make(map[string][]byte, len(files))
	found := false
	for name, content := range files {
		broken[name] = content
		if strings.HasSuffix(name, "_schema/message.py") && strings.Contains(string(content), "stats[2] += 1") {
			broken[name] = []byte(strings.ReplaceAll(string(content), "stats[2] += 1", "stats[2] += 0"))
			found = true
		}
	}
	if !found {
		t.Fatal("substitution control anchor absent")
	}
	mutant := t.TempDir()
	writeTree(t, mutant, broken)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, mutant, "main.py", inputs)
		if err == nil || !strings.Contains(out+stderr, "message field is explicitly at its default") {
			t.Fatalf("key substitution control not detected: %v\n%s\n%s", err, out, stderr)
		}
	}
}
