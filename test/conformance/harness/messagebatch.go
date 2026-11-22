// The `message-batch` rows: THE COUNT A REFUSED BATCH RETURNS, on every leg.
//
// docs/SPEC-TABLES.md §3.3: "**A refused batch returns a count of `0`**, as
// every refused batch does" and "The batch refuses `wire_damaged`, its count
// is `0`, no body of the batch is delivered, and nothing in the caller's
// storage is a value to use, the bodies before the damage included". §6.5:
// "the batch is accepted whole, every body delivered, or refused with a count
// of `0`."
//
// A row hands a driver one batch. The driver loads it into storage for 256
// bodies (the most a batch carries, so no caller capacity is in play: §6.5,
// "RESOURCE LIMITS ARE OUTSIDE THE VERDICT CONTRACT", and §3.3, "the shared
// suites assert verdicts only, never a caller's capacity") and answers one
// line:
//
//	count <n>
//
// `<n>` is the count the loader reported to its caller, exactly as reported
// (the return value, the out-parameter, or the length of the list a loader
// that hands back a fresh list returned; a loader that raises delivered no
// list, which is 0).
//
// What a row owes, by its verdict:
//
//   - `delivered`: the count is the wire's.
//   - `refused`: the count is 0. NOTHING is asserted about the storage after
//     a refusal inside the batch (§3.3: "nothing in the caller's storage is a
//     value to use"; the spec leaves its contents undefined) and nothing about
//     the reason (docs/rulings.json, `refusal-verdict-only`).
//
// A refusal decided by a caller's capacity is NOT a row here: it is a resource
// limit, and its rows run in each leg's own tests against the committed
// two-body fixture testdata/batch_capacity_two.bin under that leg's package.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// parseMessageBatchAnswer reads one driver answer: `count <n>`.
func parseMessageBatchAnswer(got []byte) (string, error) {
	text := strings.TrimRight(string(got), "\n")
	if strings.Contains(text, "\n") {
		return "", fmt.Errorf("the answer is one line, `count <n>`, not %q", text)
	}
	count, ok := strings.CutPrefix(text, "count ")
	if !ok {
		return "", fmt.Errorf("the answer is `count <n>`, not %q", text)
	}
	return count, nil
}

// messageBatchVerdict holds one answer to its row. wire is the batch the
// generator wrote. It returns "" for a pass and the failure otherwise.
func messageBatchVerdict(b *MessageBatch, count string, wire []byte) string {
	switch b.Verdict {
	case "delivered":
		if len(wire) < 2 {
			return "the row's wire carries no count"
		}
		want := fmt.Sprint(int(wire[1]) + 1)
		if count != want {
			return fmt.Sprintf("a lawful batch of %s reported count %s", want, count)
		}
	case "refused":
		if count != "0" {
			return fmt.Sprintf("a refused batch returns a count of 0 (§3.3), and the loader reported %s", count)
		}
	}
	return ""
}

// scoreMessageBatch scores one message-batch case a leg wrote.
func scoreMessageBatch(answer string, e expectation, r *result) {
	fail := func(format string, args ...any) {
		r.failures = append(r.failures, e.name+": "+fmt.Sprintf(format, args...))
		r.mark(e.name, outcomeFail)
	}
	got, err := os.ReadFile(answer)
	if err != nil {
		if _, absent := os.Stat(answer + ".absent"); absent == nil {
			r.missing++
			r.mark(e.name, outcomeAbsent)
			return
		}
		fail("the driver wrote nothing")
		return
	}
	count, err := parseMessageBatchAnswer(got)
	if err != nil {
		fail("%v", err)
		return
	}
	wire, err := os.ReadFile(e.batch.Wire())
	if err != nil {
		fail("%v — run: make conformance-generate", err)
		return
	}
	if why := messageBatchVerdict(e.batch, count, wire); why != "" {
		fail("%s", why)
		return
	}
	r.pass++
	r.mark(e.name, outcomePass)
}

// generateMessageBatches writes each row's wire from the compiler's own
// engine, and holds the engine to the row's verdict before it writes: a row
// the oracle does not answer as the manifest says asks every leg for an answer
// the reference does not give.
func generateMessageBatches(u *units, m *Manifest) error {
	for _, mb := range m.Batches {
		unit, err := u.get(mb.Unit)
		if err != nil {
			return err
		}
		conn, err := m.LookupConnection(mb.Connection)
		if err != nil {
			return fmt.Errorf("message-batch %s: %w", mb.Name, err)
		}
		ann, err := os.ReadFile(conn.Wire)
		if err != nil {
			return fmt.Errorf("message-batch %s: %w", mb.Name, err)
		}
		var vocab tablewire.Vocabulary
		var arep tabletext.Report
		if err := vocab.AnnounceRead(ann, &arep); err != nil || !vocab.Announced() || arep.Refused {
			return fmt.Errorf("message-batch %s: announcement refused: %w %+v", mb.Name, err, arep)
		}
		model := tabletext.NewModel(unit)
		root := model.Lookup(mb.Root)
		if root == nil {
			return fmt.Errorf("message-batch %s: root %q names no table", mb.Name, mb.Root)
		}
		text, err := os.ReadFile(mb.SentJSON)
		if err != nil {
			return fmt.Errorf("message-batch %s: %w", mb.Name, err)
		}
		bodies := []*tabletext.Instance{model.New(root), model.New(root)}
		for _, inst := range bodies {
			var rep tabletext.Report
			if !model.Read(inst, text, &rep) || rep.Refused {
				return fmt.Errorf("message-batch %s: %s does not read as %s: %+v", mb.Name, mb.SentJSON, mb.Root, rep)
			}
		}
		sent, err := tablewire.EncodeMessages(model, bodies)
		if err != nil {
			return fmt.Errorf("message-batch %s: EncodeMessages: %w", mb.Name, err)
		}
		load := func(wire []byte) (int, bool, string) {
			vals := make([]*tabletext.Instance, 256)
			for i := range vals {
				vals[i] = model.New(root)
			}
			var rep tabletext.Report
			n, ok, err := tablewire.DecodeMessages(model, vals, wire, &vocab, &rep)
			if err != nil || rep.Refused {
				ok = false
			}
			return n, ok, rep.Reason
		}
		wire, err := shapeMessageBatch(mb, sent, load)
		if err != nil {
			return fmt.Errorf("message-batch %s: %w", mb.Name, err)
		}
		n, ok, reason := load(wire)
		switch mb.Verdict {
		case "delivered":
			if !ok || n != 2 {
				return fmt.Errorf("message-batch %s: the row says delivered and the engine answered count=%d ok=%v %s", mb.Name, n, ok, reason)
			}
		case "refused":
			if ok || n != 0 {
				return fmt.Errorf("message-batch %s: the row says refused and the engine answered count=%d ok=%v %s", mb.Name, n, ok, reason)
			}
			// A FIRST BODY THAT READS: the row is about a refusal met after
			// one. The first body's bits are the one-body batch's, so that
			// batch's bytes before its pad must be this wire's own.
			one, err := tablewire.EncodeMessages(model, bodies[:1])
			if err != nil {
				return fmt.Errorf("message-batch %s: %w", mb.Name, err)
			}
			if len(wire) < len(one)-1 || !bytes.Equal(wire[2:len(one)-1], one[2:len(one)-1]) {
				return fmt.Errorf("message-batch %s: the damage reaches the first body", mb.Name)
			}
		}
		if err := writeFileAtomic(mb.Wire(), wire); err != nil {
			return err
		}
	}
	return nil
}

// shapeMessageBatch derives the row's wire from the lawful two-body batch.
func shapeMessageBatch(mb MessageBatch, sent []byte, load func([]byte) (int, bool, string)) ([]byte, error) {
	switch mb.Shape {
	case "whole":
		return sent, nil
	case "cut":
		return sent[:len(sent)-1], nil
	case "byte-after-pad":
		return append(append([]byte{}, sent...), 0), nil
	case "out-of-range":
		// A RANGED VALUE PAST ITS MAXIMUM, IN THE SECOND BODY: the first bit
		// of the second half whose setting the engine refuses as out_of_range
		// and as nothing else. The search is over the wire and the verdict is
		// the engine's, so the row needs no map of the body's bit layout.
		for bit := (len(sent) / 2) * 8; bit < len(sent)*8; bit++ {
			if sent[bit/8]&(1<<(bit%8)) != 0 {
				continue
			}
			wire := append([]byte{}, sent...)
			wire[bit/8] |= 1 << (bit % 8)
			if n, ok, reason := load(wire); !ok && n == 0 && strings.HasPrefix(reason, tablewire.ReasonOutOfRange) {
				return wire, nil
			}
		}
		return nil, errors.New("no single bit of the second body carries a ranged value past its maximum")
	}
	return nil, fmt.Errorf("%q is not a batch shape", mb.Shape)
}
