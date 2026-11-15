// Package fixedvector reads the fixed form's shared cross-version vectors out
// of the C++ reference's corpus manifest, so every leg asserts the answer the
// reference landed and none restates it by hand.
//
// A vector is a forged corpus file. Under the one read (docs/plans/strict-only.md)
// the reference's NEW reader REFUSES each writer's-range vector whole, so its
// manifest line carries no `expect=` and Read answers Refused
// (docs/FIXED-FORM-VERSIONING-TESTS.md, test/tables/fixedform_dump.cpp). A line
// that does carry `expect=` is the values the reference landed.
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
// The NEW reader, VNEW_range_widen with VOLD_range_widen in its lineage,
// refuses the whole load.
const RangeWidenHostile = "hostile_range_widen.bin"

// RangeWidenArrayHostile is the writer's-range vector for a ranged FIXED
// ARRAY: old_range_widen.bin (`vals [4]int32 | 0..100`) with `vals[2]` forged
// to 150, inside the NEW reader's | 0..200. Every element is held to the writer's
// range the same as a scalar, so the NEW reader refuses the whole load.
const RangeWidenArrayHostile = "hostile_range_widen_array.bin"

// RangeWidenU64Hostile is the writer's-range vector for an UNSIGNED 64-bit
// field: old_range_widen.bin (`u uint64 | 10..1000`) with `u` forged to
// 2^63 + 7.
// The comparison is in the field's own type, unsigned, so the NEW reader
// (| 0..2000) refuses the whole load; a signed comparison reads a negative
// number, inside the range, and lands it.
const RangeWidenU64Hostile = "hostile_range_widen_u64.bin"

// Vector names one shared vector: its corpus file, a Name that tells it from
// the others (a leg's subtest), and the fixed root it holds. A writer's-range
// vector names the versioning row whose pair it reads (VOLD_<Row> in the
// lineage of the reader VNEW_<Row>). A deprecated-field vector is read under
// its own layout: it names the test/tables schema that declares the root
// (Schema, without .schema) and the deprecated root field it holds off its
// declared default (Field).
type Vector struct {
	File, Name, Row, Root string
	Schema, Field         string
}

// WriterRange is every writer's-range vector (docs/FIXED-FORM-ALGORITHM.md
// §4.5), the list each leg runs.
var WriterRange = []Vector{
	{File: RangeWidenHostile, Name: "range_widen", Row: "range_widen", Root: "RangeWiden"},
	{File: RangeWidenArrayHostile, Name: "range_widen_array", Row: "range_widen", Root: "RangeWiden"},
	{File: RangeWidenU64Hostile, Name: "range_widen_u64", Row: "range_widen", Root: "RangeWiden"},
}

// The deprecated-field vectors (docs/SPEC-TABLES.md §3.4, §21.2, §22.6;
// docs/rulings.json fixed-deprecated-field-is-carried): one file per shape of
// a `| deprecated` field of test/tables/FD1.schema's FdRoot, each record
// holding a lawful value in that field that is NOT its declared default, and
// ordinary values elsewhere. A deprecated field in the fixed form is carried,
// so each file reads, lands the STORED value (the manifest's `expect=`), and
// saves back byte for byte.
const (
	DeprecatedScalar   = "deprecated_scalar.bin"   // old_count int32 = 4: holds 99
	DeprecatedString   = "deprecated_string.bin"   // old_tag string(8) = "abc": holds "hi"
	DeprecatedArray    = "deprecated_array.bin"    // old_marks [..3]uint16: holds a count of 2
	DeprecatedOptional = "deprecated_optional.bin" // old_bonus ?uint16: present
	DeprecatedNested   = "deprecated_nested.bin"   // old_spot Spot (5, 6): holds (50, 60)
	DeprecatedUnion    = "deprecated_union.bin"    // old_pick Pick: the spot arm, off its defaults
)

// DeprecatedField is every deprecated-field vector, the list each leg runs.
var DeprecatedField = []Vector{
	{File: DeprecatedScalar, Name: "deprecated_scalar", Root: "FdRoot", Schema: "FD1", Field: "old_count"},
	{File: DeprecatedString, Name: "deprecated_string", Root: "FdRoot", Schema: "FD1", Field: "old_tag"},
	{File: DeprecatedArray, Name: "deprecated_array", Root: "FdRoot", Schema: "FD1", Field: "old_marks"},
	{File: DeprecatedOptional, Name: "deprecated_optional", Root: "FdRoot", Schema: "FD1", Field: "old_bonus"},
	{File: DeprecatedNested, Name: "deprecated_nested", Root: "FdRoot", Schema: "FD1", Field: "old_spot"},
	{File: DeprecatedUnion, Name: "deprecated_union", Root: "FdRoot", Schema: "FD1", Field: "old_pick"},
}

// Expect is one vector's answer: Refused when the reference refused the whole
// load (the line carries no `expect=`), else the landed integer values by
// manifest path (`r0.v`, `r0.vals[2]`) in the manifest's order, and the landed
// text values (`r0.old_tag`, quoted in the manifest) in Texts. Clamped is
// the retired clamp's count, zero on every line the one read dumps; it stays
// only until every leg's range-widen test asserts Refused.
type Expect struct {
	File    string
	Refused bool
	Values  map[string]int64
	Paths   []string
	Texts   map[string]string
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

// Read returns file's answer from its line in corpus/manifest.txt: Refused
// when the line carries no `expect=`, else the `expect=` values. A forged
// vector's line carries `forged=`; a line with no `forged=` is a vector only
// when it carries `expect=` (a deprecated-field vector, read as written).
func Read(corpus, file string) (Expect, error) {
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		return Expect{}, fmt.Errorf("fixedvector: the corpus manifest is not readable: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only: Scanner reports read errors
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
		e := Expect{File: file, Refused: true, Values: map[string]int64{}, Texts: map[string]string{}}
		if expect != "" {
			var err error
			if e, err = parse(file, expect); err != nil {
				return Expect{}, err
			}
			if forged == "" {
				return e, nil
			}
		}
		var err error
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
	e := Expect{File: file, Values: map[string]int64{}, Texts: map[string]string{}}
	pairs, err := splitPairs(expect)
	if err != nil {
		return Expect{}, fmt.Errorf("fixedvector: %s's expect= %w", file, err)
	}
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return Expect{}, fmt.Errorf("fixedvector: %s's expect= has a pair with no value: %q", file, pair)
		}
		if strings.HasPrefix(v, `"`) {
			text, err := unquote(v)
			if err != nil {
				return Expect{}, fmt.Errorf("fixedvector: %s's expect= %s %w", file, k, err)
			}
			e.Texts[k] = text
			continue
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
	return e, nil
}

// splitPairs splits a manifest value list at its commas, keeping a comma the
// dumper escaped inside a quoted text (`\,`) in its pair.
func splitPairs(list string) ([]string, error) {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(list); i++ {
		switch c := list[i]; {
		case c == '\\' && quoted:
			i++
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, list[start:i])
			start = i + 1
		}
	}
	if quoted {
		return nil, fmt.Errorf("has an unterminated text: %q", list[start:])
	}
	return append(out, list[start:]), nil
}

// unquote reads the dumper's quoted text: `\"`, `\\` and `\,` stand for the
// character, `\uXXXX` for one byte outside printable ASCII.
func unquote(v string) (string, error) {
	if len(v) < 2 || !strings.HasSuffix(v, `"`) {
		return "", fmt.Errorf("is not a quoted text: %q", v)
	}
	body := v[1 : len(v)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(body) {
			return "", fmt.Errorf("ends in an escape: %q", v)
		}
		i++
		switch body[i] {
		case '"', '\\', ',':
			b.WriteByte(body[i])
		case 'u':
			if i+5 > len(body) {
				return "", fmt.Errorf("has a short \\u escape: %q", v)
			}
			n, err := strconv.ParseUint(body[i+1:i+5], 16, 8)
			if err != nil {
				return "", fmt.Errorf("has a \\u escape that is not one byte: %q", v)
			}
			b.WriteByte(byte(n))
			i += 4
		default:
			return "", fmt.Errorf("has an unknown escape: %q", v)
		}
	}
	return b.String(), nil
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
