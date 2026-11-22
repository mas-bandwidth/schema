// THE 2026-11-22 SURFACES (test/conformance/README.md, "The 2026-11-22
// surfaces ask what no surface asked before"; docs/SPEC-CONFORMANCE.md §2).
//
//   - `announce`: the unit's announcement writer (FL-38), a strict or lenient
//     announcement read (FL-36), and a SaveMessages with no announcement in
//     hand (FL-18).
//   - `read-into`: one report and one set of caller values through a lawful,
//     a refused and the lawful read again (FL-02, FL-03, FL-29); `fixed-nocap`
//     (FL-17).
//   - `json-write-into`: the text writer into a caller buffer longer than the
//     text (FL-14, FL-15).
//   - `write-refusal-block`: ABSENT in Go (blockBuilderAbsent below).
//
// Every answer is what the generated runtime did; the driver computes none.
package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"announce4096"
	"announce4097"
	"backenddemo"
	"messagedemo"
	tblsurf "surfaces"
)

// blockBuilderAbsent is why the Go leg answers `write-refusal-block` ABSENT:
// the Go block fill path is <Root>BlockBegin(block, storage, <Root>Counts,
// refusal), which is handed the root's COUNTS alone (empty for a root with no
// counted array) and never the value; the rows are then the caller's own
// writes into the projection, with no call that checks them. There is no Go
// call that builds a root's block FROM A VALUE, so there is no runtime answer
// to report for a used length or a fixed array's count past its maximum.
const blockBuilderAbsent = "write-refusal-block: the Go runtime has no block build from a value " +
	"(<Root>BlockBegin takes <Root>Counts only; rows are raw projection writes)"

// ---------------------------------------------------------------------------
// announce
// ---------------------------------------------------------------------------

type announcer struct {
	measure func() int64
	write   func([]byte) int64
	// read is a FRESH vocabulary of the unit, its Strict flag set or left at
	// its default, reading one announcement
	read func(strict bool, data []byte) bool
}

func announcerOf[V any, R any, E any](measure func() int64, write func([]byte) int64,
	read func(*V, []byte, *R) bool, here int, init func(*V, []E), strict func(*V, bool)) announcer {
	return announcer{measure: measure, write: write, read: func(on bool, data []byte) bool {
		v := new(V)
		init(v, make([]E, max(4096, here)))
		strict(v, on)
		var r R
		return read(v, data, &r)
	}}
}

var announcers = map[string]announcer{
	"tblann4096": announcerOf(announce4096.AnnounceMeasure, announce4096.Announce, announce4096.AnnounceRead, announce4096.TableMessageEntriesHere,
		(*announce4096.TableVocabulary).Init, func(v *announce4096.TableVocabulary, on bool) { v.Strict = on }),
	"tblann4097": announcerOf(announce4097.AnnounceMeasure, announce4097.Announce, announce4097.AnnounceRead, announce4097.TableMessageEntriesHere,
		(*announce4097.TableVocabulary).Init, func(v *announce4097.TableVocabulary, on bool) { v.Strict = on }),
	"tblsurf": announcerOf(tblsurf.AnnounceMeasure, tblsurf.Announce, tblsurf.AnnounceRead, tblsurf.TableMessageEntriesHere,
		(*tblsurf.TableVocabulary).Init, func(v *tblsurf.TableVocabulary, on bool) { v.Strict = on }),
	"messagedemo": announcerOf(messagedemo.AnnounceMeasure, messagedemo.Announce, messagedemo.AnnounceRead, messagedemo.TableMessageEntriesHere,
		(*messagedemo.TableVocabulary).Init, func(v *messagedemo.TableVocabulary, on bool) { v.Strict = on }),
}

// surfaceAnnounce answers `announce <case> <unit> <root> <mode> <input>`.
func surfaceAnnounce(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "announce" {
			continue
		}
		if len(f) != 6 {
			return fmt.Errorf("announce line has %d fields, wants 6: %v", len(f), f)
		}
		name, unit, root, mode, input := f[1], f[2], f[3], f[4], f[5]
		var answer []byte
		switch mode {
		case "write":
			a, ok := announcers[unit]
			if !ok {
				return fmt.Errorf("%s: no announcer for unit %q", name, unit)
			}
			answer = []byte("refused\n")
			if n := a.measure(); n >= 0 {
				buffer := make([]byte, n)
				if got := a.write(buffer); got == n {
					answer = buffer
				} else if got >= 0 {
					return fmt.Errorf("%s: Announce wrote %d bytes, AnnounceMeasure said %d", name, got, n)
				}
			}
		case "strict", "lenient":
			a, ok := announcers[unit]
			if !ok {
				return fmt.Errorf("%s: no announcer for unit %q", name, unit)
			}
			data, err := os.ReadFile(input)
			if err != nil {
				return err
			}
			answer = []byte("refused\n")
			if a.read(mode == "strict", data) {
				answer = []byte("read\n")
			}
		case "send":
			c := findCodec(unit, root)
			mc := findMessageCodec(unit, root)
			if c == nil || mc == nil || mc.saveBytes == nil {
				return fmt.Errorf("%s: no codec and message codec for %s.%s", name, unit, root)
			}
			value := c.fresh()
			if input != "-" {
				wire, err := os.ReadFile(input)
				if err != nil {
					return err
				}
				var rep report
				if !c.load(value, wire, &rep) {
					return fmt.Errorf("%s: load refused %s (%v)", name, input, rep.err)
				}
			}
			// NO announcement is read or written on this connection: the
			// batch is written under the sender's own vocabulary
			answer = []byte("refused\n")
			if saved, ok := mc.saveBytes(value); ok {
				answer = saved
			}
		default:
			return fmt.Errorf("%s: %q is not an announce mode", name, mode)
		}
		if err := spill(out, name, answer); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// read-into
// ---------------------------------------------------------------------------

// readStep is one read's line: `read` when the call returned success and its
// report holds no refusal, `refused` when it returned its refusal and the
// report holds it, `threw` when the call raised. A call whose return and
// report disagree answers a line of its own, which no golden holds.
func readStep(call func() (ok, refused bool)) (step string) {
	defer func() {
		if r := recover(); r != nil {
			step = "threw"
		}
	}()
	ok, refused := call()
	switch {
	case ok && !refused:
		return "read"
	case !ok && refused:
		return "refused"
	case ok:
		return "read-with-refused-report"
	}
	return "refused-with-clean-report"
}

// readInto runs the three reads of a row over ONE set of values and ONE
// report: read is the call, text the ToJson of every value it was handed.
func readInto(lawful, damaged []byte, read func([]byte) (bool, bool), text func() ([]byte, error)) ([]byte, error) {
	answer := []byte(readStep(func() (bool, bool) { return read(lawful) }) + "\n")
	answer = append(answer, readStep(func() (bool, bool) { return read(damaged) })+"\n"...)
	values, err := text()
	if err != nil {
		return nil, err
	}
	answer = append(answer, values...)
	return append(answer, readStep(func() (bool, bool) { return read(lawful) })+"\n"...), nil
}

// jsonOf is one value's ToJson text, measured then written.
func jsonOf(measure func() int64, write func([]byte) int64, what string) ([]byte, error) {
	n := measure()
	if n < 0 {
		return nil, fmt.Errorf("%s: ToJSONMeasure refused the value", what)
	}
	buffer := make([]byte, n)
	if write(buffer) != n {
		return nil, fmt.Errorf("%s: ToJSON did not write ToJSONMeasure's answer", what)
	}
	return buffer, nil
}

type readIntoKey struct{ unit, root, form string }

// readIntoRows is each row's read through the generated runtime. The Go
// variable form's read into the caller's storage is the BUILDER read
// (<Root>LoadBuilder): the region read (<Root>Load) hands back no root on a
// refusal (SPEC-TABLES §6.5), so it has no caller value to print.
var readIntoRows = map[readIntoKey]func(lawful, damaged []byte, count int, announcement []byte) ([]byte, error){
	{"tblsurf", "Counted", "variable"}: func(lawful, damaged []byte, _ int, _ []byte) ([]byte, error) {
		var b tblsurf.CountedBuilder
		if !b.Init() {
			return nil, fmt.Errorf("CountedBuilder.Init failed")
		}
		defer b.Shutdown()
		var r tblsurf.TableReport
		return readInto(lawful, damaged, func(wire []byte) (bool, bool) {
			ok := tblsurf.CountedLoadBuilder(&b, wire, &r)
			return ok, r.Refused()
		}, func() ([]byte, error) {
			root := b.GetRoot()
			return jsonOf(func() int64 { return tblsurf.CountedToJSONMeasure(root) },
				func(buf []byte) int64 { return tblsurf.CountedToJSON(root, buf) }, "Counted")
		})
	},
	{"tblsurf", "Counted", "json"}: func(lawful, damaged []byte, _ int, _ []byte) ([]byte, error) {
		var b tblsurf.CountedBuilder
		if !b.Init() {
			return nil, fmt.Errorf("CountedBuilder.Init failed")
		}
		defer b.Shutdown()
		var r tblsurf.TableReport
		return readInto(lawful, damaged, func(text []byte) (bool, bool) {
			ok := tblsurf.CountedFromJSON(&b, text, &r)
			return ok, r.Refused()
		}, func() ([]byte, error) {
			root := b.GetRoot()
			return jsonOf(func() int64 { return tblsurf.CountedToJSONMeasure(root) },
				func(buf []byte) int64 { return tblsurf.CountedToJSON(root, buf) }, "Counted")
		})
	},
	{"tblsurf", "Narrow", "fixed"}: func(lawful, damaged []byte, count int, _ []byte) ([]byte, error) {
		values := make([]tblsurf.Narrow, count)
		for i := range values {
			tblsurf.NarrowReset(&values[i])
		}
		plan := make([]tblsurf.TableFixedEntry, fixedPlanCapacity)
		var r tblsurf.TableReport
		return readInto(lawful, damaged, func(data []byte) (bool, bool) {
			n := tblsurf.NarrowFixedLoad(values, data, plan, &r)
			return n >= 0, r.Refused()
		}, func() ([]byte, error) {
			var all []byte
			for i := range values {
				v := &values[i]
				text, err := jsonOf(func() int64 { return tblsurf.NarrowToJSONMeasure(v) },
					func(buf []byte) int64 { return tblsurf.NarrowToJSON(v, buf) }, "Narrow")
				if err != nil {
					return nil, err
				}
				all = append(all, text...)
			}
			return all, nil
		})
	},
	// FL-17 has no call to hold in Go: <Root>FixedLoad is handed the caller's
	// slice, and its length IS the capacity; no form of the call omits it
	{"tblsurf", "Narrow", "fixed-nocap"}: func([]byte, []byte, int, []byte) ([]byte, error) {
		return []byte("unrepresentable\n"), nil
	},
	{"backenddemo", "StorePurchase", "message"}: func(lawful, damaged []byte, count int, announcement []byte) ([]byte, error) {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, max(4096, backenddemo.TableMessageEntriesHere)))
		var ar backenddemo.TableReport
		if !backenddemo.AnnounceRead(v, announcement, &ar) {
			return nil, fmt.Errorf("the connection's announcement did not read (%v)", ar.Err())
		}
		values := make([]backenddemo.StorePurchase, count)
		for i := range values {
			backenddemo.StorePurchaseReset(&values[i])
		}
		var r backenddemo.TableReport
		return readInto(lawful, damaged, func(data []byte) (bool, bool) {
			_, ok := backenddemo.StorePurchaseLoadMessages(values, v, data, &r)
			return ok, r.Refused()
		}, func() ([]byte, error) {
			var all []byte
			for i := range values {
				p := &values[i]
				text, err := jsonOf(func() int64 { return backenddemo.StorePurchaseToJSONMeasure(p) },
					func(buf []byte) int64 { return backenddemo.StorePurchaseToJSON(p, buf) }, "StorePurchase")
				if err != nil {
					return nil, err
				}
				all = append(all, text...)
			}
			return all, nil
		})
	},
}

// surfaceReadInto answers `read-into <case> <unit> <root> <form> <lawful>
// <damaged> <count or -> <announcement or ->`.
func surfaceReadInto(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "read-into" {
			continue
		}
		if len(f) != 9 {
			return fmt.Errorf("read-into line has %d fields, wants 9: %v", len(f), f)
		}
		name, unit, root, form := f[1], f[2], f[3], f[4]
		run, ok := readIntoRows[readIntoKey{unit, root, form}]
		if !ok {
			return fmt.Errorf("%s: the driver binds no read-into for %s.%s (%s)", name, unit, root, form)
		}
		lawful, err := os.ReadFile(f[5])
		if err != nil {
			return err
		}
		var damaged, announcement []byte
		if f[6] != "-" {
			if damaged, err = os.ReadFile(f[6]); err != nil {
				return err
			}
		}
		count := 0
		if f[7] != "-" {
			if count, err = strconv.Atoi(f[7]); err != nil || count < 1 {
				return fmt.Errorf("%s: %q is not a count of values", name, f[7])
			}
		}
		if f[8] != "-" {
			if announcement, err = os.ReadFile(f[8]); err != nil {
				return err
			}
		}
		answer, err := run(lawful, damaged, count, announcement)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := spill(out, name, answer); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// json-write-into
// ---------------------------------------------------------------------------

// surfaceJSONWriteInto answers `json-write-into <case> <unit> <root> <wire>`.
//
// GO HAS NO PROCESS LOCALE: no standard-library call sets one, and strconv
// and the generated text writer never consult the C locale, so there is no
// setlocale to make. The driver still requires the harness to have named the
// comma-decimal locale (SCHEMA_COMMA_LOCALE), so a run that lost it is loud,
// and runs the rest: the caller buffer 64 bytes past the measure, filled with
// 0xA5, every byte past the returned length checked untouched (FL-15).
func surfaceJSONWriteInto(lines []line, out string) error {
	if os.Getenv("SCHEMA_COMMA_LOCALE") == "" {
		return fmt.Errorf("json-write-into: the harness named no SCHEMA_COMMA_LOCALE")
	}
	for _, f := range lines {
		if f[0] != "json-write-into" {
			continue
		}
		if len(f) != 5 {
			return fmt.Errorf("json-write-into line has %d fields, wants 5: %v", len(f), f)
		}
		name := f[1]
		c := findCodec(f[2], f[3])
		if c == nil || c.toJson == nil {
			return fmt.Errorf("%s: no text writer for %s.%s", name, f[2], f[3])
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !c.load(value, wire, &rep) {
			return fmt.Errorf("%s: load refused %s (%v)", name, f[4], rep.err)
		}
		size := c.toJsonMeasure(value)
		if size < 0 {
			return fmt.Errorf("%s: ToJSONMeasure refused the value", name)
		}
		buffer := bytes.Repeat([]byte{0xa5}, int(size)+64)
		n := c.toJson(value, buffer)
		if n < 0 || n > int64(len(buffer)) {
			return fmt.Errorf("%s: ToJSON into a buffer of %d answered %d (measure %d)", name, len(buffer), n, size)
		}
		answer := buffer[:n]
		for _, b := range buffer[n:] {
			if b != 0xa5 {
				answer = []byte("tail-written\n")
				break
			}
		}
		if err := spill(out, name, answer); err != nil {
			return err
		}
	}
	return nil
}
