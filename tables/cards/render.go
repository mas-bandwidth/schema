// Render emits the known-field informational text view of a Record: RESULT
// first, mapped keys in a fixed declaration order, extras in stored order, then
// the stored body bytes appended directly. It is a projection like ToJSON, not
// an archival rewrite (the API AND OWNERSHIP paragraph of this card;
// docs/SPEC-TABLES.md §16).
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
)

// Render loads a temporary measured/aligned generated region from the record's
// wire, emits the text grammar, keeps that region alive through the output and
// returns the load report. Malformed data is an error
// (docs/SPEC-TABLES.md §2, §6.2).
func Render(record *Record) ([]byte, Report, error) {
	if record == nil {
		return nil, Report{}, errors.New("cards: nil record")
	}
	wire := record.Wire()
	var report TableReport
	n := CardLoadMeasure(wire)
	if n < 0 {
		return nil, report, errors.New("cards: record has no loadable region")
	}
	region := alignedRegion(n)
	root := CardLoad(region, wire, &report)
	if root == nil {
		return nil, report, errors.New("cards: record is malformed")
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "RESULT: %s sha=%s tier: %s\n",
		fixedString(root.Id[:], root.IdLength),
		fixedString(root.Sha[:], root.ShaLength),
		EnumNameTier(root.Tier))
	emitFixed(&out, "REPO", root.Repo[:], root.RepoLength)
	emitFixed(&out, "BASE", root.Base[:], root.BaseLength)
	emitFixed(&out, "KIND", root.Kind[:], root.KindLength)
	if root.Priority != PriorityNone {
		fmt.Fprintf(&out, "PRIORITY: %s\n", EnumNamePriority(root.Priority))
	}
	emitPathList(&out, "PATHS", &root.Paths)
	emitPathList(&out, "NEW", &root.New)
	emitPathList(&out, "SHARED", &root.Shared)
	emitIDList(&out, "DEPENDS-ON", &root.Needs)
	emitFixed(&out, "TEST", root.Test[:], root.TestLength)
	if root.DoneWhen != 0 {
		fmt.Fprintf(&out, "DONE-WHEN: %s\n", stringData(&root.DoneWhen, nil))
	}
	if root.DeadlineMinutes > 0 {
		fmt.Fprintf(&out, "DEADLINE: finish within %d minutes.\n", root.DeadlineMinutes)
	}
	if root.Source != 0 {
		fmt.Fprintf(&out, "SOURCE: %s\n", stringData(&root.Source, nil))
	}
	CardExtraEach(&root.Extra, func(e *KeyValueRow) bool {
		var value []byte
		if e.Value != 0 {
			value = stringData(&e.Value, nil)
		}
		fmt.Fprintf(&out, "%s: %s\n", fixedString(e.Key[:], e.KeyLength), value)
		return true
	})
	out.Write(stringData(&root.Body, nil))
	runtime.KeepAlive(region)
	return out.Bytes(), report, nil
}

// emitFixed writes one bounded fixed-string field when it is non-empty.
func emitFixed(out *bytes.Buffer, key string, buf []byte, n int32) {
	if v := fixedString(buf, n); v != "" {
		fmt.Fprintf(out, "%s: %s\n", key, v)
	}
}

// emitPathList writes a []PathText as a comma-separated header line, choosing
// the generated iterator that matches the named field.
func emitPathList(out *bytes.Buffer, key string, list *TableList[PathTextRow]) {
	collect := func(visit func(*PathTextRow) bool) {
		switch key {
		case "PATHS":
			CardPathsEach(list, visit)
		case "NEW":
			CardNewEach(list, visit)
		default:
			CardSharedEach(list, visit)
		}
	}
	var elems []string
	collect(func(e *PathTextRow) bool {
		elems = append(elems, fixedString(e.S[:], e.SLength))
		return true
	})
	if len(elems) > 0 {
		fmt.Fprintf(out, "%s: %s\n", key, joinComma(elems))
	}
}

// joinComma renders trimmed list elements in the comma-separated header form.
func joinComma(elems []string) string {
	out := elems[0]
	for _, e := range elems[1:] {
		out += ", " + e
	}
	return out
}

// emitIDList writes a []CardID as a comma-separated header line.
func emitIDList(out *bytes.Buffer, key string, list *TableList[CardIDRow]) {
	var elems []string
	CardNeedsEach(list, func(e *CardIDRow) bool {
		elems = append(elems, fixedString(e.S[:], e.SLength))
		return true
	})
	if len(elems) > 0 {
		fmt.Fprintf(out, "%s: %s\n", key, joinComma(elems))
	}
}

// fixedString reads a generated fixed string(N) buffer and its used length
// companion (docs/SPEC-TABLES.md §6.1).
func fixedString(buf []byte, n int32) string {
	if n < 0 {
		n = 0
	}
	if int(n) > len(buf) {
		n = int32(len(buf))
	}
	return string(buf[:n])
}
