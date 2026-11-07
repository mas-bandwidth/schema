package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var strictCRC32C = crc32.MakeTable(crc32.Castagnoli)

func strictReportClean(r *tabletext.Report) bool {
	if r == nil {
		return true
	}
	return !r.Refused
}

func strictDecode(model *tabletext.Model, value *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	ok, err := tablewire.Decode(model, value, wire, report)
	if err != nil || !ok || !strictReportClean(report) || !strictWireCheckForRoot(value.Def, wire) {
		model.Clear(value)
		report.Refused = true
		if err == nil {
			err = errors.New("refused")
		}
		return false, err
	}
	return true, nil
}

func strictDecodeRegion(model *tabletext.Model, value *tabletext.Instance, wire []byte, report *tabletext.Report) (bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	ok, err := tablewire.DecodeRegion(model, value, wire, report)
	if err != nil || !ok || !strictReportClean(report) || !strictWireCheckForRoot(value.Def, wire) {
		model.Clear(value)
		report.Refused = true
		if err == nil {
			err = errors.New("refused")
		}
		return false, err
	}
	return true, nil
}

func strictDecodeRetain(model *tabletext.Model, value *tabletext.Instance, wire []byte, retain *tablewire.Retain, report *tabletext.Report) (bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	ok, err := tablewire.DecodeRetain(model, value, wire, retain, report)
	if err != nil || !ok || !strictReportClean(report) || !strictWireCheckForRoot(value.Def, wire) {
		model.Clear(value)
		report.Refused = true
		if err == nil {
			err = errors.New("refused")
		}
		return false, err
	}
	return true, nil
}

func strictDecodeRegionRetain(model *tabletext.Model, value *tabletext.Instance, wire []byte, retain *tablewire.Retain, report *tabletext.Report) (bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	ok, err := tablewire.DecodeRegionRetain(model, value, wire, retain, report)
	if err != nil || !ok || !strictReportClean(report) || !strictWireCheckForRoot(value.Def, wire) {
		model.Clear(value)
		report.Refused = true
		if err == nil {
			err = errors.New("refused")
		}
		return false, err
	}
	return true, nil
}

func strictDecodeMessages(model *tabletext.Model, values []*tabletext.Instance, wire []byte, vocab *tablewire.Vocabulary, report *tabletext.Report) (int, bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	n, ok, err := tablewire.DecodeMessages(model, values, wire, vocab, report)
	if err != nil || !ok || !strictReportClean(report) {
		for _, v := range values {
			if v != nil {
				model.Clear(v)
			}
		}
		report.Refused = true
		if err == nil {
			err = errors.New("refused")
		}
		return 0, false, err
	}
	return n, true, nil
}

func strictReadJSON(model *tabletext.Model, value *tabletext.Instance, text []byte, report *tabletext.Report) bool {
	if report == nil {
		report = &tabletext.Report{}
	}
	ok := model.Read(value, text, report)
	if !ok || !strictReportClean(report) || !strictJSONCheck(text) {
		model.Clear(value)
		report.Refused = true
		return false
	}
	return true
}

func strictWireCheckForRoot(root *ir.Struct, data []byte) bool {
	// A fixed root reads form 3 alone (docs/SPEC-TABLES.md §3.4): the check
	// below walks the variable form's framing, so the engine's own read is
	// the verdict there.
	if root != nil && root.FixedDeclared {
		return true
	}
	if root != nil && root.Integrity != "" {
		if root.Integrity != "crc32c" || len(data) < 5 {
			return false
		}
		n := len(data) - 4
		if binary.LittleEndian.Uint32(data[n:]) != crc32.Checksum(data[:n], strictCRC32C) {
			return false
		}
		data = data[:n]
	}
	return strictWireCheckBody(data)
}

func strictWireCheck(data []byte) bool {
	if strictWireCheckBody(data) {
		return true
	}
	if len(data) >= 13 {
		n := len(data) - 4
		if binary.LittleEndian.Uint32(data[n:]) == crc32.Checksum(data[:n], strictCRC32C) {
			return strictWireCheckBody(data[:n])
		}
	}
	return false
}

func strictWireCheckBody(data []byte) bool {
	if len(data) < 9 || data[0] != ir.TableWireForm {
		return false
	}
	body, ids, ok := strictTrailer(data)
	if !ok {
		return false
	}
	c := &strictWireChecker{ids: ids}
	return c.checkBody(body) && !c.nonFinite
}

func strictTrailer(data []byte) (body []byte, ids []uint64, ok bool) {
	if len(data) < 9 {
		return nil, nil, false
	}
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	if count > uint64(len(data))/8 || count > ir.TableWireIdTableBound {
		return nil, nil, false
	}
	span := int(count)*8 + 8
	if span+1 > len(data) {
		return nil, nil, false
	}
	first := len(data) - span
	ids = make([]uint64, count)
	seen := make(map[uint64]bool, count)
	for i := range ids {
		id := binary.LittleEndian.Uint64(data[first+i*8:])
		if seen[id] {
			return nil, nil, false
		}
		seen[id] = true
		ids[i] = id
	}
	return data[1:first], ids, true
}

func strictReadLeb(buf []byte, off int) (v uint64, next int, ok bool) {
	shift := uint(0)
	for i := 0; ; i++ {
		if off >= len(buf) || i >= 10 {
			return 0, off, false
		}
		b := buf[off]
		off++
		if i == 9 && b > 1 {
			return 0, off, false
		}
		v |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			if i > 0 && b == 0 {
				return 0, off, false
			}
			return v, off, true
		}
		shift += 7
	}
}

type strictNodeRecord struct {
	TypeId uint64
	Body   []byte
}

func strictScanNodeRecords(payload []byte, ids []uint64) ([]strictNodeRecord, bool) {
	declared, off, ok := strictReadLeb(payload, 0)
	if !ok {
		return nil, false
	}
	var records []strictNodeRecord
	for off < len(payload) {
		ref, next, good := strictReadLeb(payload, off)
		if !good || ref == 0 || ref > uint64(len(ids)) {
			return nil, false
		}
		off = next
		length, after, good := strictReadLeb(payload, off)
		if !good {
			return nil, false
		}
		off = after
		if length > uint64(len(payload)-off) {
			return nil, false
		}
		records = append(records, strictNodeRecord{TypeId: ids[ref-1], Body: payload[off : off+int(length)]})
		off += int(length)
	}
	if off != len(payload) || declared != uint64(len(records)) {
		return nil, false
	}
	return records, true
}

func strictTextValid(b []byte) bool {
	return utf8.Valid(b) && bytes.IndexByte(b, 0) < 0
}

func strictWideValid(b []byte) bool {
	if len(b)&1 != 0 {
		return false
	}
	n := len(b) / 2
	for i := 0; i < n; i++ {
		u := uint16(b[i*2]) | uint16(b[i*2+1])<<8
		switch {
		case u == 0:
			return false
		case u >= 0xD800 && u <= 0xDBFF:
			if i+1 >= n {
				return false
			}
			low := uint16(b[(i+1)*2]) | uint16(b[(i+1)*2+1])<<8
			if low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i++
		case u >= 0xDC00 && u <= 0xDFFF:
			return false
		}
	}
	return true
}

func strictFixedWidthKind(kind int) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128,
		ir.TableKindTimestamp:
		return true
	}
	return false
}

type strictWireChecker struct {
	ids       []uint64
	edges     []uint64
	nonFinite bool
	depth     int
}

func (c *strictWireChecker) checkBody(body []byte) bool {
	var table []byte
	present := false
	if !c.body(body, true, func(payload []byte) bool {
		table, present = payload, true
		return true
	}) {
		return false
	}
	children := [][]uint64{c.edges}
	var blobs []bool
	if present {
		records, ok := strictScanNodeRecords(table, c.ids)
		if !ok {
			return false
		}
		for _, rec := range records {
			c.edges = nil
			switch rec.TypeId {
			case ir.BytesWireTypeId, ir.StringWireTypeId:
				if uint64(len(rec.Body)) > math.MaxUint32 {
					return false
				}
				if rec.TypeId == ir.StringWireTypeId && !strictTextValid(rec.Body) {
					return false
				}
				blobs = append(blobs, true)
			default:
				if !c.body(rec.Body, false, nil) {
					return false
				}
				blobs = append(blobs, false)
			}
			children = append(children, c.edges)
		}
	}
	return c.graph(children, blobs)
}

func (c *strictWireChecker) graph(children [][]uint64, blobs []bool) bool {
	n := len(children)
	names := make([]int, n)
	for _, out := range children {
		for _, index := range out {
			if index == ir.NodeWireIndexNull {
				continue
			}
			if index-1 >= uint64(n) {
				return false
			}
			names[index-1]++
		}
	}
	for k, blob := range blobs {
		if blob && names[k+1] > 1 {
			return false
		}
	}
	const (
		markUnvisited = 0
		markOpen      = 1
		markClosed    = 2
	)
	mark := make([]uint8, n)
	type frame struct {
		node int
		next int
	}
	stack := []frame{{node: 0}}
	mark[0] = markOpen
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		out := children[top.node]
		if top.next == len(out) {
			mark[top.node] = markClosed
			stack = stack[:len(stack)-1]
			continue
		}
		index := out[top.next]
		top.next++
		if index == ir.NodeWireIndexNull {
			continue
		}
		t := int(index - 1)
		switch mark[t] {
		case markOpen:
			return false
		case markClosed:
			continue
		}
		mark[t] = markOpen
		stack = append(stack, frame{node: t})
	}
	for k := 1; k < n; k++ {
		if mark[k] == markUnvisited {
			return false
		}
	}
	return true
}

func (c *strictWireChecker) body(buf []byte, root bool, table func([]byte) bool) bool {
	off := 0
	var seen map[uint64]bool
	for {
		ref, next, ok := strictReadLeb(buf, off)
		if !ok {
			return false
		}
		off = next
		if ref == 0 {
			return off == len(buf)
		}
		if ref > uint64(len(c.ids)) {
			return false
		}
		id := c.ids[ref-1]
		if seen == nil {
			seen = map[uint64]bool{}
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		if off >= len(buf) {
			return false
		}
		kind := int(buf[off])
		off++
		switch id {
		case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
			return false
		case ir.TableNodeWireId:
			if !root || kind != ir.TableKindString {
				return false
			}
			n, after, good := strictReadLeb(buf, off)
			if !good || n > uint64(len(buf)-after) {
				return false
			}
			off = after + int(n)
			if !table(buf[after:off]) {
				return false
			}
			continue
		}
		if off, ok = c.payload(buf, off, kind); !ok {
			return false
		}
	}
}

func (c *strictWireChecker) payload(buf []byte, off int, kind int) (int, bool) {
	switch {
	case strictFixedWidthKind(kind):
		w := ir.TableKindWidth(kind)
		if w > len(buf)-off {
			return off, false
		}
		return off + w, c.scalar(buf[off:off+w], kind)
	case kind == ir.TableKindPointer:
		index, next, ok := strictReadLeb(buf, off)
		if !ok {
			return off, false
		}
		c.edges = append(c.edges, index)
		return next, true
	case kind == ir.TableKindEnum:
		ref, next, ok := strictReadLeb(buf, off)
		if !ok || ref > uint64(len(c.ids)) {
			return off, false
		}
		return next, true
	case kind == ir.TableKindUnion:
		return c.union(buf, off)
	case kind == ir.TableKindString, kind == ir.TableKindWstring, kind == ir.TableKindEscape,
		kind == ir.TableKindNoPayload, kind == ir.TableKindTable, kind == ir.TableKindArray,
		kind == ir.TableKindKeyed:
		n, next, ok := strictReadLeb(buf, off)
		if !ok || n > uint64(len(buf)-next) {
			return off, false
		}
		end := next + int(n)
		return end, c.content(buf[next:end], kind)
	}
	return off, false
}

func (c *strictWireChecker) content(b []byte, kind int) bool {
	switch {
	case strictFixedWidthKind(kind):
		if len(b) != ir.TableKindWidth(kind) {
			return false
		}
		return c.scalar(b, kind)
	case kind == ir.TableKindString:
		return strictTextValid(b)
	case kind == ir.TableKindWstring:
		return strictWideValid(b)
	case kind == ir.TableKindEscape:
		return true
	case kind == ir.TableKindNoPayload:
		return len(b) == 0
	case kind == ir.TableKindTable, kind == ir.TableKindArray, kind == ir.TableKindKeyed:
		if c.depth >= 64 {
			return false
		}
		c.depth++
		defer func() { c.depth-- }()
		switch kind {
		case ir.TableKindTable:
			return c.body(b, false, nil)
		case ir.TableKindArray:
			return c.array(b)
		}
		return c.keyed(b)
	case kind == ir.TableKindPointer, kind == ir.TableKindEnum, kind == ir.TableKindUnion:
		end, ok := c.payload(b, 0, kind)
		return ok && end == len(b)
	}
	return false
}

func (c *strictWireChecker) scalar(b []byte, kind int) bool {
	switch kind {
	case ir.TableKindBool:
		if b[0] > 1 {
			return false
		}
	case ir.TableKindF32:
		if !tabletext.FiniteF32(binary.LittleEndian.Uint32(b)) {
			c.nonFinite = true
		}
	case ir.TableKindF64:
		if !tabletext.FiniteF64(binary.LittleEndian.Uint64(b)) {
			c.nonFinite = true
		}
	}
	return true
}

func (c *strictWireChecker) arrayHeader(b []byte) (ek int, count uint64, off int, ok bool) {
	if len(b) < 2 {
		return 0, 0, 0, false
	}
	count, off, ok = strictReadLeb(b, 1)
	if !ok || count > math.MaxInt32 {
		return 0, 0, 0, false
	}
	return int(b[0]), count, off, true
}

func (c *strictWireChecker) array(b []byte) bool {
	ek, count, off, ok := c.arrayHeader(b)
	if !ok {
		return false
	}
	for range count {
		if off, ok = c.payload(b, off, ek); !ok {
			return false
		}
	}
	return off == len(b)
}

func (c *strictWireChecker) keyed(b []byte) bool {
	ek, count, off, ok := c.arrayHeader(b)
	if !ok {
		return false
	}
	var seen map[uint64]bool
	for range count {
		key, next, good := strictReadLeb(b, off)
		if !good || key == 0 || key > uint64(len(c.ids)) {
			return false
		}
		if seen == nil {
			seen = map[uint64]bool{}
		}
		if seen[key] {
			return false
		}
		seen[key] = true
		n, after, good := strictReadLeb(b, next)
		if !good || n > uint64(len(b)-after) {
			return false
		}
		off = after + int(n)
		if !c.content(b[after:off], ek) {
			return false
		}
	}
	return off == len(b)
}

func (c *strictWireChecker) union(buf []byte, off int) (int, bool) {
	ref, next, ok := strictReadLeb(buf, off)
	if !ok {
		return off, false
	}
	if ref == 0 {
		return next, true
	}
	if ref > uint64(len(c.ids)) || next >= len(buf) {
		return off, false
	}
	kind := int(buf[next])
	n, after, good := strictReadLeb(buf, next+1)
	if !good || n > uint64(len(buf)-after) {
		return off, false
	}
	end := after + int(n)
	if kind == ir.TableKindTable || kind == ir.TableKindArray || kind == ir.TableKindKeyed || kind == ir.TableKindUnion {
		if c.depth >= 64 {
			return off, false
		}
		c.depth++
		defer func() { c.depth-- }()
	}
	return end, c.content(buf[after:end], kind)
}

// strictJSONCheck verifies that JSON text contains no lone/unpaired UTF-16
// surrogate escapes and no duplicate object keys at any nesting level.
func strictJSONCheck(data []byte) bool {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	i := 0
	var stack []map[string]bool
	var expectingKey []bool
	for i < len(data) {
		// skip whitespace and comments
		c := data[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < len(data) {
			if data[i+1] == '/' {
				i += 2
				for i < len(data) && data[i] != '\n' {
					i++
				}
				continue
			}
			if data[i+1] == '*' {
				i += 2
				for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}
				i += 2
				continue
			}
		}
		switch c {
		case '{':
			stack = append(stack, map[string]bool{})
			expectingKey = append(expectingKey, true)
			i++
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
				expectingKey = expectingKey[:len(expectingKey)-1]
			}
			i++
		case '[':
			stack = append(stack, nil)
			expectingKey = append(expectingKey, false)
			i++
		case ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
				expectingKey = expectingKey[:len(expectingKey)-1]
			}
			i++
		case ':':
			if len(expectingKey) > 0 && stack[len(stack)-1] != nil {
				expectingKey[len(expectingKey)-1] = false
			}
			i++
		case ',':
			if len(expectingKey) > 0 && stack[len(stack)-1] != nil {
				expectingKey[len(expectingKey)-1] = true
			}
			i++
		case '"':
			s, next, ok := strictScanJSONString(data, i)
			if !ok {
				return false
			}
			if len(expectingKey) > 0 && expectingKey[len(expectingKey)-1] && stack[len(stack)-1] != nil {
				m := stack[len(stack)-1]
				if m[s] {
					return false
				}
				m[s] = true
			}
			i = next
		default:
			i++
		}
	}
	return true
}

func strictScanJSONString(data []byte, start int) (string, int, bool) {
	var out []byte
	i := start + 1
	for i < len(data) {
		c := data[i]
		if c == '"' {
			return string(out), i + 1, true
		}
		if c < 0x20 {
			return "", i, false
		}
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		i++
		if i >= len(data) {
			return "", i, false
		}
		esc := data[i]
		i++
		switch esc {
		case '"', '\\', '/':
			out = append(out, esc)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			if i+4 > len(data) {
				return "", i, false
			}
			u1, err := strconv.ParseUint(string(data[i:i+4]), 16, 16)
			if err != nil {
				return "", i, false
			}
			i += 4
			r := rune(u1)
			if utf16.IsSurrogate(r) {
				// Must be a high surrogate followed by \uXXXX low surrogate
				if r < 0xD800 || r > 0xDBFF || i+6 > len(data) || data[i] != '\\' || data[i+1] != 'u' {
					return "", i, false
				}
				u2, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
				if err != nil || u2 < 0xDC00 || u2 > 0xDFFF {
					return "", i, false
				}
				i += 6
				r = utf16.DecodeRune(r, rune(u2))
			}
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			out = append(out, buf[:n]...)
		default:
			return "", i, false
		}
	}
	return "", i, false
}
