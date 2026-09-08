package tablewire

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// NodeRecordTypes is the node table's record scan as a MEASURE would see it
// (docs/SPEC-TABLES.md §6.5): the wire type id of every record the root body's
// node table carries, in index order, and whether the table read WHOLE — the
// form byte known, the id table read whole, the field framed, every record
// inside it, and `node_count` agreeing with the scan. A root with no node table
// at all is whole with no records.
//
// It exists for the wire fuzzer's oracle: a reader's `LoadMeasure` sums each
// record's storage from exactly this scan, so a whole scan states the exact
// answer that reader owes, and a broken one states the bound it may not exceed.
func NodeRecordTypes(data []byte) (types []uint64, whole bool) {
	body, ids, ok := trailer(data)
	if len(data) < 1 || data[0] != ir.TableWireForm || !ok {
		return nil, false
	}
	return nodeRecordTypes(body, ids)
}

func nodeRecordTypes(body []byte, ids []uint64) (types []uint64, whole bool) {
	var ignored tabletext.Report
	payload, present, framed := nodeTableBytes(body, ids, &ignored)
	if !present {
		return nil, true
	}
	if !framed {
		return nil, false
	}
	records, scanned := scanNodeRecords(payload, ids)
	if !scanned {
		return nil, false
	}
	types = make([]uint64, len(records))
	for i, rec := range records {
		types[i] = rec.TypeId
	}
	return types, true
}

// FileNodeRecord is a framed record before decoding its body. Length is needed
// to account for blob storage; a type ID alone does not determine that storage.
type FileNodeRecord struct {
	TypeId uint64
	Length int64
}

// FileNodeRecords returns the authoritative file node-table scan. It allocates
// from records actually framed, never from the untrusted declared count.
func FileNodeRecords(data []byte) ([]FileNodeRecord, bool) {
	body, ids, ok := trailer(data)
	if len(data) == 0 || data[0] != ir.TableWireForm || !ok {
		return nil, false
	}
	var report tabletext.Report
	payload, present, framed := nodeTableBytes(body, ids, &report)
	if !present {
		return nil, true
	}
	if !framed {
		return nil, false
	}
	records, scanned := scanNodeRecords(payload, ids)
	if !scanned {
		return nil, false
	}
	out := make([]FileNodeRecord, len(records))
	for i, record := range records {
		out[i] = FileNodeRecord{TypeId: record.TypeId, Length: int64(len(record.Body))}
	}
	return out, true
}
