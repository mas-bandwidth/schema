// Package cards — render.go writes a record's known header fields as
// informational text and appends the stored body bytes unchanged. It is a
// known-field view, not a wire rewrite (card API AND OWNERSHIP; card STEP 4).
package cards

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Render loads the record's wire into a temporary measured/aligned region,
// reads the known header fields and the body from that region, and returns the
// card brief plus the load report (card API AND OWNERSHIP). RESULT is first,
// then the mapped keys in a fixed declaration order, then extras in stored
// order, then the body bytes verbatim; header layout canonicalizes with LF
// terminators, the body is never trimmed or rewritten (card STEP 4 RENDER).
func Render(record *Record) ([]byte, Report, error) {
	if record == nil {
		return nil, Report{}, errors.New("cards: nil record")
	}
	var report TableReport
	n := CardLoadMeasure(record.wire)
	if n < 0 {
		_, reason := CardLoadMeasureReason(record.wire)
		if reason != nil {
			report.Reason = reason.Error()
		}
		CardLoad(make([]byte, 64), record.wire, &report)
		return nil, report, fmt.Errorf("cards: wire is not a card: %v", reason)
	}
	region := alignedRegion(n)
	root := CardLoad(region, record.wire, &report)
	if root == nil {
		return nil, report, errors.New("cards: wire is malformed")
	}
	var b bytes.Buffer
	b.WriteString("RESULT: ")
	b.WriteString(cardString(root.Id[:], root.IdLength))
	b.WriteString(" sha=")
	b.WriteString(cardString(root.Sha[:], root.ShaLength))
	b.WriteString(" tier: ")
	b.WriteString(EnumNameTier(root.Tier))
	b.WriteByte('\n')

	writeStringHeader(&b, "REPO", cardString(root.Repo[:], root.RepoLength))
	writeStringHeader(&b, "BASE", cardString(root.Base[:], root.BaseLength))
	writeStringHeader(&b, "KIND", cardString(root.Kind[:], root.KindLength))
	if root.Priority != PriorityNone {
		b.WriteString("PRIORITY: ")
		b.WriteString(EnumNamePriority(root.Priority))
		b.WriteByte('\n')
	}
	writePathHeader(&b, "PATHS", &root.Paths, CardPathsEach)
	writePathHeader(&b, "NEW", &root.New, CardNewEach)
	writePathHeader(&b, "SHARED", &root.Shared, CardSharedEach)
	writeNeedsHeader(&b, &root.Needs)
	writeStringHeader(&b, "TEST", cardString(root.Test[:], root.TestLength))
	if root.DoneWhen != 0 {
		writeStringHeader(&b, "DONE-WHEN", string(stringData(&root.DoneWhen, nil)))
	}
	if root.DeadlineMinutes != 0 {
		b.WriteString("DEADLINE: finish within ")
		b.WriteString(strconv.FormatInt(int64(root.DeadlineMinutes), 10))
		b.WriteString(" minutes.\n")
	}
	if root.Source != 0 {
		writeStringHeader(&b, "SOURCE", string(stringData(&root.Source, nil)))
	}
	writeExtras(&b, &root.Extra)
	if root.Body != 0 {
		b.Write(stringData(&root.Body, nil))
	}
	return b.Bytes(), report, nil
}

// writeStringHeader emits `KEY: value` only when value is non-empty, the
// canonical spelling of an absent string default (card STEP 4 RENDER).
func writeStringHeader(b *bytes.Buffer, key, value string) {
	if value == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

// writePathHeader emits a comma-joined list header only when the list is
// non-empty, in canonical ", " order (card HEADER MAPPING, STEP 4).
func writePathHeader(b *bytes.Buffer, key string, list *TableList[PathTextRow],
	each func(*TableList[PathTextRow], func(*PathTextRow) bool, ...*TableArena) bool) {
	var parts []string
	each(list, func(e *PathTextRow) bool {
		parts = append(parts, cardString(e.S[:], e.SLength))
		return true
	})
	writeJoinedHeader(b, key, parts)
}

func writeNeedsHeader(b *bytes.Buffer, list *TableList[CardIDRow]) {
	var parts []string
	CardNeedsEach(list, func(e *CardIDRow) bool {
		parts = append(parts, cardString(e.S[:], e.SLength))
		return true
	})
	writeJoinedHeader(b, "DEPENDS-ON", parts)
}

func writeJoinedHeader(b *bytes.Buffer, key string, parts []string) {
	if len(parts) == 0 {
		return
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(strings.Join(parts, ", "))
	b.WriteByte('\n')
}

// writeExtras emits each extra as `KEY: value` in stored order, an empty value
// as a bare `KEY:` (card STEP 4 RENDER).
func writeExtras(b *bytes.Buffer, list *TableList[KeyValueRow]) {
	CardExtraEach(list, func(e *KeyValueRow) bool {
		b.WriteString(cardString(e.Key[:], e.KeyLength))
		b.WriteString(": ")
		if e.Value != 0 {
			b.Write(stringData(&e.Value, nil))
		}
		b.WriteByte('\n')
		return true
	})
}
