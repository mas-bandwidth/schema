package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// The listdemo unit (tables/lists/Save.schema). The Mixed table carries four
// lists of four element classes — an enum, a flags mask, a union and a ranged
// int32 — under the wire-fuzzer cause D9b.
const listdemoMixedSchema = `package listdemo

fixed table Placement
{
    x     float32
    y     float32
    model uint32
}

fixed table LogEntry { tick uint32 }

table Save
{
    placements []Placement
    log        []*LogEntry
    scores     []int32
}

enum Grade { A, B, C }

flags Perm { Read, Write, Own }

fixed table Point
{
    x int32
    y int32
}

union Hit
{
    point  Point
    damage int32
}

table Mixed
{
    grades []Grade
    perms  []Perm
    hits   []Hit
    bounds []int32 | min = 0, max = 100
}
`

// This historical fuzzer input contains later framing damage. It now pins
// whole-load discard, while a separate clean repeat below pins the original
// wrong-element-kind preservation rule without that damage masking the value.
const mixedListDecodedMutantHex = "01010e051e03020304010e12090203000000000000000400000010000000060e1a0f03070d2d080401000000090402000000000a040407000000000b0e0e040300000000320000006400000000c599f782764e0ad9ec2202864cfc63af522602864cfa63af052802864cff63afa82e867084edf24abbf00c9bccfb2d73b145c34435abfe73071702864cf563af541502864cf463afc07fb18abe08637f68b7f0ae4c0cf6530b00000000000000"

// Compare both public results with the current independent engine, including
// exact counters; the old partial-value golden predates whole-load discard.
func TestWireMixedListDecodedValueDart(t *testing.T) {
	out, err := runMixedListProbe(t, false)
	if err != nil {
		t.Fatalf("mixed list probe: %v\n%s", err, out)
	}
}

func TestWireMixedListDecodedValueDartNegativeControl(t *testing.T) {
	out, err := runMixedListProbe(t, true)
	if err == nil || !strings.Contains(string(out), "FAIL: clean repeat saved value") {
		t.Fatalf("clear-before-kind control did not fail on the live value oracle: %v\n%s", err, out)
	}
}

func runMixedListProbe(t *testing.T, sabotage bool) ([]byte, error) {
	t.Helper()
	u := buildUnit(t, listdemoMixedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["ListdemoWire.dart"])
	if wire == "" {
		t.Fatal("missing wire module")
	}
	if sabotage {
		// STRICT NEGATIVE CONTROL: drop the repeated-field refusal and the
		// element-kind refusal in the Mixed body, so the second grades
		// occurrence is skipped and the first lands — the tolerant read.
		// The strict probe must go red on the clean repeat's saved value.
		start := strings.Index(wire, "void mixedWireLoadBody(")
		if start < 0 {
			t.Fatal("control missing Mixed body")
		}
		seg := wire[start:]
		if !strings.Contains(seg, "if (!seenRefs.add(ref)) { r.report.refuse('wire_damaged'); return; }") {
			t.Fatal("control missing repeated-field refusal")
		}
		seg = strings.Replace(seg, "if (!seenRefs.add(ref)) { r.report.refuse('wire_damaged'); return; }", "if (!seenRefs.add(ref)) {}", 1)
		if !strings.Contains(seg, "if (elemKind != 30) { r.report.refuse('kind_mismatch'); r.pos = end; break; }") {
			t.Fatal("control missing enum kind check")
		}
		seg = strings.Replace(seg, "if (elemKind != 30) { r.report.refuse('kind_mismatch'); r.pos = end; break; }", "if (elemKind != 30) { r.pos = end; break; }", 1)
		wire = wire[:start] + seg
	}
	dir := t.TempDir()
	writeFiles(t, dir, packetFiles(t, u))
	if err := os.WriteFile(filepath.Join(dir, "ListdemoWire.dart"), []byte(wire), 0600); err != nil {
		t.Fatal(err)
	}
	mutant, err := hex.DecodeString(mixedListDecodedMutantHex)
	if err != nil {
		t.Fatal(err)
	}
	// Two fully framed occurrences of grades: valid enum elements followed by
	// a u64 list of the wrong kind. The second occurrence must preserve the first.
	clean := []byte{1, 1, 14, 5, 30, 3, 2, 3, 4, 1, 14, 18, 9, 2}
	clean = binary.LittleEndian.AppendUint64(clean, 3)
	clean = binary.LittleEndian.AppendUint64(clean, 4)
	clean = append(clean, 0)
	ids := binary.LittleEndian.Uint64(mutant[len(mutant)-8:])
	clean = append(clean, mutant[len(mutant)-int(ids)*8-8:]...)
	model := tabletext.NewModel(u)
	var rows strings.Builder
	for _, c := range []struct {
		name    string
		data    []byte
		damage  bool
		unknown int
	}{
		{"historical damage", mutant, true, 0}, {"clean repeat", clean, true, 0},
	} {
		value := model.New(model.Lookup("Mixed"))
		var r tabletext.Report
		ok, e := tablewire.Decode(model, value, c.data, &r)
		if (e != nil && !r.Refused) || ok == c.damage || r.Refused != c.damage || r.Unknown != c.unknown ||
			r.Widened != 0 {
			t.Fatalf("%s engine oracle: ok=%v err=%v report=%+v", c.name, ok, e, r)
		}
		saved, e := tablewire.Encode(model, value)
		if e != nil {
			t.Fatal(e)
		}
		if c.damage && hex.EncodeToString(saved) != "01000000000000000000" {
			t.Fatalf("damage did not produce all declared defaults: %x", saved)
		}
		if !c.damage && len(saved) <= 10 {
			t.Fatal("clean preservation witness has no live value")
		}
		fmt.Fprintf(&rows, "checkCase(%q,%s,%q,%d,%t);\n", c.name, dartCountBytes(c.data), hex.EncodeToString(saved), c.unknown, c.damage)
	}
	probe := strings.ReplaceAll(mixedListProbe, "CASES", rows.String())
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

const mixedListProbe = `
import 'dart:io';
import 'dart:typed_data';
import 'ListdemoWire.dart';
bool failed=false;
void check(bool ok,String what){if(!ok){stderr.writeln('FAIL: $what');failed=true;}}
String hex(List<int> b)=>b.map((v)=>v.toRadixString(16).padLeft(2,'0')).join();
void checkCase(String name,Uint8List wire,String expected,int unknown,bool damage){
 final r=TableWireReport();
 final v=mixedWireLoad(wire,r);
 check(hex(mixedWireSave(v))==expected,'$name saved value');
 check(r.refused==damage,'$name verdict');
 if(!damage){check(r.unknown==unknown && r.widened==0,'$name exact report');}
}
void main(){
 CASES
 if(failed)exit(1);
 stdout.writeln('historical damage/defaults and clean repeated-kind preservation pass');
}
`
