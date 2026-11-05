package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"backenddemo"
	"tabledemo"
	widedemo "widedemo"
)

// ---------------------------------------------------------------------------
// the write-refusal surfaces through the other writers
// (testdata/conformance/tables/FORMAT.md): write-refusal-fixed,
// write-refusal-message and write-refusal-json
// ---------------------------------------------------------------------------
//
// Each row loads its wire with the ordinary Load, sets the one field exactly as
// the write-refusal surface does (wrefMutate), and writes the one value through
// a different writer: FixedSave as a batch of one, SaveMessages as a batch of
// one, or the text writer. Every Go writer refuses in every build: -1 with
// invalid_value from the ...Reason twin or the report.

// wrefBytes parses a `bytes` or `units` value: hex, two digits a byte.
func wrefBytes(text string) ([]byte, error) {
	b, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("value %q is not hex", text)
	}
	return b, nil
}

// wrefUnits parses a `units` value: four hex digits a UTF-16 code unit.
func wrefUnits(text string) ([]uint16, error) {
	b, err := wrefBytes(text)
	if err != nil || len(b)%2 != 0 {
		return nil, fmt.Errorf("value %q is not four hex digits a unit", text)
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return units, nil
}

// wrefSetText sets a narrow text's exact bytes; false when the storage cannot
// hold that many.
func wrefSetText(store []byte, length *int32, text string) (bool, error) {
	b, err := wrefBytes(text)
	if err != nil {
		return false, err
	}
	if len(b) > len(store) {
		return false, nil
	}
	clear(store)
	copy(store, b)
	*length = int32(len(b))
	return true, nil
}

// wrefLoad loads a manifest wire as unit.root through the leg's ordinary Load.
func wrefLoad(name, unit, root, wirePath string) (any, error) {
	c := findCodec(unit, root)
	if c == nil {
		return nil, fmt.Errorf("%s: no codec for %s %s", name, unit, root)
	}
	wire, err := os.ReadFile(wirePath)
	if err != nil {
		return nil, err
	}
	value := c.fresh()
	var rep report
	if !c.load(value, wire, &rep) {
		return nil, fmt.Errorf("%s: load refused %s", name, wirePath)
	}
	return value, nil
}

// wrefAnswer spills `refused` or `wrote`, holding a refusal to invalid_value.
func wrefAnswer(out, name string, n int64, reason error) error {
	if n >= 0 {
		return spill(out, name, []byte("wrote\n"))
	}
	if !errors.Is(reason, tabledemo.TableRefuseInvalidValue) && !errors.Is(reason, widedemo.TableRefuseInvalidValue) {
		return fmt.Errorf("%s: refused with %v, wants invalid_value", name, reason)
	}
	return spill(out, name, []byte("refused\n"))
}

// surfaceWriteRefusalFixed saves the one value through <Root>FixedSave. A root
// this leg emits no fixed form for answers `<case>.absent` with the reason: in
// a regional unit a fixed table whose closure reaches a union keeps form 1
// (docs/SPEC-TABLES.md §3.4, the named follow-on), which is RootConfig and
// LoadoutConfig here, both through WeaponConfig's Effect.
func surfaceWriteRefusalFixed(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "write-refusal-fixed" {
			continue
		}
		if len(f) < 8 {
			return fmt.Errorf("write-refusal-fixed line has %d fields, wants 8", len(f))
		}
		name, unit, root, wirePath, path, kind, text := f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		if unit == "tabledemo" {
			if err := spillAbsentReason(out, name, "regional unit: a fixed table whose closure reaches a union keeps form 1 on the Go leg (SPEC-TABLES §3.4)"); err != nil {
				return err
			}
			continue
		}
		value, err := wrefLoad(name, unit, root, wirePath)
		if err != nil {
			return err
		}
		held, err := wrefMutate(value, path, kind, text)
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		if !held {
			if err := spill(out, name, []byte("unrepresentable\n")); err != nil {
				return err
			}
			continue
		}
		var n int64
		var reason error
		switch v := value.(type) {
		case *widedemo.Caption:
			batch := []widedemo.Caption{*v}
			buffer := make([]byte, widedemo.CaptionFixedMeasure(1))
			n, reason = widedemo.CaptionFixedSaveReason(batch, buffer)
			if n < 0 && !bytes.Equal(buffer, make([]byte, len(buffer))) {
				return fmt.Errorf("%s: the refused batch wrote into the buffer", name)
			}
		default:
			return fmt.Errorf("%s: no FixedSave for %T", name, value)
		}
		if err := wrefAnswer(out, name, n, reason); err != nil {
			return err
		}
	}
	return nil
}

// surfaceWriteRefusalMessage saves the one value through <Root>SaveMessages.
// The <unit> column is a connection key: its announcement is read into the
// vocabulary the message surface uses, and the batch of one is written against
// it. A refusal is -1 from MeasureMessages and SaveMessages alike, with the
// report's reason invalid_value, and nothing written.
func surfaceWriteRefusalMessage(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "write-refusal-message" {
			continue
		}
		if len(f) < 8 {
			return fmt.Errorf("write-refusal-message line has %d fields, wants 8", len(f))
		}
		name, conn, root, wirePath, path, kind, text := f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		var unit, announcementPath string
		for _, c := range lines {
			if c[0] == "connection" && c[1] == conn {
				unit, announcementPath = c[2], c[4]
				break
			}
		}
		if unit != "backenddemo" {
			return fmt.Errorf("%s: no message writer bound for connection %s (unit %q)", name, conn, unit)
		}
		announcement, err := os.ReadFile(announcementPath)
		if err != nil {
			return err
		}
		vocabulary := new(backenddemo.TableVocabulary)
		vocabulary.Init(make([]backenddemo.TableMessageEntry, backenddemo.TableMessageEntriesHere))
		var announced backenddemo.TableReport
		if !backenddemo.AnnounceRead(vocabulary, announcement, &announced) {
			return fmt.Errorf("%s: the announcement was refused: %s", name, announcementPath)
		}
		value, err := wrefLoad(name, unit, root, wirePath)
		if err != nil {
			return err
		}
		held, err := wrefMutate(value, path, kind, text)
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		if !held {
			if err := spill(out, name, []byte("unrepresentable\n")); err != nil {
				return err
			}
			continue
		}
		var measured, saved int64
		var measureReport, saveReport backenddemo.TableReport
		buffer := make([]byte, 64*1024)
		switch v := value.(type) {
		case *backenddemo.StorePurchase:
			measured = backenddemo.StorePurchaseMeasureMessages([]*backenddemo.StorePurchase{v}, &measureReport)
			saved = backenddemo.StorePurchaseSaveMessages([]*backenddemo.StorePurchase{v}, buffer, &saveReport)
		case *backenddemo.MatchResult:
			measured = backenddemo.MatchResultMeasureMessages([]*backenddemo.MatchResult{v}, &measureReport)
			saved = backenddemo.MatchResultSaveMessages([]*backenddemo.MatchResult{v}, buffer, &saveReport)
		default:
			return fmt.Errorf("%s: no SaveMessages for %T", name, value)
		}
		if (measured < 0) != (saved < 0) || saved >= 0 && saved != measured {
			return fmt.Errorf("%s: MeasureMessages answered %d and SaveMessages %d", name, measured, saved)
		}
		answer := "wrote\n"
		if saved < 0 {
			if measureReport.Reason != "invalid_value" || saveReport.Reason != "invalid_value" {
				return fmt.Errorf("%s: refused with %q and %q, wants invalid_value", name, measureReport.Reason, saveReport.Reason)
			}
			answer = "refused\n"
		}
		if err := spill(out, name, []byte(answer)); err != nil {
			return err
		}
	}
	return nil
}

// surfaceWriteRefusalJson writes the one value through the text writer, which
// spells a zero byte or unit `\u0000` and refuses only ill-formed text
// (docs/SPEC-TABLES.md §16.3): `wrote` when the text holds that escape.
func surfaceWriteRefusalJson(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "write-refusal-json" {
			continue
		}
		if len(f) < 8 {
			return fmt.Errorf("write-refusal-json line has %d fields, wants 8", len(f))
		}
		name, unit, root, wirePath, path, kind, text := f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		c := findCodec(unit, root)
		if c == nil || c.toJson == nil {
			return fmt.Errorf("%s: no text writer for %s %s", name, unit, root)
		}
		value, err := wrefLoad(name, unit, root, wirePath)
		if err != nil {
			return err
		}
		held, err := wrefMutate(value, path, kind, text)
		if err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		if !held {
			if err := spill(out, name, []byte("unrepresentable\n")); err != nil {
				return err
			}
			continue
		}
		size := c.toJsonMeasure(value)
		answer := "refused\n"
		if size >= 0 {
			buffer := make([]byte, size)
			if n := c.toJson(value, buffer); n != size {
				return fmt.Errorf("%s: the text writer measured %d and wrote %d", name, size, n)
			}
			if !bytes.Contains(buffer, []byte(`\u0000`)) {
				return fmt.Errorf("%s: the text holds no \\u0000: %s", name, buffer)
			}
			answer = "wrote\n"
		}
		if err := spill(out, name, []byte(answer)); err != nil {
			return err
		}
	}
	return nil
}
