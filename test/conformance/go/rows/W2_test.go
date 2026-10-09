// Cell go/W2 — "absent optional skips store" (docs/roadmap.sexp,
// optional-values/go; audit schema#898, matrix schema#876).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:203-206, §3.1 the write):
//
//	"The template is a compile-time constant — the hash, then zero
//	 everywhere a value lands ... **An absent optional** writes flag `0`
//	 and **skips the payload store**: a declared default under a clear
//	 flag is meaning too (fix 13)."
//
// and §7 proof item 1 (docs/FIXED-FORM-ALGORITHM.md:1682): "`p3` sets an
// absent link's payload in STORAGE on purpose and the file's bytes for it
// are the template's zeros, which is exactly the check".
//
// VECTOR: constructed from the law over test/tables/P3.schema — Chain has
// `link ?Link`; a Chain whose LinkPresent is false but whose Link carries
// the 0x5A stain the corpus sets on purpose. The body of a fixed file sits
// at TableFixedHeaderBytes(16) + 4 + len(ChainFixedLayout) + 8 (the record
// hash), per ChainFixedSave's FILE shape; the flag is at body offset 20
// (name string(16) = 4+16) and the payload slot at 21..37. Standalone: no
// other rows/ file, no fixture data, reaches the generated tables code
// (build/tables-generated-go/tblp3) the way the driver does.

package main

import (
	"bytes"
	"fmt"
	"testing"

	"tblp3"
)

var w2Failed int

func w2Check(ok bool, what string) {
	if ok {
		fmt.Printf("ok: %s\n", what)
	} else {
		fmt.Printf("FAIL: %s\n", what)
		w2Failed++
	}
}

func TestRowW2(t *testing.T) {
	w2Failed = 0
	bodyAt := tblp3.TableFixedHeaderBytes + 4 + len(tblp3.ChainFixedLayout) + 8

	// ---- ABSENT: LinkPresent = false, payload STAINED with 0x5A ----
	v := tblp3.Chain{}
	copy(v.Name[:], "absent")
	v.NameLength = 6
	v.LinkPresent = false
	v.Link.Value = 0x5A5A5A
	for i := range v.Link.Tag {
		v.Link.Tag[i] = 0x5A
	}
	v.Link.TagLength = 5
	w2Check(v.Link.Tag[0] == 0x5A && v.Link.Value == 0x5A5A5A,
		"CONTROL: the absent payload really is stained in storage")

	need := tblp3.ChainFixedMeasure(1)
	buf := make([]byte, need)
	n := tblp3.ChainFixedSave([]tblp3.Chain{v}, buf)
	w2Check(n == need, "absent optional: the record saves")

	body := buf[bodyAt : bodyAt+tblp3.ChainFixedBodyBytes]

	// THE LAW: not one byte of the absent payload reached the wire.
	w2Check(bytes.IndexByte(body, 0x5A) < 0,
		"ABSENT OPTIONAL: not one byte of the absent payload reached the wire")
	w2Check(body[20] == 0 && bytes.Equal(body[21:37], make([]byte, 16)),
		"absent optional: flag 0 on the wire and the slot holds the template's zeros")

	// ---- READ BACK: the reader answers the flag, not the stain ----
	back := make([]tblp3.Chain, 1)
	plan := make([]tblp3.TableFixedEntry, 256)
	var r tblp3.TableReport
	nn := tblp3.ChainFixedLoad(back, buf, plan, &r)
	w2Check(nn == 1 && r.Verdict == tblp3.TableOpenOk && !r.Malformed,
		"absent optional: the record reads")
	w2Check(!back[0].LinkPresent && back[0].Link.Value == 0 &&
		back[0].Link.TagLength == 0,
		"absent optional: the flag reads false and the payload reads the wire's zeros")
	w2Check(r.Clamped == 0 && r.Unknown == 0 && r.KindMismatch == 0 &&
		r.Widened == 0 && r.Duplicate == 0,
		"absent optional: a clean read moves no counter")

	// ---- THE DISCRIMINATING HALF: the SAME payload PRESENT reaches the wire,
	// so the check above is about the skip and not about a writer that never
	// writes payloads. ----
	present := v
	present.LinkPresent = true
	present.Link.TagLength = 4
	pbuf := make([]byte, need)
	n = tblp3.ChainFixedSave([]tblp3.Chain{present}, pbuf)
	pbody := pbuf[bodyAt : bodyAt+tblp3.ChainFixedBodyBytes]
	w2Check(n == need && bytes.IndexByte(pbody, 0x5A) >= 0 && pbody[20] == 1,
		"NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire under flag 1")

	if w2Failed != 0 {
		t.Fatalf("go/W2: %d assertion(s) red", w2Failed)
	}
}
