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
	// NoText marks an instance the corpus carries on the WIRE and not yet as
	// TEXT (docs/SPEC-TABLES.md §16.2): the variable class has no text form in
	// any implementation, the tool refuses a variable root in both directions,
	// and a JSON golden nothing can write is a golden nothing holds. The
	// marker is the corpus saying which half it owes, and schema#275 removes
	// it rather than the harness quietly tolerating a missing file.
	NoText bool
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

// Hostile is one tree of the hostile-value battery (docs/SPEC-TABLES.md §16.2,
// §16.3, §17.5) and the verdict every implementation owes it: `refused`, or the
// §4 report the read produces. Tree is the directory `schema pack` reads, so
// the text is <Tree>/<Root>.json (§17).
type Hostile struct {
	Name    string
	Unit    string
	Root    string
	Tree    string
	Refused bool
	Counts  Counts
}

// Verdict is the answer a driver writes for one hostile case.
func (h Hostile) Verdict() string {
	if h.Refused {
		return "refused"
	}
	return h.Counts.String()
}

// Cook is a cooked file (docs/SPEC-TABLES.md §7) and the canonical node dump its
// Open must produce. The CASE and the ROOT are two columns because one root can
// have more than one fixture: the valued chain and the value-initialised one
// are the same table read two ways.
type Cook struct {
	Case string
	Root string
	Unit string
	Dump string
	File string // filled in by the harness once test/cookgen has written it
}

// CookWrite is one instance a runtime must WRITE as a cooked file
// (docs/SPEC-TABLES.md §7.6), and the two files `schema cook` produces from the
// same wire — one per byte order. The TOOL is the reference: a runtime's bytes
// are byte-compared against these, and a cook is content-addressed by (asset
// hash, build version), so two writers of one instance produce ONE artifact or
// the pair means nothing.
//
// It names an INSTANCE rather than repeating its unit, root and wire: the
// instance line already carries those, and a second copy of a path is a second
// place for it to drift.
type CookWrite struct {
	Instance string
	Little   string
	Big      string
}

// Block is a block image (docs/SPEC-TABLES.md §19) an Open must accept, and the
// canonical ROW DUMP its reader must produce out of it (§19.2). Open alone says
// a reader checked the prologue; the dump is the value-for-value read.
type Block struct {
	Name string
	Unit string
	File string
	Dump string
}

// Patch is one word a forgery writes: little-endian, `width` bytes at `offset`.
type Patch struct {
	Offset int64
	Width  int
	Value  uint64
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
	// Pointer is what the caller's BUFFER looks like, which is not a fact a
	// file can hold: 0 is an aligned base, 1..63 is a base that many bytes past
	// one, and -1 is a NULL buffer. The cook battery's unaligned-base arm is
	// the whole reason the column exists.
	Pointer int
	// Patches are the words this forgery writes, in order. Most rows carry one
	// and some carry none — a truncation damages nothing and claims less.
	Patches []Patch
	// Extent is the length the CALLER claims, which a forgery may set past the
	// bytes the image carries or short of them — a fact a file alone cannot
	// hold. -1 is "the file's own length".
	Extent  int64
	Verdict string // "refuse" or "open"
	Label   string
	File    string // filled in by the harness once materialised
}

// parsePatches reads the three PARALLEL columns a forgery's patch is spelled
// as: comma-separated offsets, widths and values of equal length. One word is
// the ordinary case and reads as one number in each column; an offset of -1 is
// a forgery that damages nothing, which is what a truncation is.
func parsePatches(offsets, widths, values string) ([]Patch, error) {
	off := strings.Split(offsets, ",")
	wid := strings.Split(widths, ",")
	val := strings.Split(values, ",")
	if len(off) != len(wid) || len(off) != len(val) {
		return nil, fmt.Errorf("a patch's offset, width and value columns name %d, %d and %d words",
			len(off), len(wid), len(val))
	}
	var out []Patch
	for i := range off {
		o, err := strconv.ParseInt(off[i], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an offset", off[i])
		}
		w, err := strconv.Atoi(wid[i])
		if err != nil {
			return nil, fmt.Errorf("%q is not a width", wid[i])
		}
		v, err := strconv.ParseUint(val[i], 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a value", val[i])
		}
		if o < 0 {
			continue // no patch: the forgery is the extent or the pointer alone
		}
		if w < 1 || w > 8 {
			return nil, fmt.Errorf("%d is not a word width", w)
		}
		out = append(out, Patch{Offset: o, Width: w, Value: v})
	}
	return out, nil
}

// Manifest is the whole registry.
type Manifest struct {
	Units      []Unit
	Instances  []Instance
	Reports    []ReportCase
	Hostiles   []Hostile
	Cooks      []Cook
	CookWrites []CookWrite
	Blocks     []Block
	Forgeries  []Forgery
}

// LookupInstance returns the instance a name calls out, which is how a
// `cook-write` line reaches the unit, the root and the wire it is written from
// without repeating any of them.
func (m *Manifest) LookupInstance(name string) (Instance, error) {
	for _, i := range m.Instances {
		if i.Name == name {
			return i, nil
		}
	}
	return Instance{}, fmt.Errorf("the manifest names no instance %q", name)
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
			if len(f) != 5 && len(f) != 6 {
				return nil, fmt.Errorf("%s: instance takes name, unit, root, wire, and optionally no-text", where)
			}
			inst := Instance{
				Name: f[1], Unit: f[2], Root: f[3], Wire: f[4],
				JSON: jsonDir + "/" + f[1] + ".json",
			}
			if len(f) == 6 {
				if f[5] != "no-text" {
					return nil, fmt.Errorf("%s: %q is not an instance marker; the only one is no-text", where, f[5])
				}
				inst.NoText = true
			}
			m.Instances = append(m.Instances, inst)
		case "report":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: report takes case, unit, root, wire", where)
			}
			m.Reports = append(m.Reports, ReportCase{Name: f[1], Unit: f[2], Root: f[3], Wire: f[4]})
		case "json-hostile":
			if len(f) != 6 {
				return nil, fmt.Errorf("%s: json-hostile takes case, unit, root, tree, verdict", where)
			}
			h := Hostile{Name: f[1], Unit: f[2], Root: f[3], Tree: f[4]}
			if f[5] == "refused" {
				h.Refused = true
			} else {
				c, err := ParseCounts(f[5])
				if err != nil {
					return nil, fmt.Errorf("%s: %w", where, err)
				}
				h.Counts = c
			}
			m.Hostiles = append(m.Hostiles, h)
		case "cook":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: cook takes case, unit, root, dump", where)
			}
			m.Cooks = append(m.Cooks, Cook{Case: f[1], Unit: f[2], Root: f[3], Dump: f[4]})
		case "cook-write":
			if len(f) != 4 {
				return nil, fmt.Errorf("%s: cook-write takes an instance and its two cooked files, little then big", where)
			}
			m.CookWrites = append(m.CookWrites, CookWrite{Instance: f[1], Little: f[2], Big: f[3]})
		case "block":
			if len(f) != 5 {
				return nil, fmt.Errorf("%s: block takes name, unit, file, dump", where)
			}
			m.Blocks = append(m.Blocks, Block{Name: f[1], Unit: f[2], File: f[3], Dump: f[4]})
		case "forgery":
			// forgery <name> <kind> <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label...>
			if len(f) < 12 {
				return nil, fmt.Errorf("%s: forgery takes name, kind, subject, base, pointer, offset, width, value, extent, verdict, label", where)
			}
			pointer := 0
			if f[5] == "null" {
				pointer = -1
			} else {
				n, err := strconv.Atoi(f[5])
				if err != nil || n < 0 {
					return nil, fmt.Errorf("%s: %q is not a lead, and is not null", where, f[5])
				}
				pointer = n
			}
			patches, err := parsePatches(f[6], f[7], f[8])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			extent, err := strconv.ParseInt(f[9], 0, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an extent", where, f[9])
			}
			if f[10] != "refuse" && f[10] != "open" {
				return nil, fmt.Errorf("%s: %q is not a verdict", where, f[10])
			}
			m.Forgeries = append(m.Forgeries, Forgery{
				Name: f[1], Kind: f[2], Subject: f[3], Base: f[4],
				Pointer: pointer, Patches: patches, Extent: extent,
				Verdict: f[10], Label: strings.Join(f[11:], " "),
			})
		default:
			return nil, fmt.Errorf("%s: %q is not a manifest line kind", where, f[0])
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	// a cook-write line REACHES an instance, and a name that reaches nothing is
	// a case that would silently expect nothing
	for _, cw := range m.CookWrites {
		if _, err := m.LookupInstance(cw.Instance); err != nil {
			return nil, fmt.Errorf("%s: cook-write %s: %w", path, cw.Instance, err)
		}
	}
	return m, nil
}
