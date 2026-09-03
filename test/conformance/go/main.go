// THE GO CONFORMANCE DRIVER (test/conformance/README.md).
//
// One process per surface. The harness hands it the derived manifest, the
// surface name and an output directory; the driver writes one file per case
// and says nothing else. Every expectation lives in the DATA — this file holds
// no literal instance, no expected byte and no expected count.
//
//	driver <manifest> list
//	driver <manifest> <surface> <outdir>
//
// Exit 0 means the surface ran. Exit 2 means this backend does not implement
// it, which the matrix prints as ABSENT rather than as a failure: a backend
// that has no text form is missing a feature, not failing a test, and the
// difference is the whole reason the matrix exists.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
// ---------------------------------------------------------------------------

type line []string

func readManifest(path string) ([]line, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []line
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		text := strings.TrimRight(scanner.Text(), "\r")
		if text == "" || text[0] == '#' {
			continue
		}
		fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '\t' })
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		out = append(out, fields)
	}
	return out, scanner.Err()
}

// ---------------------------------------------------------------------------
// the codec table: one row per (unit, root) the corpus names
// ---------------------------------------------------------------------------
//
// Every row is the SAME function values, so a surface is one loop over this
// table and not a switch per root. Each unit declares its OWN TableReport —
// the generated surface is per package — so the driver carries one report
// shape of its own and each row copies into it.

type report struct {
	unknown, kindMismatch, clamped, duplicate int32
	malformed                                 bool
}

type codec struct {
	unit string
	root string
	// reset returns the value to its declared defaults and hands back the
	// opaque handle the rest of the row takes
	fresh   func() any
	load    func(value any, wire []byte, rep *report) bool
	measure func(value any) int64
	save    func(value any, buffer []byte) int64
	// the TEXT form (docs/SPEC-TABLES.md §16); nil where a backend has none
	fromJson      func(value any, text []byte, rep *report) bool
	toJsonMeasure func(value any) int64
	toJson        func(value any, buffer []byte) int64
}

// row binds one generated table's surface into the erased shape above. One
// generic function stands in for the C++ driver's macro: the type parameters
// are what restore the type, and the storage is one instance per row so the
// driver allocates nothing per case either.
//
// R is the UNIT's own TableReport — the generated surface is per package, so
// there are as many report types as units — and `snap` narrows it to the one
// shape the harness's five counters need.
func row[T any, R any](
	unit, root string,
	reset func(*T),
	load func(*T, []byte, *R) bool,
	measure func(*T) int64,
	save func(*T, []byte) int64,
	fromJson func(*T, []byte, *R) bool,
	toJsonMeasure func(*T) int64,
	toJson func(*T, []byte) int64,
	snap func(*R) report,
) codec {
	var storage T
	return codec{
		unit: unit, root: root,
		fresh: func() any { reset(&storage); return &storage },
		load: func(value any, wire []byte, rep *report) bool {
			var inner R
			ok := load(value.(*T), wire, &inner)
			*rep = snap(&inner)
			return ok
		},
		measure: func(value any) int64 { return measure(value.(*T)) },
		save:    func(value any, buffer []byte) int64 { return save(value.(*T), buffer) },
		fromJson: func(value any, text []byte, rep *report) bool {
			var inner R
			ok := fromJson(value.(*T), text, &inner)
			*rep = snap(&inner)
			return ok
		},
		toJsonMeasure: func(value any) int64 { return toJsonMeasure(value.(*T)) },
		toJson:        func(value any, buffer []byte) int64 { return toJson(value.(*T), buffer) },
	}
}

func findCodec(unit, root string) *codec {
	for i := range codecTable {
		if codecTable[i].unit == unit && codecTable[i].root == root {
			return &codecTable[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

func spill(dir, name string, data []byte) error {
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// ---------------------------------------------------------------------------
// the surfaces
// ---------------------------------------------------------------------------

var scratch []byte

func surfaceWire(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "instance" {
			continue
		}
		c := findCodec(f[2], f[3])
		if c == nil {
			return fmt.Errorf("no codec for %s.%s", f[2], f[3])
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !c.load(value, wire, &rep) {
			return fmt.Errorf("%s: load refused %s", f[1], f[4])
		}
		size := c.measure(value)
		if size < 0 {
			return fmt.Errorf("%s: measure refused the value", f[1])
		}
		if int64(cap(scratch)) < size {
			scratch = make([]byte, size)
		}
		scratch = scratch[:size]
		clear(scratch)
		if c.save(value, scratch) != size {
			return fmt.Errorf("%s: save did not write measure's answer", f[1])
		}
		if err := spill(out, f[1], scratch); err != nil {
			return err
		}
	}
	return nil
}

func surfaceReport(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "report" {
			continue
		}
		c := findCodec(f[2], f[3])
		if c == nil {
			return fmt.Errorf("no codec for %s.%s", f[2], f[3])
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		ok := c.load(value, wire, &rep)
		malformed := "false"
		if rep.malformed || !ok {
			malformed = "true"
		}
		text := fmt.Sprintf("%d,%d,%d,%d,%s\n", rep.unknown, rep.kindMismatch, rep.clamped, rep.duplicate, malformed)
		if err := spill(out, f[1], []byte(text)); err != nil {
			return err
		}
	}
	return nil
}

func surfaceJsonRead(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "instance" {
			continue
		}
		c := findCodec(f[2], f[3])
		if c == nil || c.fromJson == nil {
			return fmt.Errorf("no text form for %s.%s", f[2], f[3])
		}
		text, err := os.ReadFile("testdata/conformance/tables/json/" + f[1] + ".json")
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !c.fromJson(value, text, &rep) {
			return fmt.Errorf("%s: FromJson refused the text", f[1])
		}
		size := c.measure(value)
		if size < 0 {
			return fmt.Errorf("%s: measure refused the value", f[1])
		}
		buffer := make([]byte, size)
		if c.save(value, buffer) != size {
			return fmt.Errorf("%s: save did not write measure's answer", f[1])
		}
		if err := spill(out, f[1], buffer); err != nil {
			return err
		}
	}
	return nil
}

func surfaceJsonWrite(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "instance" {
			continue
		}
		c := findCodec(f[2], f[3])
		if c == nil || c.toJson == nil {
			return fmt.Errorf("no text form for %s.%s", f[2], f[3])
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		if !c.load(value, wire, &rep) {
			return fmt.Errorf("%s: load refused %s", f[1], f[4])
		}
		size := c.toJsonMeasure(value)
		if size < 0 {
			return fmt.Errorf("%s: ToJsonMeasure refused the value", f[1])
		}
		buffer := make([]byte, size)
		if c.toJson(value, buffer) != size {
			return fmt.Errorf("%s: ToJson did not write ToJsonMeasure's answer", f[1])
		}
		if err := spill(out, f[1]+".json", buffer); err != nil {
			return err
		}
	}
	return nil
}

func surfaceBlock(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "block" {
			continue
		}
		data, err := os.ReadFile(f[3])
		if err != nil {
			return err
		}
		verdict := "refuse\n"
		opened, err := openBlock(f[1], data, -1)
		if err != nil {
			return err
		}
		if opened {
			verdict = "open\n"
		}
		if err := spill(out, f[1], []byte(verdict)); err != nil {
			return err
		}
	}
	return nil
}

// surfaceForgery answers ONE of the two forgery batteries, selected by the
// KIND column. The two are one shape and two kinds so the matrix can say which
// reader a backend has (test/conformance/README.md).
//
// A derived line is `forgery <name> <kind> <subject> <file> <extent> <pointer>`.
// <extent> is the length the caller CLAIMS and <pointer> is the BUFFER it holds
// — `0` an aligned base, `1`..`63` that many bytes past one, `null` no buffer
// at all. Neither is a fact a file can carry, which is why both are columns.
func surfaceForgery(kind string, lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "forgery" || f[2] != kind {
			continue
		}
		data, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		extent, err := strconv.ParseInt(f[5], 0, 64)
		if err != nil {
			return err
		}
		lead, nilBuffer := 0, false
		if f[6] == "null" {
			nilBuffer = true
		} else if lead, err = strconv.Atoi(f[6]); err != nil {
			return err
		}
		var opened bool
		if kind == "block" {
			opened, err = openBlockForged(f[3], data, extent, lead, nilBuffer)
		} else {
			opened, err = openCookForged(f[3], data, extent, lead, nilBuffer)
		}
		if err != nil {
			return err
		}
		verdict := "refuse\n"
		if opened {
			verdict = "open\n"
		}
		if err := spill(out, f[1], []byte(verdict)); err != nil {
			return err
		}
	}
	return nil
}

// surfaceJsonHostile is the text form's hostile battery (§16.2, §16.3, §17.5):
// one tree per rule the page states, and the verdict is `refused` or the §4
// report the read produces. The tree is what `schema pack` reads, so the text
// is <tree>/<root>.json (§17).
func surfaceJsonHostile(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "json-hostile" {
			continue
		}
		c := findCodec(f[2], f[3])
		if c == nil || c.fromJson == nil {
			return fmt.Errorf("no text form for %s.%s", f[2], f[3])
		}
		text, err := os.ReadFile(f[4] + "/" + f[3] + ".json")
		if err != nil {
			return err
		}
		value := c.fresh()
		var rep report
		ok := c.fromJson(value, text, &rep)
		verdict := "refused\n"
		if ok && !rep.malformed {
			verdict = fmt.Sprintf("%d,%d,%d,%d,false\n", rep.unknown, rep.kindMismatch, rep.clamped, rep.duplicate)
		}
		if err := spill(out, f[1], []byte(verdict)); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <manifest> list\n       %s <manifest> <surface> <outdir>\n", os.Args[0], os.Args[0])
		os.Exit(2)
	}
	lines, err := readManifest(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "driver: %v\n", err)
		os.Exit(1)
	}
	surface := os.Args[2]
	if surface == "list" {
		for _, s := range surfaces() {
			fmt.Println(s)
		}
		return
	}
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: %s <manifest> <surface> <outdir>\n", os.Args[0])
		os.Exit(2)
	}
	out := os.Args[3]
	var run func([]line, string) error
	switch surface {
	case "wire":
		run = surfaceWire
	case "report":
		run = surfaceReport
	case "json-read":
		run = surfaceJsonRead
	case "json-write":
		run = surfaceJsonWrite
	case "cook":
		run = surfaceCook
	case "json-hostile":
		run = surfaceJsonHostile
	case "block":
		run = surfaceBlock
	case "block-dump":
		run = surfaceBlockDump
	case "forgery":
		run = func(lines []line, out string) error { return surfaceForgery("block", lines, out) }
	case "cook-forgery":
		run = func(lines []line, out string) error { return surfaceForgery("cook", lines, out) }
	default:
		os.Exit(2)
	}
	if err := run(lines, out); err != nil {
		fmt.Fprintf(os.Stderr, "driver: %s: %v\n", surface, err)
		os.Exit(1)
	}
}
