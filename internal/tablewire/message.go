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

// Vocabulary is ONE ANNOUNCEMENT's id table (docs/SPEC-TABLES.md §3.3): the
// entries it carried, whole, with slot `1` the reserved build-version id, slot
// `2` the reserved records id, and slots `3` and up the peer's vocabulary,
// under one numbering — beside the per-entry RECORDS that say what a field
// header under each id spells.
//
// THE SCOPE IS THE ANNOUNCEMENT'S, and nothing here assumes a transport. The
// announcement is delivered ONCE, reliably, before the first body, and is
// never re-announced; the bodies then ride any channel, one self-delimiting
// batch to a datagram on an unreliable one. A peer holds one of these per
// direction, the one it writes with and the one it reads with, and neither is
// the other's. A body from a peer that never announced is refused by name.
type Vocabulary struct {
	// MaxEntries is the receiver's declared bound, and an announcement above
	// it is refused by name before an entry is touched. Zero means the
	// conforming default, [ir.TableVocabularyMaxEntries].
	MaxEntries int

	ids          []uint64
	records      []ir.TableMessageDescriptor
	buildVersion uint64
	announced    bool
}

// Records is the per-entry array in slot order: slot `k`'s record is
// `Records()[k-1]`. It is what a reader skips an id it cannot name by, on a
// body that carries no kind byte.
func (v *Vocabulary) Records() []ir.TableMessageDescriptor { return v.records }

// RefBits is the width of a reference on this table's bodies:
// `bits_required(entries)`.
func (v *Vocabulary) RefBits() int { return ir.TableMessageRefBits(len(v.ids)) }

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
// — with TWO STRICT CHECKS over the body: the reserved build-version field
// present, exactly once, under kind `9`, eight bytes wide, and the reserved
// RECORDS field present, exactly once, under kind `12`, carrying exactly one
// fixed-width record per entry. Every other field is an ordinary field under
// §4's tolerance, so an unknown one is skipped and counted and the
// announcement can GAIN a field in a later minor without a lockstep redeploy.
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
	version, records, read := announcementFields(body, ids, report)
	if !read {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	v.ids = ids
	v.records = records
	v.buildVersion = version
	v.announced = true
	return nil
}

// announcementFields walks the announcement's body under §4's ordinary
// tolerance and applies the two strict checks: the reserved build-version
// field present EXACTLY ONCE under kind `9` at eight bytes, and the reserved
// RECORDS field present EXACTLY ONCE under kind `12` at exactly one record an
// entry. An unknown field beside them is skipped and counted like any other,
// which is what makes the announcement a table body rather than a fixed
// header.
func announcementFields(body []byte, ids []uint64, report *tabletext.Report) (uint64, []ir.TableMessageDescriptor, bool) {
	r := &wireReader{buf: body, report: report, ids: ids}
	version, seenVersion, seenRecords := uint64(0), 0, 0
	var records []ir.TableMessageDescriptor
	for {
		ref, ok := r.leb()
		if !ok {
			report.Malformed = true
			return 0, nil, false
		}
		if ref == 0 {
			return version, records, seenVersion == 1 && seenRecords == 1
		}
		id, named := r.id(ref)
		if !named || !r.has(1) {
			report.Malformed = true
			return 0, nil, false
		}
		kind := r.u8()
		switch id {
		case ir.TableBuildVersionWireId:
			if kind != ir.TableKindU64 || !r.has(8) {
				return 0, nil, false
			}
			version = r.u64()
			seenVersion++
		case ir.TableMessageRecordsWireId:
			if kind != ir.TableKindString {
				return 0, nil, false
			}
			n, ok := r.leb()
			if !ok || n != uint64(ir.TableMessageRecordBytes*len(ids)) || !r.has(int(n)) {
				return 0, nil, false
			}
			at := r.off
			r.off += int(n)
			records = make([]ir.TableMessageDescriptor, len(ids))
			for i := range records {
				records[i] = ir.TableMessageDecodeDescriptor(body[at+ir.TableMessageRecordBytes*i:])
			}
			seenRecords++
		default:
			report.Unknown++
			if !r.skip(kind) {
				report.Malformed = true
				return 0, nil, false
			}
		}
	}
}

// Refused reports whether an error from [Decode], [DecodeMessages] or
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
