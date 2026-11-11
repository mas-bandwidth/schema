// Command gen builds the fixed-empty-batch wire vector (rulings
// fixed-form-edges): one file holding one record, and the same header, layout
// and integrity trailer with the record removed. It writes the engine's own
// one-record bytes and derives the no-record file from them by stripping the
// record and recomputing the CRC-32C trailer, so the two files differ only in
// the bytes a file without a record lacks.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

var fileCRC32C = crc32.MakeTable(crc32.Castagnoli)

const value = `{"a": 7, "name": "hello"}`

func main() {
	schema := flag.String("schema", "test/fixed-empty-batch/Batch.schema", "the fixed table's schema file")
	out := flag.String("out", "testdata/wire/fixed-empty-batch", "where the two files are written")
	flag.Parse()

	oneRecord, noRecord, err := build(*schema)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*out, "one-record.bin"), oneRecord, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*out, "no-record.bin"), noRecord, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// build returns the engine's one-record bytes and the derived no-record bytes.
func build(schema string) (oneRecord, noRecord []byte, err error) {
	u, err := compiler.New().Load([]string{schema})
	if err != nil {
		return nil, nil, fmt.Errorf("load %s: %w", schema, err)
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Batch")
	if st == nil {
		return nil, nil, fmt.Errorf("schema has no fixed table Batch")
	}
	inst := m.New(st)
	var report tabletext.Report
	if !m.Read(inst, []byte(value), &report) || !report.Silent() {
		return nil, nil, fmt.Errorf("place %s: %+v", value, report)
	}
	oneRecord, err = tablewire.Encode(m, inst)
	if err != nil {
		return nil, nil, fmt.Errorf("encode one record: %w", err)
	}
	if len(oneRecord) < 24 {
		return nil, nil, fmt.Errorf("encoded file too short: %d bytes", len(oneRecord))
	}
	// THE NO-RECORD FILE IS THE SAME HEADER AND LAYOUT, the record dropped and
	// the integrity trailer recomputed over the bytes before it (§2.14).
	layoutBytes := binary.LittleEndian.Uint32(oneRecord[16:])
	headerAndLayout := oneRecord[:20+int(layoutBytes)]
	noRecord = binary.LittleEndian.AppendUint32(append([]byte(nil), headerAndLayout...),
		crc32.Checksum(headerAndLayout, fileCRC32C))
	return oneRecord, noRecord, nil
}
