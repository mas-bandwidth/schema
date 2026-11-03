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

// Expect is one vector's answer: the landed integer values by manifest path
// (`r0.v`) and the `clamped` count.
type Expect struct {
	File    string
	Values  map[string]int64
	Clamped int64
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
		named, expect := false, ""
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
			}
		}
		if !named {
			continue
		}
		if expect == "" {
			return Expect{}, fmt.Errorf("fixedvector: %s's manifest line carries no expect=", file)
		}
		return parse(file, expect)
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
	}
	if e.Clamped < 0 {
		return Expect{}, fmt.Errorf("fixedvector: %s's expect= carries no clamped", file)
	}
	return e, nil
}
