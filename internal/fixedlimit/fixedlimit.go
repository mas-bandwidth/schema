// Package fixedlimit reads the fixed form's hard-limit vectors out of the C++
// reference's corpus, so every leg asserts the answer the reference gave and
// none restates it by hand.
//
// test/tables/fixedform_dump.cpp writes two files per limit of
// docs/SPEC-TABLES.md §3.4 (fixed_max_depth, fixed_record_bound) and one line
// per file in limits.txt beside them: the file AT the bound, which the NEW
// reader reads whole through its lineage entry for the OLD layout, and the file
// one PAST it, whose forged layout the reader refuses. The line carries the
// verdict, the refusal's name, the report's counters, the values the reader
// landed, and `parse=`: what the reference's layout parser answered for the
// file's layout bytes on their own.
package fixedlimit

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The four vector files. DepthAt nests exactly 64 (VOLD_limit_depth); DepthOver
// is the same file with one table level spliced into its layout, 65. RecordAt
// is a 65536-byte record body (VOLD_limit_record); RecordOver is the same file
// with its layout and its record grown to 65537.
const (
	DepthAt    = "old_limit_depth.bin"
	DepthOver  = "hostile_limit_depth.bin"
	RecordAt   = "old_limit_record.bin"
	RecordOver = "hostile_limit_record.bin"
)

// Vector is one line of limits.txt.
type Vector struct {
	File    string
	Limit   string // the spec's limit marker name
	Bound   int64  // the marker's value
	Root    string
	Records int64
	Verdict string // "read" or "refused"
	Reason  string // the refusal's name, empty for a read
	Parse   string // "ok" or the layout parser's refusal name
	// Counters are the report's unknown, kind_mismatch, widened, clamped and
	// duplicate counts and its malformed flag (0 or 1).
	Counters map[string]int64
	// Values are the landed integers by path (`r0.lead`), empty for a refusal.
	Values map[string]int64
}

// Refused answers whether the reference refused the file.
func (v Vector) Refused() bool { return v.Verdict == "refused" }

// Int returns the landed value at path, or an error naming the vector.
func (v Vector) Int(path string) (int64, error) {
	n, ok := v.Values[path]
	if !ok {
		return 0, fmt.Errorf("fixedlimit: %s carries no value %s", v.File, path)
	}
	return n, nil
}

// Counter returns one report counter, or an error naming the vector.
func (v Vector) Counter(name string) (int64, error) {
	n, ok := v.Counters[name]
	if !ok {
		return 0, fmt.Errorf("fixedlimit: %s carries no counter %s", v.File, name)
	}
	return n, nil
}

// Read returns file's line of corpus/limits.txt.
func Read(corpus, file string) (Vector, error) {
	f, err := os.Open(filepath.Join(corpus, "limits.txt"))
	if err != nil {
		return Vector{}, fmt.Errorf("fixedlimit: limits.txt is not readable: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		v, err := parse(sc.Text())
		if err != nil {
			return Vector{}, err
		}
		if v.File == file {
			return v, nil
		}
	}
	if err := sc.Err(); err != nil {
		return Vector{}, err
	}
	return Vector{}, fmt.Errorf("fixedlimit: %s is not in limits.txt", file)
}

func parse(line string) (Vector, error) {
	v := Vector{Counters: map[string]int64{}, Values: map[string]int64{}}
	for field := range strings.FieldsSeq(line) {
		k, val, ok := strings.Cut(field, "=")
		if !ok {
			return Vector{}, fmt.Errorf("fixedlimit: a field with no value: %q", field)
		}
		var err error
		switch k {
		case "file":
			v.File = val
		case "limit":
			v.Limit = val
		case "bound":
			v.Bound, err = strconv.ParseInt(val, 10, 64)
		case "root":
			v.Root = val
		case "records":
			v.Records, err = strconv.ParseInt(val, 10, 64)
		case "verdict":
			v.Verdict = val
		case "reason":
			v.Reason = val
		case "parse":
			v.Parse = val
		case "counters":
			err = pairs(val, v.Counters)
		case "values":
			err = pairs(val, v.Values)
		}
		if err != nil {
			return Vector{}, fmt.Errorf("fixedlimit: %s: %w", field, err)
		}
	}
	if v.File == "" || v.Limit == "" || v.Verdict == "" || v.Parse == "" || len(v.Counters) == 0 {
		return Vector{}, fmt.Errorf("fixedlimit: an incomplete line: %q", line)
	}
	if v.Refused() == (v.Reason == "") {
		return Vector{}, fmt.Errorf("fixedlimit: %s: a refusal names its reason and a read names none", v.File)
	}
	return v, nil
}

func pairs(text string, into map[string]int64) error {
	for pair := range strings.SplitSeq(text, ",") {
		k, val, ok := strings.Cut(pair, "=")
		if !ok {
			return fmt.Errorf("a pair with no value: %q", pair)
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("%s is not an integer: %w", k, err)
		}
		into[k] = n
	}
	return nil
}

// Layout returns the layout bytes a form-3 file carries: the u32 length at
// byte 16 and the bytes behind it (docs/SPEC-TABLES.md §3.4, THE FIRST BYTE).
func Layout(file []byte) ([]byte, error) {
	if len(file) < 20 {
		return nil, fmt.Errorf("fixedlimit: %d bytes is shorter than a form-3 header", len(file))
	}
	n := int(binary.LittleEndian.Uint32(file[16:]))
	if 20+n > len(file) {
		return nil, fmt.Errorf("fixedlimit: a %d-byte layout past a %d-byte file", n, len(file))
	}
	return file[20 : 20+n], nil
}

// DeepPath is the path of DepthAt's deepest value, `r0` then 63 `.d` hops
// and `.v`: the entry at depth 64.
func DeepPath() string {
	return "r0" + strings.Repeat(".d", 63) + ".v"
}
