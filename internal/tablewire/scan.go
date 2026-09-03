package tablewire

import "github.com/mas-bandwidth/schema/v2/internal/tabletext"

// NodeRecordTypes is the node table's record scan as a MEASURE would see it
// (docs/SPEC-TABLES.md §6.5): the wire type id of every record the root body's
// node table carries, in index order, and whether the table read WHOLE — every
// field framed, every record inside its field, and `node_count` agreeing with
// the scan. A root with no node table at all is whole with no records.
//
// It exists for the wire fuzzer's oracle: a reader's `LoadMeasure` sums each
// record's storage from exactly this scan, so a whole scan states the exact
// answer that reader owes, and a broken one states the bound it may not exceed.
func NodeRecordTypes(data []byte) (types []uint64, whole bool) {
	var ignored tabletext.Report
	payload, present, framed := nodeTableBytes(data, &ignored)
	if !present {
		return nil, true
	}
	if !framed {
		return nil, false
	}
	records, scanned := scanNodeRecords(payload)
	if !scanned {
		return nil, false
	}
	types = make([]uint64, len(records))
	for i, rec := range records {
		types[i] = rec.TypeId
	}
	return types, true
}
