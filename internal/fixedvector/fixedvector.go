// Package fixedvector reads the fixed form's shared cross-version vectors out
// of the C++ reference's corpus manifest, so every leg asserts the answer the
// reference landed and none restates it by hand.
//
// A vector is a forged corpus file whose manifest line carries `expect=`: the
// values the reference's NEW reader landed from those bytes and the `clamped`
// count its report held (test/tables/fixedform_dump.cpp).
package fixedvector

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RangeWidenHostile is the file of the writer's-range vector
// (docs/FIXED-FORM-ALGORITHM.md §4.5): old_range_widen.bin with `v` forged to
// 150, outside the OLD writer's | 0..100 and inside the NEW reader's | 0..200.
// The NEW reader, VNEW_range_widen with VOLD_range_widen in its lineage, lands
// the WRITER's max 100 and counts `clamped` exactly once.
const RangeWidenHostile = "hostile_range_widen.bin"

// RangeWidenArrayHostile is the writer's-range vector for a ranged FIXED
// ARRAY: old_range_widen.bin (`vals [4]int32 | 0..100`) with `vals[2]` forged
// to 150, inside the NEW reader's | 0..200. Every element is held to the writer's
// range the same as a scalar, so the NEW reader lands 100 in that slot, the
// other three as written, and counts `clamped` exactly once.
const RangeWidenArrayHostile = "hostile_range_widen_array.bin"

// RangeWidenU64Hostile is the writer's-range vector for an UNSIGNED 64-bit
// field: old_range_widen.bin (`u uint64 | 10..1000`) with `u` forged to
// 2^63 + 7.
// The comparison is in the field's own type, unsigned, so the NEW reader
// (| 0..2000) lands the writer's MAX 1000 and counts `clamped` exactly once; a
// signed comparison reads a negative number and lands the min.
const RangeWidenU64Hostile = "hostile_range_widen_u64.bin"

// Vector names one writer's-range vector: its corpus file, a Name that tells
// it from the others (a leg's subtest), the versioning row whose pair it reads
// (VOLD_<Row> in the lineage of the reader VNEW_<Row>), and the fixed root
// both declare.
type Vector struct {
	File, Name, Row, Root string
}

// WriterRange is every writer's-range vector (docs/FIXED-FORM-ALGORITHM.md
// §4.5), the list each leg runs.
var WriterRange = []Vector{
	{File: RangeWidenHostile, Name: "range_widen", Row: "range_widen", Root: "RangeWiden"},
	{File: RangeWidenArrayHostile, Name: "range_widen_array", Row: "range_widen", Root: "RangeWiden"},
	{File: RangeWidenU64Hostile, Name: "range_widen_u64", Row: "range_widen", Root: "RangeWiden"},
}

// Expect is one vector's answer: the landed integer values by manifest path
// (`r0.v`, `r0.vals[2]`), the paths in the manifest's order, and the
// `clamped` count.
type Expect struct {
	File    string
	Values  map[string]int64
	Paths   []string
	Clamped int64
	// ForgedPath and ForgedAt are the manifest's `forged=<path>@<byte>=...`:
	// the one cell the vector damaged and its absolute byte offset in the file.
	ForgedPath string
	ForgedAt   int64
}

// Leaf is one landed value of a record, its manifest path split into the
// root field's name and, for an array element, the element's index (-1 for
// a scalar).
type Leaf struct {
	Path  string
	Field string
	Index int
	Value int64
}

// Leaves returns record 0's landed values in the manifest's order, each split
// so a leg can spell the field and the subscript in its own language.
func (e Expect) Leaves() ([]Leaf, error) {
	var out []Leaf
	for _, p := range e.Paths {
		rest, ok := strings.CutPrefix(p, "r0.")
		if !ok {
			return nil, fmt.Errorf("fixedvector: %s's expect= path %s is not record 0's", e.File, p)
		}
		l := Leaf{Path: p, Field: rest, Index: -1, Value: e.Values[p]}
		if name, sub, ok := strings.Cut(rest, "["); ok {
			idx, err := strconv.Atoi(strings.TrimSuffix(sub, "]"))
			if err != nil || !strings.HasSuffix(sub, "]") || strings.ContainsAny(name, ".[") {
				return nil, fmt.Errorf("fixedvector: %s's expect= path %s is not a field or one element of one", e.File, p)
			}
			l.Field, l.Index = name, idx
		} else if strings.Contains(rest, ".") {
			return nil, fmt.Errorf("fixedvector: %s's expect= path %s is not a root field", e.File, p)
		}
		out = append(out, l)
	}
	return out, nil
}

// Int returns the landed value at path, or an error naming the vector when
// the manifest does not carry it.
func (e Expect) Int(path string) (int64, error) {
	v, ok := e.Values[path]
	if !ok {
		return 0, fmt.Errorf("fixedvector: %s's expect= carries no %s", e.File, path)
	}
	return v, nil
}

// Read returns the `expect=` field of file's line in corpus/manifest.txt.
func Read(corpus, file string) (Expect, error) {
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		return Expect{}, fmt.Errorf("fixedvector: the corpus manifest is not readable: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		head, _, _ := strings.Cut(sc.Text(), " values=")
		named, expect, forged := false, "", ""
		for field := range strings.FieldsSeq(head) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				named = v == file
			case "expect":
				expect = v
			case "forged":
				forged = v
			}
		}
		if !named {
			continue
		}
		if expect == "" {
			return Expect{}, fmt.Errorf("fixedvector: %s's manifest line carries no expect=", file)
		}
		e, err := parse(file, expect)
		if err != nil {
			return Expect{}, err
		}
		if e.ForgedPath, e.ForgedAt, err = parseForged(file, forged); err != nil {
			return Expect{}, err
		}
		return e, nil
	}
	if err := sc.Err(); err != nil {
		return Expect{}, err
	}
	return Expect{}, fmt.Errorf("fixedvector: %s is not in the corpus manifest", file)
}

func parse(file, expect string) (Expect, error) {
	e := Expect{File: file, Values: map[string]int64{}, Clamped: -1}
	for pair := range strings.SplitSeq(expect, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return Expect{}, fmt.Errorf("fixedvector: %s's expect= has a pair with no value: %q", file, pair)
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Expect{}, fmt.Errorf("fixedvector: %s's expect= %s is not an integer: %w", file, k, err)
		}
		if k == "clamped" {
			e.Clamped = n
			continue
		}
		e.Values[k] = n
		e.Paths = append(e.Paths, k)
	}
	if e.Clamped < 0 {
		return Expect{}, fmt.Errorf("fixedvector: %s's expect= carries no clamped", file)
	}
	return e, nil
}

// parseForged splits `<path>@<byte>=<value>` into the path and the byte.
func parseForged(file, forged string) (string, int64, error) {
	path, rest, ok := strings.Cut(forged, "@")
	if !ok || path == "" {
		return "", 0, fmt.Errorf("fixedvector: %s's manifest line carries no forged=<path>@<byte>", file)
	}
	atText, _, _ := strings.Cut(rest, "=")
	at, err := strconv.ParseInt(atText, 10, 64)
	if err != nil || at < 0 {
		return "", 0, fmt.Errorf("fixedvector: %s's forged= byte %q is not an offset", file, atText)
	}
	return path, at, nil
}
