package gotable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestMessageWriter(t *testing.T) {
	schema := `package probe
 enum Grade { Gold, Silver }
 type Nested { n int32 }
 union Choice { number uint128 | overlay
 pair [2]int16
 words string(12)
 ping }
 fixed table Root { yes bool
 small int16 | min = -5, max = 14
 raw uint64
 big uint128
 text string(15)
 blob bytes(9)
 wide wstring(8)
 items [..3]int32
 grades [Grade]Nested
 choice Choice
 optional ?Nested }
 `
	text := `{"yes":true,"small":-3,"raw":18446744073709551615,"big":340282366920938463463374607431768211455,"text":"hi😀","blob":"AAECAw==","wide":"hello","items":[-3,7],"grades":{"Silver":{"n":4}},"choice":{"pair":[5,9]},"optional":{}}`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(text), &report) || !report.Silent() {
		t.Fatalf("text %+v", report)
	}
	expected, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v, v})
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, schema, fmt.Sprintf(`package probe
 import("testing";"bytes")
 func TestMessage(t *testing.T){var v Root;var report TableReport;if !RootFromJson(&v,[]byte(%q),&report)||report!=(TableReport{}){t.Fatalf("json %%+v",report)};values:=[]*Root{&v,&v};want:=[]byte{%s};if RootMeasureMessages(values,nil)!=int64(len(want)){t.Fatalf("measure %%d want %%d",RootMeasureMessages(values,nil),len(want))};out:=make([]byte,len(want));if RootSaveMessages(values,out,&report)!=int64(len(want))||!bytes.Equal(out,want){t.Fatalf("message %%x want %%x",out,want)}
 var storage [TableMessageEntriesHere]TableMessageEntry;var vocab TableVocabulary;vocab.Init(storage[:]);announcement:=make([]byte,AnnounceMeasure());Announce(announcement);if !AnnounceRead(&vocab,announcement,&report)||!vocab.Announced||vocab.Count!=TableMessageEntriesHere{t.Fatalf("announce %%+v",report)};for i:=range tableMessageEntries{if vocab.Entries[i]!=tableMessageEntries[i]{t.Fatalf("entry %%d: %%+v want %%+v",i,vocab.Entries[i],tableMessageEntries[i])}}
 var decoded [2]Root;if n,ok:=RootLoadMessages(decoded[:],&vocab,out,&report);!ok||n!=2||decoded[0]!=v||decoded[1]!=v {t.Fatalf("load %%d %%v %%+v got %%+v want %%+v",n,ok,report,decoded[0],v)}
 if allocations:=testing.AllocsPerRun(10,func(){RootMeasureMessages(values,nil);RootSaveMessages(values,out,nil);RootLoadMessages(decoded[:],&vocab,out,nil);vocab.Init(storage[:]);AnnounceRead(&vocab,announcement,nil)});allocations!=0{t.Fatalf("allocations %%v",allocations)}
 }
  `, text, byteLiterals(expected)))
}

func TestGoMessageSkipOfZeroWidthElementsIsBounded(t *testing.T) {
	t.Parallel()
	const probeSchema = `package probe

fixed table Root {
	n int32
}
`
	u := unitFrom(t, probeSchema)
	text := tableMessageRuntime(u)

	// Step 2 (red first): The emitted skip for kind 0 and 32 elements must take
	// the arithmetic branch, and a nested zero-width element must be refused.
	if !strings.Contains(text, "e.Element.Kind==0") || !strings.Contains(text, "e.Element.Kind==32") {
		t.Fatal("the emitted skip does not take the arithmetic branch for kind 0 and 32 elements")
	}
	if !strings.Contains(text, "r.Offset==before") {
		t.Fatal("the emitted skip does not refuse a nested zero-width element")
	}

	t.Run("hostile_announcement", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")

		probeTest := `package probe

import (
	"encoding/binary"
	"testing"
)

func TestHostileAnnouncementBounded(t *testing.T) {
	lebs := func(v uint64) []byte {
		var out []byte
		for v >= 0x80 {
			out = append(out, uint8(v)|0x80)
			v >>= 7
		}
		return append(out, uint8(v))
	}

	// Hostile announcement {kind 14, min = max = 0xFFFFFFFF, elem 0}
	entry := make([]byte, 0, 32)
	entry = binary.LittleEndian.AppendUint64(entry, 0x1000)
	entry = append(entry, 14)
	entry = append(entry, lebs(0xFFFFFFFF)...)
	entry = append(entry, lebs(0xFFFFFFFF)...)
	entry = append(entry, 0) // elem 0

	vocabSub := append([]byte{6}, lebs(uint64(len(entry)))...)
	vocabSub = append(vocabSub, entry...)
	vocabField := append([]byte{2, 14}, lebs(uint64(len(vocabSub)))...)
	vocabField = append(vocabField, vocabSub...)

	versionField := binary.LittleEndian.AppendUint64([]byte{1, 9}, BuildVersion)

	announcement := append([]byte{1}, versionField...)
	announcement = append(announcement, vocabField...)
	announcement = append(announcement, 0x00) // terminator reference
	announcement = binary.LittleEndian.AppendUint64(announcement, 0xfffffffffffffffe)
	announcement = binary.LittleEndian.AppendUint64(announcement, 0xfffffffffffffffd)
	announcement = binary.LittleEndian.AppendUint64(announcement, 2)

	var storage [16]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	var rep TableReport
	if !AnnounceRead(&vocab, announcement, &rep) || !vocab.Announced {
		t.Fatalf("AnnounceRead failed: %+v", rep)
	}

	// Batch of 1 message (count 0), with field reference 1 (1 bit = 1),
	// followed by terminator reference 0 (1 bit = 0) and padding zeros.
	batch := []byte{2, 0, 0x01}

	MessageSkipIterations = 0
	var out [1]Root
	var loadRep TableReport
	n, ok := RootLoadMessages(out[:], &vocab, batch, &loadRep)
	if !ok || n != 1 || loadRep.Malformed {
		t.Fatalf("RootLoadMessages failed: n=%d ok=%v rep=%+v", n, ok, loadRep)
	}
	if MessageSkipIterations != 0 {
		t.Fatalf("expected bounded count of 0 iterations recorded by test hook, got %d", MessageSkipIterations)
	}

	// Also test that a nested zero-width element is refused as malformed:
	// {kind 14, min = 1, max = 1, elem 14 {min = 0, max = 0, elem 0}}
	nestedEntry := make([]byte, 0, 32)
	nestedEntry = binary.LittleEndian.AppendUint64(nestedEntry, 0x2000)
	nestedEntry = append(nestedEntry, 14)       // outer kind 14
	nestedEntry = append(nestedEntry, lebs(1)...) // outer min 1
	nestedEntry = append(nestedEntry, lebs(1)...) // outer max 1
	nestedEntry = append(nestedEntry, 14)         // elem 14 (nested array)
	nestedEntry = append(nestedEntry, lebs(0)...) // inner min 0
	nestedEntry = append(nestedEntry, lebs(0)...) // inner max 0
	nestedEntry = append(nestedEntry, 0)          // inner elem 0

	vocabSub2 := append([]byte{6}, lebs(uint64(len(nestedEntry)))...)
	vocabSub2 = append(vocabSub2, nestedEntry...)
	vocabField2 := append([]byte{2, 14}, lebs(uint64(len(vocabSub2)))...)
	vocabField2 = append(vocabField2, vocabSub2...)

	announcement2 := append([]byte{1}, versionField...)
	announcement2 = append(announcement2, vocabField2...)
	announcement2 = append(announcement2, 0x00)
	announcement2 = binary.LittleEndian.AppendUint64(announcement2, 0xfffffffffffffffe)
	announcement2 = binary.LittleEndian.AppendUint64(announcement2, 0xfffffffffffffffd)
	announcement2 = binary.LittleEndian.AppendUint64(announcement2, 2)

	var vocab2 TableVocabulary
	vocab2.Init(storage[:])
	var rep2 TableReport
	if !AnnounceRead(&vocab2, announcement2, &rep2) || !vocab2.Announced {
		t.Fatalf("AnnounceRead 2 failed: %+v", rep2)
	}

	var loadRep2 TableReport
	n2, ok2 := RootLoadMessages(out[:], &vocab2, batch, &loadRep2)
	if ok2 || n2 != 0 || !loadRep2.Malformed {
		t.Fatalf("expected nested zero-width element to be refused as malformed: n=%d ok=%v rep=%+v", n2, ok2, loadRep2)
	}
}
`

		edit := func(files map[string][]byte) {
			injected := false
			for name, data := range files {
				s := string(data)
				if strings.Contains(s, "func tableMessageSkip(") {
					s += "\nvar MessageSkipIterations uint64\n" +
						"var MessageSkipLoopHook = func() { MessageSkipIterations++; if MessageSkipIterations > 128 { panic(\"loop not bounded: iterations exceeded 128\") } }\n"
					s = strings.ReplaceAll(s, "for i:=uint64(0);i<n;i++ {", "for i:=uint64(0);i<n;i++ { MessageSkipLoopHook();")
					files[name] = []byte(s)
					injected = true
					break
				}
			}
			if !injected {
				t.Fatal("failed to inject test hook into generated files")
			}
		}

		out, err := runGeneratedEdited(t, probeSchema, probeTest, edit)
		if err != nil {
			t.Fatalf("generated test failed: %v\n%s", err, out)
		}
	})
}
