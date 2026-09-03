// The conformance manifest reader (testdata/conformance/tables/MANIFEST.txt).
//
// One line-oriented format, read here by the harness and by every language
// driver. The whole point of it is that nothing in the data names a language:
// a port implements test/conformance/README.md's contract against this file
// and the harness is the gate.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Unit is a compilation unit by the key the other lines use.
type Unit struct {
	Key   string
	Paths []string
}

// Instance is one table instance in two forms that must agree: the wire bytes
// at Wire (docs/SPEC-TABLES.md §3) and the JSON text at JSON (§16).
type Instance struct {
	Name string
	Unit string
	Root string
	Wire string
	JSON string
}

// Counts is a read report (docs/SPEC-TABLES.md §4), which is the whole expectation
// a report case carries.
type Counts struct {
	Unknown      int
	KindMismatch int
	Clamped      int
	Duplicate    int
	Malformed    bool
}

func (c Counts) String() string {
	return fmt.Sprintf("%d,%d,%d,%d,%t", c.Unknown, c.KindMismatch, c.Clamped, c.Duplicate, c.Malformed)
}

// ParseCounts reads the "u,k,c,d,m" spelling every leg of the harness uses.
func ParseCounts(text string) (Counts, error) {
	var c Counts
	parts := strings.Split(strings.TrimSpace(text), ",")
	if len(parts) != 5 {
		return c, fmt.Errorf("a report is five fields, not %d: %q", len(parts), text)
	}
	nums := []*int{&c.Unknown, &c.KindMismatch, &c.Clamped, &c.Duplicate}
	for i, p := range nums {
		v, err := strconv.Atoi(parts[i])
		if err != nil {
			return c, fmt.Errorf("%q is not a counter: %w", parts[i], err)
		}
		*p = v
	}
	switch parts[4] {
	case "true":
		c.Malformed = true
	case "false":
		c.Malformed = false
	default:
		return c, fmt.Errorf("%q is not true or false", parts[4])
	}
	return c, nil
}

// ReportCase is bytes read by a type that did not write them.
type ReportCase struct {
	Name string
	Unit string
	Root string
	Wire string
}

// Cook is a cooked file (docs/SPEC-TABLES.md §7) and the canonical node dump its
// Open must produce.
type Cook struct {
	Root string
	Unit string
	Dump string
	File string // filled in by the harness once test/cookgen has written it
}

// Block is a block image (docs/SPEC-TABLES.md §19) an Open must accept.
type Block struct {
	Name string
	Unit string
	File string
}

// Forgery is one damaged fixture and the verdict every implementation owes it.
// A forgery is carried as a PATCH over a base fixture rather than as a whole
// file: the patch is what a person can review, and the harness materialises the
// file so a driver only ever meets a path.
type Forgery struct {
	Name    string
	Kind    string // "cook" or "block"
	Subject string // the cook's root, or the block's name
	Base    string // the fixture key the patch is applied to
	Offset  int64
	Width   int
	Value   uint64
	// Extent is the length the CALLER claims, which a forgery may set past the
	// bytes the image carries — a fact a file alone cannot hold. -1 is "the
	// file's own length".
	Extent  int64
	Verdict string // "refuse" or "open"
	Label   string
	File    string // filled in by the harness once materialised
}

// Manifest is the whole registry.
type Manifest struct {
	Units     []Unit
	Instances []Instance
	Reports   []ReportCase
	Cooks     []Cook
	Blocks    []Block
	Forgeries []Forgery
}

// Unit returns the unit a key names.
func (m *Manifest) UnitPaths(key string) ([]string, error) {
	for _, u := range m.Units {
		if u.Key == key {
			return u.Paths, nil
		}
	}
	return nil, fmt.Errorf("the manifest names no unit %q", key)
}

// ReadManifest parses one manifest file. jsonDir is where an instance's text
// lives; it is a parameter rather than a constant because the harness writes a
// DERIVED manifest into the build tree, and a derived manifest points at the
// committed texts.
func ReadManifest(path, jsonDir string) (*Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	m := &Manifest{}
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for scan.Scan() {
		line++
		text := strings.TrimSpace(scan.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		f := strings.Fields(text)
		where := fmt.Sprintf("%s:%d", path, line)
		switch f[0] {
		case "unit":
			if len(f) < 3 {
				return nil, fmt.Errorf("%s: unit takes a key and at least one path", where)
			}
			m.Units = append(m.Units, Unit{Key: f[1], Paths: f[2:]})
		case "instance":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: instance takes name, unit, root, wire", where)
			}
			m.Instances = append(m.Instances, Instance{
				Name: f[1], Unit: f[2], Root: f[3], Wire: f[4],
				JSON: jsonDir + "/" + f[1] + ".json",
			})
		case "report":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: report takes case, unit, root, wire", where)
			}
			m.Reports = append(m.Reports, ReportCase{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4]})
		case "cook":
			if len(f) != 4 {
				return nil, fmt.Errorf("%s: cook takes root, unit, dump", where)
			}
			m.Cooks = append(m.Cooks, Cook{Root: f[1], Unit: f[2], Dump: f[3]})
		case "block":
			if len(f) != 4 {
				return nil, fmt.Errorf("%s: block takes name, unit, file", where)
			}
			m.Blocks = append(m.Blocks, Block{Name: f[1], Unit: f[2], File: f[3]})
		case "forgery":
			// forgery <name> <kind> <subject> <base> <offset> <width> <value> <extent> <verdict> <label...>
			if len(f) < 11 {
				return nil, fmt.Errorf("%s: forgery takes name, kind, subject, base, offset, width, value, extent, verdict, label", where)
			}
			offset, err := strconv.ParseInt(f[5], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an offset", where, f[5])
			}
			width, err := strconv.Atoi(f[6])
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a width", where, f[6])
			}
			value, err := strconv.ParseUint(f[7], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a value", where, f[7])
			}
			extent, err := strconv.ParseInt(f[8], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an extent", where, f[8])
			}
			if f[9] != "refuse" && f[9] != "open" {
				return nil, fmt.Errorf("%s: %q is not a verdict", where, f[9])
			}
			m.Forgeries = append(m.Forgeries, Forgery{
				Name: f[1], Kind: f[2], Subject: f[3], Base: f[4],
				Offset: offset, Width: width, Value: value, Extent: extent,
				Verdict: f[9], Label: strings.Join(f[10:], " "),
			})
		default:
			return nil, fmt.Errorf("%s: %q is not a manifest line kind", where, f[0])
		}
	}
	return m, scan.Err()
}
