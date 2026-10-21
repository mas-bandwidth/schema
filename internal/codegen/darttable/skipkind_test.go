package darttable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSkipKindStepsEveryScalarAtItsWidth: the variable wire's reader steps over
// an unnamed field by its kind (docs/SPEC-TABLES.md §3, §4), and the width of
// every scalar kind is ir.TableKindWidth, the one table every backend's skipper
// mirrors. A kind the table gives a width but skipKind leaves out reads as
// framing damage where the reference skips it: kind 36, the timestamp, was that
// gap (the Go oracle skips it at eight bytes since f107c3b7b).
func TestSkipKindStepsEveryScalarAtItsWidth(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, unitSchemas(t, "tables/pointers")...))
	if err != nil {
		t.Fatal(err)
	}
	var src string
	for _, data := range files {
		if s := string(data); strings.Contains(s, "bool skipKind(int kind) {") {
			src = s
			break
		}
	}
	if src == "" {
		t.Fatal("no generated library defines skipKind")
	}
	_, body, _ := strings.Cut(src, "bool skipKind(int kind) {")
	body, _, _ = strings.Cut(body, "\n  }\n")
	for kind := 1; kind < 64; kind++ {
		w := ir.TableKindWidth(kind)
		if w == 0 || kind == ir.TableKindPointer {
			continue // a pointer is a LEB on the variable wire
		}
		at := strings.Index(body, fmt.Sprintf("case %d:", kind))
		if at < 0 {
			t.Errorf("skipKind has no case for kind %d, which ir.TableKindWidth gives %d bytes", kind, w)
			continue
		}
		rest := body[at:]
		if before, _, ok := strings.Cut(rest, "return true;"); !ok || !strings.Contains(before, fmt.Sprintf("if (!has(%d)) return false;", w)) {
			t.Errorf("skipKind steps kind %d at other than its %d bytes", kind, w)
		}
	}
}
