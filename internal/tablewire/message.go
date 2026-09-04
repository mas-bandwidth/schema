// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a file carries its own id
// table and a MESSAGE STREAM announces one and then carries none.
//
// A form-`2` wire is TWO PARTS, the FORM BYTE and the ROOT BODY, and its
// references resolve against the CONNECTION's table rather than a trailer of
// its own. The table is the UNIT's whole vocabulary in a compiler-settled
// order, announced once a connection per direction by an ordinary form-`1`
// file, and everything else about the wire — the body's framing, the elision
// rules, §4's tolerance and every malformed rule — is §3's unchanged.
package tablewire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageReason is why a message or an announcement was REFUSED — a verdict
// that decodes nothing, moves none of §4's five counters and never reports
// damage, on the form byte's own terms (docs/SPEC-TABLES.md §3, §3.3).
//
// It is deliberately NOT the cooked form's `TableRefuseReason` vocabulary
// (§7.4): a caller meeting one of those has been refused a FILE and falls back
// or gives up, and a caller meeting one of these has been refused a MESSAGE on
// a connection, which is a different recovery with a different owner.
type MessageReason int

// The refusal reasons of the message path (docs/SPEC-TABLES.md §3.3, §11).
const (
	// ReasonNewerForm is the FORM BYTE's own, and it is why the verdict
	// existed before this form did: a form byte this reader does not carry.
	ReasonNewerForm MessageReason = iota
	// ReasonNoVocabulary is a form-`2` message that arrived before the
	// announcement, or after one that was refused. Nothing is decoded, the
	// reader holds no table for the connection, and `malformed` does not fire.
	ReasonNoVocabulary
	// ReasonSecondAnnouncement is a second announcement on a connection. It
	// does not replace the table, does not amend it and changes nothing: the
	// peer is not speaking this form and the receiver closes the connection.
	ReasonSecondAnnouncement
	// ReasonVocabularyTooLarge is an announcement whose entry count is above
	// the receiver's declared maximum. The count is a fixed little-endian u64
	// at the end of the announcement, so it is read, compared and refused
	// without touching an entry.
	ReasonVocabularyTooLarge
	// ReasonMessageFormAsFile is a form-`2` wire where a FILE was expected. A
	// message stored on its own is not readable, because its table is
	// somewhere else.
	ReasonMessageFormAsFile
)

func (r MessageReason) String() string {
	switch r {
	case ReasonNewerForm:
		return "newer_form"
	case ReasonNoVocabulary:
		return "no_vocabulary"
	case ReasonSecondAnnouncement:
		return "second_announcement"
	case ReasonVocabularyTooLarge:
		return "vocabulary_too_large"
	case ReasonMessageFormAsFile:
		return "message_form_as_file"
	}
	return fmt.Sprintf("MessageReason(%d)", int(r))
}

// MessageRefusal is a message path refusal, carrying the reason by name and
// the build version the connection's table was announced under where one was
// ever read. It is returned rather than folded into the report, exactly as
// [FormRefusal] is, and a caller must not carry on as if it had a value.
type MessageRefusal struct {
	Reason       MessageReason
	BuildVersion uint64 // 0 where no announcement was ever read
}

func (e *MessageRefusal) Error() string {
	if e.BuildVersion != 0 {
		return fmt.Sprintf("the message was refused: %s, on the connection announced at build version %016x (docs/SPEC-TABLES.md §3.3)", e.Reason, e.BuildVersion)
	}
	return fmt.Sprintf("the message was refused: %s (docs/SPEC-TABLES.md §3.3)", e.Reason)
}

// Vocabulary is ONE DIRECTION of ONE CONNECTION's id table
// (docs/SPEC-TABLES.md §3.3): the entries an announcement carried, whole, with
// slot `1` the reserved build-version id and slots `2` and up the peer's
// vocabulary, under one numbering.
//
// A peer holds two of these for a connection, the one it writes with and the
// one it reads with, and neither is the other's. A restart is a new connection
// with empty tables, and nothing is cached across connections.
type Vocabulary struct {
	// MaxEntries is the receiver's declared bound, and an announcement above
	// it is refused by name before an entry is touched. Zero means the
	// conforming default, [ir.TableVocabularyMaxEntries].
	MaxEntries int

	ids          []uint64
	buildVersion uint64
	announced    bool
}

// Announced reports whether this direction's table is set. Only the FIRST
// announcement sets it, and a refused announcement sets none.
func (v *Vocabulary) Announced() bool { return v.announced }

// BuildVersion is the build the table was announced under. It KEYS the table
// and gates nothing: peers connect on the protocol id and may differ in build
// version (§20.5), and a receiver never refuses a message because the
// announced build version is not its own.
func (v *Vocabulary) BuildVersion() uint64 { return v.buildVersion }

// Entries is the table, whole, in slot order: slot `k` is `Entries()[k-1]`.
func (v *Vocabulary) Entries() []uint64 { return v.ids }

func (v *Vocabulary) bound() int {
	if v.MaxEntries > 0 {
		return v.MaxEntries
	}
	return ir.TableVocabularyMaxEntries
}

// Announce is the unit's own ID TABLE MESSAGE, byte for byte — a form-`1`
// file, so it needs no second form byte, no envelope and no rule of its own.
// Every byte is settled by the compiler, which is why a backend may emit it as
// a constant.
func Announce(u *ir.Unit) []byte { return ir.TableAnnouncement(u) }

// AnnounceRead reads an announcement into this direction's table
// (docs/SPEC-TABLES.md §3.3).
//
// The BOUND IS CHECKED BEFORE ANYTHING IS ALLOCATED: the entry count is a
// fixed little-endian u64 at the end, so it is read, compared and refused
// without touching an entry. Everything after that is §3's ordinary FILE read
// — every malformed rule already covers the announcement, because it IS a file
// — with EXACTLY ONE STRICT CHECK over the body: the reserved build-version
// field present, exactly once, under kind `9`, eight bytes wide. Every other
// field is an ordinary field under §4's tolerance, so an unknown one is
// skipped and counted and the announcement can GAIN a field in a later minor
// without a lockstep redeploy.
//
// A refused announcement sets NO TABLE, and a malformed one sets none either.
func (v *Vocabulary) AnnounceRead(data []byte, report *tabletext.Report) error {
	// THE FIRST ANNOUNCEMENT SETS THE TABLE, AND IT IS THE ONLY ONE THAT CAN.
	// A second does not replace it, does not amend it and changes nothing.
	if v.announced {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonSecondAnnouncement, BuildVersion: v.buildVersion}
	}
	if len(data) < 1 {
		report.Malformed = true
		return nil
	}
	if data[0] != ir.TableWireForm {
		report.Refused = true
		if data[0] == ir.TableWireMessageForm {
			// an ANNOUNCEMENT is a file, so a message where one was expected
			// is the same refusal a file reader gives it
			return &MessageRefusal{Reason: ReasonMessageFormAsFile}
		}
		return &MessageRefusal{Reason: ReasonNewerForm}
	}
	// A TABLE PAST THE BOUND IS REFUSED BEFORE ANYTHING IS ALLOCATED.
	if len(data) < 9 {
		report.Malformed = true
		return nil
	}
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	if count > uint64(v.bound()) {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonVocabularyTooLarge}
	}
	body, ids, ok := trailer(data)
	if !ok {
		report.Malformed = true
		return nil
	}
	if end, terminated := bodyExtent(body, ids); terminated && end != len(body) {
		report.Malformed = true
		return nil
	}
	version, read := announcementBuildVersion(body, ids, report)
	if !read {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	v.ids = ids
	v.buildVersion = version
	v.announced = true
	return nil
}

// announcementBuildVersion walks the announcement's body under §4's ordinary
// tolerance and applies the one strict check: the reserved build-version field
// present, EXACTLY ONCE, under kind `9`, eight bytes wide. An unknown field
// beside it is skipped and counted like any other, which is what makes the
// announcement a table body rather than a fixed header.
func announcementBuildVersion(body []byte, ids []uint64, report *tabletext.Report) (uint64, bool) {
	r := &wireReader{buf: body, report: report, ids: ids}
	version, seen := uint64(0), 0
	for {
		ref, ok := r.leb()
		if !ok {
			report.Malformed = true
			return 0, false
		}
		if ref == 0 {
			return version, seen == 1
		}
		id, named := r.id(ref)
		if !named || !r.has(1) {
			report.Malformed = true
			return 0, false
		}
		kind := r.u8()
		if id != ir.TableBuildVersionWireId {
			report.Unknown++
			if !r.skip(kind) {
				report.Malformed = true
				return 0, false
			}
			continue
		}
		if kind != ir.TableKindU64 || !r.has(8) {
			return 0, false
		}
		version = r.u64()
		seen++
	}
}

// EncodeMessage is one instance's FORM-`2` wire: the form byte and the root
// body, and nothing else (docs/SPEC-TABLES.md §3.3). The body ends at its own
// zero reference, as it does in a file, and there is no trailer — the
// message's last byte is the body's terminator.
//
// Every reference is a SLOT of the unit's vocabulary, which is a compile-time
// fact, so nothing is interned and no id is written.
func EncodeMessage(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	g, err := Number(m, inst)
	if err != nil {
		return nil, err
	}
	e := &encoder{m: m, g: g, ids: vocabularyIdTable(m.Unit)}
	fields, err := encodeBodyFields(e, inst)
	if err != nil {
		return nil, err
	}
	if len(g.Records()) > 0 {
		fields, err = e.appendNodeTable(fields, g)
		if err != nil {
			return nil, err
		}
	}
	if e.ids.missing != 0 {
		// an id the walk reached that the unit's own vocabulary does not
		// spell is a compiler defect, never a wire one: the vocabulary is the
		// closure's whole id set by construction (§3.3)
		return nil, fmt.Errorf("the unit's vocabulary names no slot for id %016x — the message form's table is the closure's whole id set (docs/SPEC-TABLES.md §3.3)", e.ids.missing)
	}
	out := make([]byte, 0, 1+len(fields)+1)
	out = append(out, ir.TableWireMessageForm)
	out = append(out, fields...)
	return append(out, 0), nil
}

// vocabularyIdTable is the writer's half under the MESSAGE form: the slot an
// id takes is settled by the compiler, so `ref` is a lookup that answers a
// constant and `mark`/`rollback` have nothing to undo — an elided field costs
// no entry because there are no entries to cost.
func vocabularyIdTable(u *ir.Unit) *idTable {
	slots := map[uint64]uint64{}
	for i, id := range ir.TableVocabulary(u) {
		slots[id] = uint64(i + 1)
	}
	return &idTable{fixed: slots}
}

// DecodeMessage fills one instance from a FORM-`2` message, resolving every
// reference against the CONNECTION's table (docs/SPEC-TABLES.md §3.3).
//
// The error is a REFUSAL — a wire this reader will not decode at all — and is
// returned rather than folded into the report; false with a nil error is
// framing damage past the point the walk could continue, and the instance
// keeps what it decoded.
func DecodeMessage(m *tabletext.Model, inst *tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	// THE FORM BYTE IS READ FIRST, before anything else, so a message that is
	// both a form this reader does not carry and damaged is a refusal and
	// never damage (§3).
	if len(data) < 1 {
		report.Malformed = true
		return false, nil
	}
	if data[0] != ir.TableWireMessageForm {
		report.Refused = true
		return false, &MessageRefusal{Reason: ReasonNewerForm}
	}
	// WHAT A PEER DOES WHEN IT HAS NO TABLE FOR THE CONNECTION: IT REFUSES THE
	// MESSAGE BY NAME. Nothing is decoded, no counter moves and `malformed`
	// does not fire.
	if v == nil || !v.announced {
		report.Refused = true
		return false, &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	// A form-`2` wire has NO TRAILER, so the body runs to the last byte and
	// there is no stray-byte rule between a terminator and a first entry: the
	// message's last byte IS the body's terminator, and anything else is
	// §3's ordinary framing damage on the root body.
	body := data[1:]
	if !ir.VariableTables(m.Unit)[inst.Def.Name] {
		r := &wireReader{buf: body, report: report, m: m, ids: v.ids}
		return r.body(inst), nil
	}
	return decodeVariable(m, inst, body, v.ids, report)
}

// Refused reports whether an error from [Decode], [DecodeMessage] or
// [Vocabulary.AnnounceRead] is a REFUSAL VERDICT rather than a failure to
// produce an answer: nothing was decoded, no counter moved, and no damage is
// reported (docs/SPEC-TABLES.md §3, §3.3). The refusal is the answer, so a
// caller reports it and carries on rather than propagating an error.
func Refused(err error) bool {
	if _, form := errors.AsType[*FormRefusal](err); form {
		return true
	}
	_, message := errors.AsType[*MessageRefusal](err)
	return message
}

// Trailer splits a FILE wire into its root body and its id table
// (docs/SPEC-TABLES.md §3): the form byte, the body, and the trailer the
// reader finds from the END. ok is false where the trailer cannot be read
// whole, which is malformed on every path that reads one.
//
// It is the file form's half of what [Resolve] needs, and the message form's
// half is the connection's table, which is why the two forms can be compared
// under RESOLUTION at all (§3.3).
func Trailer(data []byte) (body []byte, ids []uint64, ok bool) { return trailer(data) }
