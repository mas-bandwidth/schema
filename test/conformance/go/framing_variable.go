// THE FRAMING VECTORS AT tblr1's FIXED ROOT (docs/LAYERS/1-framing.md).
//
// The framing vectors are form-1 files, and the rules they pin are the
// variable form's. tblr1's Cfg is declared `fixed table`, so its own Load
// refuses every form-1 file as previous_form before any framing rule runs
// (docs/SPEC-TABLES.md §3.4). The oracle reads these rows with Cfg declared a
// plain variable table (test/conformance/harness/generate.go,
// framingVariableUnit), and so does this driver: tblr1framing is the same unit
// generated with that one keyword dropped (make/go.mk). The verdict and the
// report are that generated read's. The value it reads is handed to the fixed
// unit through the text form, so the write-back and the value text are the
// fixed root's own.
package main

import (
	"fmt"
	"os"
	"unsafe"

	"tblr1"
	tblr1v "tblr1framing"
)

func tblr1CfgFramingLoad(value any, wire []byte, rep *report) bool {
	dst := value.(*tblr1.Cfg)
	tblr1.CfgReset(dst)
	n := tblr1v.CfgLoadMeasure(wire)
	if n < 0 {
		*rep = report{refused: true}
		return false
	}
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	var r tblr1v.TableReport
	root := tblr1v.CfgLoad(raw[off:off+uintptr(n)], wire, &r)
	*rep = report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	if root == nil || rep.refused {
		rep.refused = true
		return false
	}
	// The read accepted the file. A value the fixed unit cannot take back
	// must not turn that acceptance into a refusal, so it stops the driver.
	size := tblr1v.CfgToJSONMeasure(root)
	if size < 0 {
		fail("tblr1 Cfg framing: the variable read's value has no text")
	}
	text := make([]byte, size)
	if tblr1v.CfgToJSON(root, text) != size {
		fail("tblr1 Cfg framing: ToJSON did not write ToJSONMeasure's answer")
	}
	var back tblr1.TableReport
	if !tblr1.CfgFromJSON(dst, text, &back) {
		fail(fmt.Sprintf("tblr1 Cfg framing: the fixed unit refused the variable read's value %s", text))
	}
	return true
}

func fail(why string) {
	fmt.Fprintln(os.Stderr, why)
	os.Exit(1)
}
