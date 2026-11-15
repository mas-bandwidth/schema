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
//	docs/SPEC-TABLES.md:7194  "A field moved between `?T` and a plain nesting
//	  is not an evolution event at all — the bytes do not move."
//
// P1 nests Link BY VALUE; P3 marks the same field `?Link` (test/tables/
// P1.schema, P3.schema). Both are FIXED-CLASS roots, so the strict read is
// form `3` and their historical form-`1` files are previous_form rows
// (testdata/conformance/tables/FIXED-MIGRATION.md). VECTOR: the form-`3`
// fixtures testdata/conformance/tables/wire/chain_value.bin (P1),
// chain_optional.bin (P3 with the link PRESENT), chain_value_empty.bin and
// chain_optional_empty.bin — plus form-`3` files this test saves from the
// two generated modules. The old testdata/wire/tables/ files stay asserted
// only as the retired form-1 refusals. Derivation of the form-`3`
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

func e5Silent(r1 tblp1.TableReport) bool {
	return r1.Verdict == tblp1.TableOpenOk && !r1.Refused() &&
		r1.Unknown == 0 && r1.Widened == 0
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

	// ---- form 1 is the HISTORICAL form; form 3 is the strict read ----
	// P1 and P3 are fixed-class roots, so a form-1 file is previous_form
	// under the strict read (docs/plans/strict-only.md;
	// testdata/conformance/tables/FIXED-MIGRATION.md). The four historical
	// fixtures stay checked as refusals; the positive seam is form 3's.
	chainValue := e5Slurp(wire + "chain_value.bin")
	chainOptional := e5Slurp(wire + "chain_optional.bin")
	chainValueEmpty := e5Slurp(wire + "chain_value_empty.bin")
	chainOptionalEmpty := e5Slurp(wire + "chain_optional_empty.bin")
	e5Check(chainValue != nil && chainOptional != nil &&
		chainValueEmpty != nil && chainOptionalEmpty != nil,
		"the four historical chain fixtures under testdata/wire/tables/ are present")

	e5Check(bytes.Equal(chainValue, chainOptional),
		"the historical P1 and P3 form-1 files are byte-identical (the form-1 bytes do not move)")

	var old1 tblp1.Chain
	var oldR1 tblp1.TableReport
	e5Check(!tblp1.ChainLoad(&old1, chainValue, &oldR1) &&
		oldR1.Refusal == tblp1.TableRefusePreviousForm,
		"form 1 is retired: P1 refuses its historical chain_value.bin as previous_form")
	oldR1 = tblp1.TableReport{}
	e5Check(!tblp1.ChainLoad(&old1, chainValueEmpty, &oldR1) &&
		oldR1.Refusal == tblp1.TableRefusePreviousForm,
		"form 1 is retired: P1 refuses its historical chain_value_empty.bin as previous_form")
	var old3 tblp3.Chain
	var oldR3 tblp3.TableReport
	e5Check(!tblp3.ChainLoad(&old3, chainOptional, &oldR3) &&
		oldR3.Refusal == tblp3.TableRefusePreviousForm,
		"form 1 is retired: P3 refuses its historical chain_optional.bin as previous_form")
	oldR3 = tblp3.TableReport{}
	e5Check(!tblp3.ChainLoad(&old3, chainOptionalEmpty, &oldR3) &&
		oldR3.Refusal == tblp3.TableRefusePreviousForm,
		"form 1 is retired: P3 refuses its historical chain_optional_empty.bin as previous_form")

	// ---- form 3 fixtures: the migration's positive inputs ----
	const fixedWire = "../../../../testdata/conformance/tables/wire/"
	fv := e5Slurp(fixedWire + "chain_value.bin")
	fo := e5Slurp(fixedWire + "chain_optional.bin")
	fve := e5Slurp(fixedWire + "chain_value_empty.bin")
	foe := e5Slurp(fixedWire + "chain_optional_empty.bin")
	e5Check(fv != nil && fo != nil && fve != nil && foe != nil,
		"the four form-3 chain fixtures under testdata/conformance/tables/wire/ are present")

	var v1 [1]tblp1.Chain
	var r1 tblp1.TableReport
	fplan1 := make([]tblp1.TableFixedEntry, 64)
	n := tblp1.ChainFixedLoad(v1[:], fv, fplan1, &r1)
	e5Check(n == 1 && e5Silent(r1) &&
		v1[0].NameLength == 5 && string(v1[0].Name[:5]) == "chain" &&
		v1[0].Link.Value == 7 && v1[0].Link.TagLength == 3 &&
		string(v1[0].Link.Tag[:3]) == "tip",
		"P1 reads its form-3 chain_value.bin: 7/\"tip\"/\"chain\"")

	var v3 [1]tblp3.Chain
	var r3 tblp3.TableReport
	fplan3 := make([]tblp3.TableFixedEntry, 64)
	n = tblp3.ChainFixedLoad(v3[:], fo, fplan3, &r3)
	e5Check(n == 1 && e5SilentP3(r3) && v3[0].LinkPresent &&
		v3[0].NameLength == 5 && string(v3[0].Name[:5]) == "chain" &&
		v3[0].Link.Value == 7 && v3[0].Link.TagLength == 3 &&
		string(v3[0].Link.Tag[:3]) == "tip",
		"P3 reads its form-3 chain_optional.bin with the ?Link present")

	var e1 [1]tblp1.Chain
	var er1 tblp1.TableReport
	n = tblp1.ChainFixedLoad(e1[:], fve, fplan1, &er1)
	e5Check(n == 1 && e5Silent(er1) && e1[0].NameLength == 0 &&
		e1[0].Link.Value == 0 && e1[0].Link.TagLength == 0,
		"P1's form-3 empty file reads all-default")

	var e3 [1]tblp3.Chain
	var er3 tblp3.TableReport
	n = tblp3.ChainFixedLoad(e3[:], foe, fplan3, &er3)
	e5Check(n == 1 && e5SilentP3(er3) && e3[0].LinkPresent &&
		e3[0].Link.Value == 0 && e3[0].Link.TagLength == 0,
		"P3's form-3 empty file reads its present ?Link with a zero payload")

	// read a fixture and write it back: the bytes are identical (§7 item 1).
	fagain1 := make([]byte, len(fv))
	n = tblp1.ChainFixedSave(v1[:], fagain1)
	e5Check(n == int64(len(fagain1)) && bytes.Equal(fagain1, fv),
		"P1's form-3 file: read and saved back, the bytes are identical")
	fagain3 := make([]byte, len(fo))
	n = tblp3.ChainFixedSave(v3[:], fagain3)
	e5Check(n == int64(len(fagain3)) && bytes.Equal(fagain3, fo),
		"P3's form-3 file: read and saved back, the bytes are identical")

	// the fixture bodies are one flag byte apart, as the seam law says.
	fb1 := fv[tblp1.TableFixedHeaderBytes+4+len(tblp1.ChainFixedLayout)+8:]
	fb3 := fo[tblp3.TableFixedHeaderBytes+4+len(tblp3.ChainFixedLayout)+8:]
	e5Check(len(fb1) == 36 && len(fb3) == 37 &&
		bytes.Equal(fb3[:20], fb1[:20]) && fb3[20] == 0x01 &&
		bytes.Equal(fb3[21:], fb1[20:]),
		"the form-3 fixtures: P3's present record is P1's with the flag 1 inserted before the payload")

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

func e5SilentP3(r tblp3.TableReport) bool {
	return r.Verdict == tblp3.TableOpenOk && !r.Refused() &&
		r.Unknown == 0 && r.Widened == 0
}
