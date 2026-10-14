// render.go — the informational text view of a typed card.
//
// Render loads the record's wire into a temporary measured/aligned generated
// region and emits the admitted brief text: RESULT first, the mapped keys in a
// fixed documented order, extras in stored order, then the body bytes verbatim
// (docs/SPEC-TABLES.md §6.2, §16). It is a known-field view reporting tolerant
// projection events like ToJSON, never an archival/machine-state rewrite.
package cards

import (
	"bytes"
	"errors"
	"fmt"
)

// Render reads the record's wire into a fresh measured/aligned region and
// returns its informational text plus the load report. Failures and malformed
// wire are errors. The temporary region stays alive through the walk, and no
// generated *Card or shallow relative-pointer copy escapes.
func Render(record *Record) ([]byte, Report, error) {
	if record == nil || record.wire == nil {
		return nil, Report{}, errors.New("cards: nil record")
	}
	var report TableReport
	n := CardLoadMeasure(record.wire)
	if n < 0 {
		_, reason := CardLoadMeasureReason(record.wire)
		if reason != nil {
			report.Reason = reason.Error()
		}
		return nil, report, fmt.Errorf("cards: wire is not a card: %v", reason)
	}
	region := alignedRegion(n)
	root := CardLoad(region, record.wire, &report)
	if root == nil {
		return nil, report, errors.New("cards: wire is malformed")
	}

	var out bytes.Buffer
	tierName, err := tierString(root.Tier)
	if err != nil {
		return nil, report, err
	}
	out.WriteString("RESULT: ")
	out.WriteString(rowStr(root.Id[:], root.IdLength))
	out.WriteString(" sha=")
	out.WriteString(rowStr(root.Sha[:], root.ShaLength))
	out.WriteString(" tier: ")
	out.WriteString(tierName)
	out.WriteByte('\n')

	emitStr(&out, "REPO", rowStr(root.Repo[:], root.RepoLength))
	emitStr(&out, "BASE", rowStr(root.Base[:], root.BaseLength))
	emitStr(&out, "KIND", rowStr(root.Kind[:], root.KindLength))
	if prio, err := priorityString(root.Priority); err != nil {
		return nil, report, err
	} else if prio != "" {
		out.WriteString("PRIORITY: ")
		out.WriteString(prio)
		out.WriteByte('\n')
	}
	emitList(&out, "PATHS", collectPaths(&root.Paths))
	emitList(&out, "NEW", collectPaths(&root.New))
	emitList(&out, "SHARED", collectPaths(&root.Shared))
	emitList(&out, "DEPENDS-ON", collectIDs(&root.Needs))
	emitStr(&out, "TEST", rowStr(root.Test[:], root.TestLength))
	emitPtr(&out, "DONE-WHEN", &root.Done)
	emitDeadline(&out, root.DeadlineMinutes)
	emitPtr(&out, "SOURCE", &root.Source)

	CardExtraEach(&root.Extra, func(e *KeyValueRow) bool {
		out.WriteString(rowStr(e.Key[:], e.KeyLength))
		out.WriteString(": ")
		if e.Value != 0 {
			out.Write(stringData(&e.Value, nil))
		}
		out.WriteByte('\n')
		return true
	})

	if root.Body != 0 {
		out.Write(stringData(&root.Body, nil))
	}
	return out.Bytes(), report, nil
}

func tierString(t Tier) (string, error) {
	switch t {
	case Tierflash:
		return "flash", nil
	case Tierpro:
		return "pro", nil
	case Tierheavy:
		return "heavy", nil
	case Tierfrontier:
		return "frontier", nil
	}
	return "", fmt.Errorf("cards: tier %d has no header spelling", t)
}

func priorityString(p Priority) (string, error) {
	switch p {
	case PriorityNone:
		return "", nil
	case Prioritynormal:
		return "normal", nil
	case Priorityhigh:
		return "high", nil
	case Prioritycritical:
		return "critical", nil
	case Priorityblocker:
		return "blocker", nil
	}
	return "", fmt.Errorf("cards: priority %d has no header spelling", p)
}

func emitStr(out *bytes.Buffer, key, value string) {
	if value == "" {
		return
	}
	out.WriteString(key)
	out.WriteString(": ")
	out.WriteString(value)
	out.WriteByte('\n')
}

func emitPtr(out *bytes.Buffer, key string, slot *int64) {
	if *slot == 0 {
		return
	}
	out.WriteString(key)
	out.WriteString(": ")
	out.Write(stringData(slot, nil))
	out.WriteByte('\n')
}

func emitDeadline(out *bytes.Buffer, minutes int32) {
	if minutes == 0 {
		return
	}
	out.WriteString("DEADLINE: finish within ")
	out.WriteString(fmt.Sprintf("%d", minutes))
	out.WriteString(" minutes.")
	out.WriteByte('\n')
}

func emitList(out *bytes.Buffer, key string, elems []string) {
	if len(elems) == 0 {
		return
	}
	out.WriteString(key)
	out.WriteString(": ")
	for i, e := range elems {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(e)
	}
	out.WriteByte('\n')
}
