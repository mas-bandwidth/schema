// Cell go/E5 — "?T vs plain nesting" (docs/roadmap.sexp, optional-values/go;
// audit schema#898, matrix schema#876).
//
// THE LAWS:
//
//	docs/FIXED-FORM-ALGORITHM.md:182  `?T` | `1 + C(T)` | "the present flag
//	  THEN the payload, which rides WHOLE"
//	docs/FIXED-FORM-ALGORITHM.md:186-188  "Kind `35` is a LAYOUT kind, not a
//	  wire kind: §2.3 makes `?T` and a plain `T` wire-identical on form `1`,
//	  but here they are one byte apart"
//	docs/FIXED-FORM-ALGORITHM.md:1682 (§7 proof item 1)  "`p1`/`p3` (a value
//	  against a `?T`, present and absent) ... Read a file and save it back;
//	  the bytes must be identical"
//	docs/FIXED-FORM-ALGORITHM.md:1705 (§8 coverage matrix)  "`?T` against a
//	  plain nesting | `P1`/`P3`" — the go cell this file fills.
//	docs/SPEC-TABLES.md "The three wires": `Chain` is a declared `fixed
//	  table` in both P1 and P3, and every read at a fixed root answers
//	  `previous_form` for a form-`1` file, with nothing decoded and no
//	  counter moved. The four form-`1` chain fixtures are therefore refusal
//	  rows under both spellings; the `?T`-against-plain-nesting law is
//	  checked on form `3` below.
//
// P1 nests Link BY VALUE; P3 marks the same field `?Link` (test/tables/
// P1.schema, P3.schema). VECTOR: the fixture pins the driver also reads —
// testdata/wire/tables/chain_value.bin (saved under P1's plain nesting),
// chain_optional.bin (saved under P3 with the link PRESENT),
// chain_value_empty.bin and chain_optional_empty.bin — plus form-`3` files
// this test saves from the two generated modules. Derivation of the form-`3`
// byte expectations, from the layout law and test/tables/P3.schema:
// name string(16) rides 4+16 = 20 bytes; Link = int32(4) + string(8)(4+8)
// = 16; so P1.Chain's body is 20+16 = 36 and P3.Chain's is 20+(1+16) = 37,
// the present flag at body offset 20 and the payload at 21 (the flag THEN
// the payload). A fixed FILE is the 16-byte header, the layout behind its
// u32 length, then records of 8+body bytes (ChainFixedSave's own comment).
//
// Standalone: depends on no other rows/ file; reaches the generated tables
// code (build/tables-generated-go via go.mod replaces) the way the driver
// does.

package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"tblp1"
	"tblp3"
)

var e5Failed int

func e5Check(ok bool, what string) {
	if ok {
		fmt.Printf("ok: %s\n", what)
	} else {
		fmt.Printf("FAIL: %s\n", what)
		e5Failed++
	}
}

func e5Slurp(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s: %v\n", path, err)
		return nil
	}
	return data
}

func TestRowE5(t *testing.T) {
	e5Failed = 0
	const wire = "../../../../testdata/wire/tables/"

	// ---- form 1 at a fixed root: previous_form under BOTH spellings ----
	// docs/SPEC-TABLES.md "The three wires": `Chain` is a declared fixed table
	// in P1 and P3, so the plain ChainLoad refuses each form-1 fixture by
	// name, with nothing decoded (the value is the reset value, whatever the
	// caller's storage held) and no counter moved. The fixtures themselves
	// still pin that `?T` and a plain nesting were one form-1 spelling.
	chainValue := e5Slurp(wire + "chain_value.bin")
	chainOptional := e5Slurp(wire + "chain_optional.bin")
	chainValueEmpty := e5Slurp(wire + "chain_value_empty.bin")
	chainOptionalEmpty := e5Slurp(wire + "chain_optional_empty.bin")
	e5Check(chainValue != nil && chainOptional != nil &&
		chainValueEmpty != nil && chainOptionalEmpty != nil,
		"the four chain fixtures under testdata/wire/tables/ are present")

	e5Check(bytes.Equal(chainValue, chainOptional),
		"P1's chain_value.bin and P3's chain_optional.bin are byte-identical (form 1 spelled ?T and plain nesting alike)")

	var reset1 tblp1.Chain
	tblp1.ChainReset(&reset1)
	var reset3 tblp3.Chain
	tblp3.ChainReset(&reset3)
	fixtures := []struct {
		name string
		data []byte
	}{
		{"chain_value.bin", chainValue},
		{"chain_optional.bin", chainOptional},
		{"chain_value_empty.bin", chainValueEmpty},
		{"chain_optional_empty.bin", chainOptionalEmpty},
	}
	for _, f := range fixtures {
		e5Check(len(f.data) > 0 && f.data[0] == 1,
			"form1/"+f.name+": the fixture is form 1")

		v1 := tblp1.Chain{NameLength: 3}
		v1.Link.Value = -7
		var r1 tblp1.TableReport
		ok := tblp1.ChainLoad(&v1, f.data, &r1)
		e5Check(!ok && r1.Refusal == tblp1.TableRefusePreviousForm &&
			v1 == reset1 && r1.Unknown == 0 && r1.Widened == 0 && r1.Retained == 0 && r1.RetainLost == 0,
			"form1/"+f.name+": P1's plain load refuses previous_form, nothing decoded, no counter moved")

		v3 := tblp3.Chain{NameLength: 3, LinkPresent: true}
		v3.Link.Value = -7
		var r3 tblp3.TableReport
		ok = tblp3.ChainLoad(&v3, f.data, &r3)
		e5Check(!ok && r3.Refusal == tblp3.TableRefusePreviousForm &&
			v3 == reset3 && r3.Unknown == 0 && r3.Widened == 0 && r3.Retained == 0 && r3.RetainLost == 0,
			"form1/"+f.name+": P3's plain load refuses previous_form, nothing decoded, no counter moved")
	}
	var n int64

	// ---- form 3 (?T vs plain nesting are ONE BYTE APART) ----
	// docs/FIXED-FORM-ALGORITHM.md:182 and :186-188: the optional costs the
	// present flag and the payload then rides whole — exactly one inserted
	// byte over the plain nesting's record.
	// The FILE then is bigger by the record's one byte AND by the optional's
	// own layout entry: a layout is 4 bytes of count plus 17 bytes per entry,
	// P1 declares 5 and P3 6.
	e5Check(tblp3.ChainFixedBodyBytes == tblp1.ChainFixedBodyBytes+1 &&
		tblp3.ChainFixedRecordBytes == tblp1.ChainFixedRecordBytes+1 &&
		tblp3.ChainFixedMeasure(1)-tblp1.ChainFixedMeasure(1) == 1+17 &&
		tblp1.ChainFixedLayout[0] == 5 && tblp3.ChainFixedLayout[0] == 6,
		"C(?T) = 1 + C(T): the fixed body and record are one byte apart (36 -> 37, 44 -> 45), the file by that byte and the flag's own layout entry")

	one := tblp1.Chain{NameLength: 5}
	copy(one.Name[:], "hello")
	one.Link.Value = 42
	copy(one.Link.Tag[:], "tag")
	one.Link.TagLength = 3
	three := tblp3.Chain{Name: one.Name, NameLength: one.NameLength,
		Link: tblp3.Link{Value: one.Link.Value, Tag: one.Link.Tag,
			TagLength: one.Link.TagLength}, LinkPresent: true}

	p1File := make([]byte, tblp1.ChainFixedMeasure(1))
	p3File := make([]byte, tblp3.ChainFixedMeasure(1))
	n = tblp1.ChainFixedSave([]tblp1.Chain{one}, p1File)
	e5Check(int64(len(p1File)) == n, "P1 saves the present-content record")
	n = tblp3.ChainFixedSave([]tblp3.Chain{three}, p3File)
	e5Check(int64(len(p3File)) == n, "P3 saves the present ?T twin")

	p1Body := p1File[tblp1.TableFixedHeaderBytes+4+len(tblp1.ChainFixedLayout)+8:]
	p3Body := p3File[tblp3.TableFixedHeaderBytes+4+len(tblp3.ChainFixedLayout)+8:]
	e5Check(len(p1Body) == 36 && len(p3Body) == 37,
		"bodies are 36 and 37 bytes as the layout table derives")

	wants := append(append([]byte{}, p1Body[:20]...), 0x01)
	wants = append(wants, p1Body[20:]...)
	e5Check(bytes.Equal(p3Body, wants),
		"the present ?T record is the plain record with the flag 1 inserted BEFORE the payload — the payload rides WHOLE")

	// absent arm: flag 0, the payload slot template zeros, same length —
	// "an optional costs one presence bool and no pointer" (P3.schema).
	absent := three
	absent.LinkPresent = false
	aFile := make([]byte, tblp3.ChainFixedMeasure(1))
	n = tblp3.ChainFixedSave([]tblp3.Chain{absent}, aFile)
	aBody := aFile[tblp3.TableFixedHeaderBytes+4+len(tblp3.ChainFixedLayout)+8:]
	e5Check(n == int64(len(aFile)) && aBody[20] == 0 &&
		bytes.Equal(aBody[21:37], make([]byte, 16)) &&
		bytes.Equal(aBody[:20], p1Body[:20]),
		"the absent record: flag 0, payload slot zero, one byte apart still")

	// §7 proof item 1: read a file and save it back; the bytes must be
	// identical — for each of the three fixed files above.
	plan3 := make([]tblp3.TableFixedEntry, 64)
	back3 := make([]tblp3.Chain, 1)
	var fr3 tblp3.TableReport
	nn := tblp3.ChainFixedLoad(back3, p3File, plan3, &fr3)
	again3 := make([]byte, len(p3File))
	n = tblp3.ChainFixedSave(back3, again3)
	e5Check(nn == 1 && fr3.Verdict == tblp3.TableOpenOk && !fr3.Refused() &&
		n == int64(len(again3)) && bytes.Equal(again3, p3File),
		"P3's present file: read and saved back, the bytes are identical")

	back1 := make([]tblp1.Chain, 1)
	plan1 := make([]tblp1.TableFixedEntry, 64)
	var fr1 tblp1.TableReport
	nn1 := tblp1.ChainFixedLoad(back1, p1File, plan1, &fr1)
	again1 := make([]byte, len(p1File))
	n = tblp1.ChainFixedSave(back1, again1)
	e5Check(nn1 == 1 && fr1.Verdict == tblp1.TableOpenOk && !fr1.Refused() &&
		n == int64(len(again1)) && bytes.Equal(again1, p1File),
		"P1's plain-nesting file: read and saved back, the bytes are identical")

	backA := make([]tblp3.Chain, 1)
	var frA tblp3.TableReport
	nnA := tblp3.ChainFixedLoad(backA, aFile, plan3, &frA)
	againA := make([]byte, len(aFile))
	n = tblp3.ChainFixedSave(backA, againA)
	e5Check(nnA == 1 && frA.Verdict == tblp3.TableOpenOk && !frA.Refused() &&
		n == int64(len(againA)) && bytes.Equal(againA, aFile),
		"P3's absent file: read and saved back, the bytes are identical")

	// and the values themselves land as the flag says.
	e5Check(back3[0].LinkPresent && back3[0].Link.Value == 42 &&
		back3[0].Link.TagLength == 3 &&
		!backA[0].LinkPresent && backA[0].Link.Value == 0,
		"form 3 reads: present lands 42/\"tag\", absent reads flag false payload zero")

	if e5Failed != 0 {
		t.Fatalf("go/E5: %d assertion(s) red", e5Failed)
	}
}
