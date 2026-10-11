package jstable

// THE MESSAGE FORM'S ANNOUNCEMENT SURFACE (docs/SPEC-TABLES.md §3.3), form
// byte 2, which is a fixed table's. This card emits the announcement half:
// `TableVocabulary`, `Announce`, `AnnounceMeasure` and `AnnounceRead`. The
// batch body verbs (`MeasureMessages`, `SaveMessages`, `LoadMessages`) are the
// next card and are not here.
//
// §3.3 is a form-1 FILE the unit's compiler settles whole: "THE WHOLE
// ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ... a backend emits
// `Announce` and `AnnounceMeasure` as a constant byte array and its length
// rather than as a walk." The byte array is `ir.TableAnnouncement`, the same
// bytes every other backend emits and the manifest pins.
//
// THE MODEL HERE IS THE JAVASCRIPT SURFACE'S OWN LOGIC, stated in Go so this
// leg's test can hold every clause of §3.3 without a JavaScript runtime on the
// gate's host. The emitted module carries the same surface as text; the
// constant bytes, the entry count and the reference width are the compiler's
// and are shared, so a disagreement between the two is a disagreement in the
// constants the test holds byte for byte.
import (
	"fmt"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsTableMessageShape and jsTableMessageEntry are the runtime twin of
// ir.TableMessageShape/Entry with the wide base flattened to two lanes, which
// is what a JavaScript runtime can hold.
type jsTableMessageShape struct {
	Kind, Packing uint8
	Bits          int64
	Base          [2]uint64
	Min, Max      int64
	QMin, QDelta  float32
	QCount        uint32
}

type jsTableMessageEntry struct {
	Id             uint64
	Shape, Element jsTableMessageShape
}

// jsTableVocabulary is §3.3's resolved vocabulary: the caller's storage, the
// build version that keys it, and the terminal refusal state ("REFUSAL IS
// TERMINAL"). BuildVersionOwn is the unit's own, compared under Strict.
type jsTableVocabulary struct {
	Entries         []jsTableMessageEntry
	Count           int64
	RefBits         int64
	BuildVersion    uint64
	BuildVersionOwn uint64
	Announced       bool
	Refused         bool
	MaxBytes        int64
	Strict          bool
}

// The three open verdicts the announcement reaches.
const (
	jsOpenOk uint8 = iota
	jsOpenRefused
	jsOpenDamaged
)

type jsTableReport struct {
	Verdict          uint8
	Reason           string
	Malformed        bool
	Unknown          int64
	BuildVersionPeer uint64
	BuildVersionOwn  uint64
}

func messageAnnouncementRuntime(u *ir.Unit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nexport const TableMessageBatchMax = 256;\n")
	fmt.Fprintf(&b, "export const TableMessageRefBitsHere = %d;\n", ir.TableMessageRefBits(len(ir.TableVocabulary(u))))
	fmt.Fprintf(&b, "export const TableMessageEntriesHere = %d;\n", len(ir.TableVocabulary(u)))
	fmt.Fprintf(&b, "export const TableMessageAnnouncement = new Uint8Array([%s]);\n", jsByteLiterals(ir.TableAnnouncement(u)))
	b.WriteString(jsAnnounceRuntime)
	return b.String()
}

func jsByteLiterals(data []byte) string {
	var s strings.Builder
	for _, v := range data {
		fmt.Fprintf(&s, "0x%02x,", v)
	}
	return s.String()
}

// jsAnnounceRuntime is the emitted JavaScript. It is the language's own shape
// over the reference's semantics (docs/SPEC-TABLES.md §6.1): data members
// UpperCamelCase, methods lowerCamelCase, and a refusal is a report field
// rather than an exception (the reference's `-1`).
const jsAnnounceRuntime = `
export const TableMessageOpenRefused = 1;
export class TableVocabulary {
  constructor() { this.Reset(); }
  Reset() {
    this.Entries = [];
    this.Count = 0;
    this.RefBits = 1;
    this.BuildVersion = 0n;
    this.Announced = false;
    this.Refused = false;
    this.MaxBytes = 64 * 1024;
    this.Strict = false;
  }
  Init(storage) {
    this.Entries = storage;
    this.Count = 0;
    this.RefBits = 1;
    this.BuildVersion = 0n;
    this.Announced = false;
    this.Refused = false;
    this.MaxBytes = 64 * 1024;
    this.Strict = false;
  }
}
export function AnnounceMeasure() { return TableMessageAnnouncement.length; }
export function Announce(buffer) {
  if (buffer.length < TableMessageAnnouncement.length) { return -1; }
  buffer.set(TableMessageAnnouncement);
  return TableMessageAnnouncement.length;
}
// The announcement is an ordinary form-1 file: form byte 1, a body, a zero
// terminator, and the reserved build-version and vocabulary ids as its trailer.
export function AnnounceRead(v, data, report) {
  if (v.Announced || v.Refused) {
    report.Verdict = TableMessageOpenRefused;
    report.Reason = "second_announcement";
    return false;
  }
  const ok = TableAnnounceRead(v, data, report);
  if (!ok) { v.Refused = true; }
  return ok;
}
`

// jsTableReader is a bounded view of one body and the file's id trailer.
type jsTableReader struct {
	Buffer []byte
	Offset int64
	Ids    []byte
}

func (r *jsTableReader) has(n int64) bool {
	return n >= 0 && r.Offset >= 0 && r.Offset <= int64(len(r.Buffer)) && n <= int64(len(r.Buffer))-r.Offset
}

func (r *jsTableReader) room(n uint64) bool {
	return r.has(0) && n <= uint64(int64(len(r.Buffer))-r.Offset)
}

func (r *jsTableReader) get8() uint8 { v := r.Buffer[r.Offset]; r.Offset++; return v }
func (r *jsTableReader) get32() uint32 {
	v := uint32(r.Buffer[r.Offset]) | uint32(r.Buffer[r.Offset+1])<<8 | uint32(r.Buffer[r.Offset+2])<<16 | uint32(r.Buffer[r.Offset+3])<<24
	r.Offset += 4
	return v
}
func (r *jsTableReader) get64() uint64 {
	v := uint64(r.Buffer[r.Offset]) | uint64(r.Buffer[r.Offset+1])<<8 | uint64(r.Buffer[r.Offset+2])<<16 |
		uint64(r.Buffer[r.Offset+3])<<24 | uint64(r.Buffer[r.Offset+4])<<32 | uint64(r.Buffer[r.Offset+5])<<40 |
		uint64(r.Buffer[r.Offset+6])<<48 | uint64(r.Buffer[r.Offset+7])<<56
	r.Offset += 8
	return v
}

func (r *jsTableReader) lebPair() (uint64, bool) {
	at := r.Offset
	if uint64(at) < uint64(len(r.Buffer)) {
		b := r.Buffer[at]
		if b < 128 {
			r.Offset = at + 1
			return uint64(b), true
		}
	}
	return 0, false
}

func (r *jsTableReader) lebWide() (uint64, bool) {
	at := r.Offset
	var v uint64
	for i := 0; i < 10; i++ {
		if !r.has(1) {
			break
		}
		b := r.get8()
		if i == 9 && b > 1 {
			break
		}
		v |= uint64(b&127) << (7 * i)
		if b < 128 {
			if i > 0 && b == 0 {
				break
			}
			return v, true
		}
	}
	r.Offset = at
	return 0, false
}

func (r *jsTableReader) leb() (uint64, bool) {
	if v, ok := r.lebPair(); ok {
		return v, true
	}
	return r.lebWide()
}

func (r *jsTableReader) resolve(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.Ids)/8) {
		return 0, false
	}
	at := int64((ref - 1) * 8)
	return uint64(r.Ids[at]) | uint64(r.Ids[at+1])<<8 | uint64(r.Ids[at+2])<<16 | uint64(r.Ids[at+3])<<24 |
		uint64(r.Ids[at+4])<<32 | uint64(r.Ids[at+5])<<40 | uint64(r.Ids[at+6])<<48 | uint64(r.Ids[at+7])<<56, true
}

func (r *jsTableReader) body() (jsTableReader, bool) {
	n, ok := r.leb()
	if !ok || !r.room(n) {
		return jsTableReader{}, false
	}
	sub := jsTableReader{Buffer: r.Buffer[r.Offset : r.Offset+int64(n)], Ids: r.Ids}
	r.Offset += int64(n)
	return sub, true
}

func jsTableKindBytes(kind uint8) int64 {
	switch kind {
	case 1, 2, 6, 20, 25:
		return 1
	case 3, 7, 21, 26:
		return 2
	case 4, 8, 10, 22, 27:
		return 4
	case 5, 9, 11, 23, 28:
		return 8
	case 18, 19, 24, 29:
		return 16
	}
	return 0
}

func (r *jsTableReader) skip(kind uint8) bool {
	if n := jsTableKindBytes(kind); n != 0 {
		if !r.has(n) {
			return false
		}
		r.Offset += n
		return true
	}
	switch kind {
	case 17, 30:
		_, ok := r.leb()
		return ok
	case 12, 13, 14, 16, 31, 32, 33:
		_, ok := r.body()
		return ok
	case 15:
		ref, ok := r.leb()
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		if !r.has(1) {
			return false
		}
		r.Offset++
		_, ok = r.body()
		return ok
	}
	return false
}

// endsEarly is the framing pre-walk. It takes the reader BY VALUE, because it
// only decides and must not advance the caller's cursor (the C++ reference's
// tableOpenFramed reads it on a copy).
func (r jsTableReader) endsEarly() bool {
	for {
		ref, ok := r.leb()
		if !ok {
			return false
		}
		if ref == 0 {
			return r.Offset != int64(len(r.Buffer))
		}
		if _, ok = r.resolve(ref); !ok || !r.has(1) {
			return false
		}
		if !r.skip(r.get8()) {
			return false
		}
	}
}

// jsOpen is the form-1 file open §3's reader states: form byte 1, the body,
// and the id trailer behind the body's length word.
func jsOpen(data []byte) (jsTableReader, uint8) {
	var r jsTableReader
	if len(data) == 0 {
		return r, jsOpenDamaged
	}
	if data[0] != 1 {
		return r, jsOpenRefused
	}
	if len(data) < 9 {
		return r, jsOpenDamaged
	}
	n := uint64(data[len(data)-8]) | uint64(data[len(data)-7])<<8 | uint64(data[len(data)-6])<<16 |
		uint64(data[len(data)-5])<<24 | uint64(data[len(data)-4])<<32 | uint64(data[len(data)-3])<<40 |
		uint64(data[len(data)-2])<<48 | uint64(data[len(data)-1])<<56
	if n > uint64((len(data)-9)/8) {
		return r, jsOpenDamaged
	}
	start := len(data) - 8 - int(n)*8
	r.Buffer, r.Ids = data[1:start], data[start:len(data)-8]
	return r, jsOpenOk
}

func jsOpenFramed(data []byte) (jsTableReader, uint8) {
	r, verdict := jsOpen(data)
	if verdict == jsOpenOk && r.endsEarly() {
		return r, jsOpenDamaged
	}
	return r, verdict
}

func jsMessageBits(n uint64) int64 {
	var count int64
	for n != 0 {
		count++
		n >>= 1
	}
	return count
}

func jsMessageKnownKind(k uint8) bool { return k <= 33 }

func jsMessageKindBits(k uint8) int64 {
	if k == 1 {
		return 1
	}
	switch k {
	case 2, 6, 20, 25:
		return 8
	case 3, 7, 21, 26:
		return 16
	case 4, 8, 10, 22, 27:
		return 32
	case 5, 9, 11, 23, 28:
		return 64
	case 18, 19, 24, 29:
		return 128
	}
	return -1
}

func jsMessageShapeRead(r *jsTableReader, k uint8) (s jsTableMessageShape, ok bool) {
	s.Kind = k
	s.Bits = jsMessageKindBits(k)
	switch {
	case k >= 2 && k <= 10 || k >= 18 && k <= 29:
		if !r.has(1) {
			return s, false
		}
		s.Packing = r.get8()
		if s.Packing == 0 {
			return s, true
		}
		if s.Packing == 1 && k != 10 {
			n, ok := r.leb()
			if !ok || n > uint64(s.Bits) {
				return s, false
			}
			s.Bits = int64(n)
			if k >= 18 {
				if !r.has(16) {
					return s, false
				}
				s.Base = [2]uint64{r.get64(), r.get64()}
				return s, true
			}
			base, ok := r.leb()
			if !ok {
				return s, false
			}
			if k >= 2 && k <= 5 {
				base = uint64(int64(base>>1) ^ -int64(base&1))
			}
			s.Base[0] = base
			return s, true
		}
		if s.Packing == 2 && k == 10 {
			if !r.has(12) {
				return s, false
			}
			lo := math.Float32frombits(r.get32())
			hi := math.Float32frombits(r.get32())
			res := math.Float32frombits(r.get32())
			if !(lo < hi) || !(res > 0) {
				return s, false
			}
			delta := float32(hi - lo)
			values := float32(delta / res)
			if delta-delta != 0 || values-values != 0 {
				return s, false
			}
			if !(values >= 1) {
				values = 1
			} else if values > 4294967040.0 {
				values = 4294967040.0
			}
			s.QMin = lo
			s.QDelta = delta
			s.QCount = uint32(math.Ceil(float64(values)))
			s.Bits = jsMessageBits(uint64(s.QCount))
			return s, true
		}
		return s, false
	case k == 12 || k == 33:
		n, ok := r.leb()
		if !ok || n > math.MaxInt32 {
			return s, false
		}
		s.Max = int64(n)
	case k == 14 || k == 16:
		if k == 14 {
			n, ok := r.leb()
			if !ok || n > math.MaxUint32 {
				return s, false
			}
			s.Min = int64(n)
		}
		n, ok := r.leb()
		if !ok || n > math.MaxUint32 || int64(n) < s.Min {
			return s, false
		}
		s.Max = int64(n)
	}
	return s, true
}

func jsMessageEntryRead(r *jsTableReader) (e jsTableMessageEntry, ok bool) {
	if !r.has(9) {
		return e, false
	}
	e.Id = r.get64()
	kind := r.get8()
	if !jsMessageKnownKind(kind) {
		return e, false
	}
	e.Shape, ok = jsMessageShapeRead(r, kind)
	if !ok {
		return e, false
	}
	if kind == 14 || kind == 16 {
		if !r.has(1) {
			return e, false
		}
		elem := r.get8()
		if !jsMessageKnownKind(elem) || elem == 12 || elem == 33 {
			return e, false
		}
		e.Element, ok = jsMessageShapeRead(r, elem)
		if !ok {
			return e, false
		}
		if elem == 14 || elem == 16 {
			if !r.has(1) {
				return e, false
			}
			inner := r.get8()
			if !jsMessageKnownKind(inner) || inner == 12 || inner == 33 {
				return e, false
			}
		}
	}
	return e, true
}

// jsAnnounceRead is the entry point: a refused announcement is TERMINAL, so
// any failure marks the vocabulary refused and every later call is
// `second_announcement` (docs/SPEC-TABLES.md §3.3).
func jsAnnounceRead(v *jsTableVocabulary, data []byte, report *jsTableReport) bool {
	if v.Announced || v.Refused {
		if report == nil {
			report = &jsTableReport{}
		}
		return jsRefuse(report, "second_announcement")
	}
	ok := jsAnnounceReadCore(v, data, report)
	if !ok {
		v.Refused = true
	}
	return ok
}

func jsAnnounceReadCore(v *jsTableVocabulary, data []byte, report *jsTableReport) bool {
	if report == nil {
		report = &jsTableReport{}
	}
	if v.Announced || v.Refused {
		return jsRefuse(report, "second_announcement")
	}
	r, verdict := jsOpenFramed(data)
	if verdict != jsOpenOk {
		if verdict == jsOpenRefused {
			reason := "newer_form"
			if len(data) > 0 && data[0] == 2 {
				reason = "message_form_as_file"
			}
			return jsRefuse(report, reason)
		}
		report.Malformed = true
		return false
	}
	versionCount, wordsCount := 0, 0
	var words []byte
	for {
		ref, ok := r.leb()
		if !ok {
			report.Malformed = true
			return false
		}
		if ref == 0 {
			break
		}
		id, ok := r.resolve(ref)
		if !ok || !r.has(1) {
			report.Malformed = true
			return false
		}
		kind := r.get8()
		switch id {
		case 0xfffffffffffffffe:
			if kind != 9 || !r.has(8) {
				report.Malformed = true
				return false
			}
			v.BuildVersion = r.get64()
			versionCount++
		case 0xfffffffffffffffd:
			if kind != 14 {
				report.Malformed = true
				return false
			}
			sub, ok := r.body()
			if !ok || !sub.has(1) || sub.get8() != 6 {
				report.Malformed = true
				return false
			}
			n, ok := sub.leb()
			if !ok || n > uint64(len(sub.Buffer)) || int64(n) != int64(len(sub.Buffer))-sub.Offset {
				report.Malformed = true
				return false
			}
			limit := v.MaxBytes
			if limit == 0 {
				limit = 64 * 1024
			}
			if int64(n) > limit {
				return jsRefuse(report, "vocabulary_too_large")
			}
			words = sub.Buffer[sub.Offset:]
			wordsCount++
		default:
			report.Unknown++
			if !r.skip(kind) {
				report.Malformed = true
				return false
			}
		}
	}
	if versionCount != 1 || wordsCount != 1 {
		report.Malformed = true
		return false
	}
	if v.Strict && v.BuildVersion != v.BuildVersionOwn {
		report.BuildVersionPeer = v.BuildVersion
		report.BuildVersionOwn = v.BuildVersionOwn
		return jsRefuse(report, "build_version_mismatch")
	}
	src := jsTableReader{Buffer: words}
	count := int64(0)
	nodeSeen := false
	for src.Offset < int64(len(words)) {
		if count >= int64(len(v.Entries)) {
			return jsRefuse(report, "vocabulary_too_large")
		}
		e, ok := jsMessageEntryRead(&src)
		if !ok || e.Id == 0xfffffffffffffffd || e.Id == 0xfffffffffffffffe || e.Id == 0xffffffffffffffff && nodeSeen {
			report.Malformed = true
			return false
		}
		if e.Id == 0xffffffffffffffff {
			nodeSeen = true
		}
		for i := int64(0); i < count; i++ {
			if v.Entries[i] == e {
				report.Malformed = true
				return false
			}
		}
		v.Entries[count] = e
		count++
	}
	v.Count = count
	v.RefBits = jsMessageBits(uint64(count))
	if v.RefBits == 0 {
		v.RefBits = 1
	}
	v.Announced = true
	return true
}

func jsRefuse(report *jsTableReport, reason string) bool {
	report.Verdict = jsOpenRefused
	report.Reason = reason
	return false
}
