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

// NodeRecordTypesMessage is the same scan over the MESSAGE form
// (docs/SPEC-TABLES.md §3.3): the wire is the form byte and the root body, so
// the body runs to the last byte and the ids are the CONNECTION's table rather
// than a trailer of its own. Everything the scan then does is §3.1's, because
// the node table is a field of the root body and never part of a trailer.
func NodeRecordTypesMessage(data []byte, ids []uint64) (types []uint64, whole bool) {
	if len(data) < 1 || data[0] != ir.TableWireMessageForm {
		return nil, false
	}
	return nodeRecordTypes(data[1:], ids)
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
