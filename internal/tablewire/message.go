// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a file carries its own id
// table and a MESSAGE STREAM announces one and then carries none.
//
// A form-`2` wire is THREE PARTS, the FORM BYTE, the BODY COUNT and the
// BODIES as one continuous bit stream, and its references resolve against the
// CONNECTION's table rather than a trailer of its own. The table is the UNIT's
// whole vocabulary in a compiler-settled order, announced once a connection
// per direction by an ordinary form-`1` file, and everything else about the
// wire, the elision rules, §4's tolerance and every malformed rule, is §3's
// unchanged.
package tablewire

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageReason is why a message or an announcement was REFUSED. A refusal is
// a verdict that decodes nothing, moves none of §4's five counters and never
// reports damage, on the form byte's own terms (docs/SPEC-TABLES.md §3, §3.3).
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
	// does not replace the table, does not amend it and changes nothing. The
	// library returns the refusal and nothing more; whether the connection
	// closes is the application's own call.
	ReasonSecondAnnouncement
	// ReasonVocabularyTooLarge covers the vocabulary's TWO bounds: an entry
	// count above the receiver's declared capacity, and a vocabulary field
	// longer than the byte bound. The byte bound is read off the field's own
	// length before an entry is touched, and the entry bound refuses at the
	// entry that passes it, so neither reads a vocabulary it has refused.
	ReasonVocabularyTooLarge
	// ReasonMessageFormAsFile is a form-`2` wire where a FILE was expected. A
	// message stored on its own is not readable, because its table is
	// somewhere else.
	ReasonMessageFormAsFile
	// ReasonBatchTooLarge covers the batch's two bounds (docs/SPEC-TABLES.md
	// §3.3): `M` above 256 on the WRITE side, where the count's width is a
	// wire constant, and `M` above the caller's capacity on the READ side,
	// where nothing is decoded and the returned count says what the wire
	// carries. A caller reads the reason and then reads the two numbers.
	ReasonBatchTooLarge
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
	case ReasonBatchTooLarge:
		return "batch_too_large"
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
// entries it carried, whole, in the projection's order, slot `k` being
// `Entries()[k-1]`. The announcement's own two reserved ids, the build version
// and the vocabulary, are its transport and take NO slot; the reserved
// node-table id takes exactly one, because a pointered body names the node
// table through it.
//
// THE SCOPE IS THE ANNOUNCEMENT'S, and nothing here assumes a transport. The
// announcement is delivered ONCE, reliably, before the first body, and is
// never re-announced; the bodies then ride any channel, one self-delimiting
// batch to a datagram on an unreliable one. A peer holds one of these per
// direction, the one it writes with and the one it reads with, and neither is
// the other's. A body from a peer that never announced is refused by name.
type Vocabulary struct {
	// THE BOUND IS TWO NUMBERS, because an entry is no longer a fixed width:
	// an announcement above either is refused by name before an entry is
	// touched. Zero means the conforming default of each
	// ([ir.TableVocabularyMaxEntries], [ir.TableVocabularyMaxBytes]).
	MaxEntries int
	MaxBytes   int

	entries      []ir.TableVocabularyEntry
	buildVersion uint64
	announced    bool
	// refused is the terminal state a refused first announcement leaves the
	// connection in: no vocabulary for its life, and every announcement after
	// it refused as second_announcement (§3.3)
	refused bool
}

// Entries is the vocabulary, whole, in slot order: slot `k` is
// `Entries()[k-1]`. Each is an id beside a kind beside a SHAPE, which is what
// a reader skips an entry it cannot name by, on a body that carries no kind
// byte, and decodes one whose declaration has moved by.
func (v *Vocabulary) Entries() []ir.TableVocabularyEntry { return v.entries }

// RefBits is the width of a reference against this vocabulary:
// `bits_required(0, E)`.
func (v *Vocabulary) RefBits() int { return ir.TableMessageRefBits(len(v.entries)) }

// Announced reports whether this direction's table is set. Only the FIRST
// announcement sets it, and a refused announcement sets none.
func (v *Vocabulary) Announced() bool { return v.announced }

// BuildVersion is the build the table was announced under. It KEYS the table
// and gates nothing: peers connect on the protocol id and may differ in build
// version (§20.5), and a receiver never refuses a message because the
// announced build version is not its own.
func (v *Vocabulary) BuildVersion() uint64 { return v.buildVersion }

func (v *Vocabulary) bound() int {
	if v.MaxEntries > 0 {
		return v.MaxEntries
	}
	return ir.TableVocabularyMaxEntries
}

// MaxBytes is the receiver's second declared bound: the vocabulary field's own
// byte length, checked from that length before an entry is touched. Zero means
// the conforming default, [ir.TableVocabularyMaxBytes].
func (v *Vocabulary) byteBound() int {
	if v.MaxBytes > 0 {
		return v.MaxBytes
	}
	return ir.TableVocabularyMaxBytes
}

// Announce is the unit's own ID TABLE MESSAGE, byte for byte. It is a form-`1`
// file, so it needs no second form byte, no envelope and no rule of its own.
// Every byte is settled by the compiler, which is why a backend may emit it as
// a constant.
func Announce(u *ir.Unit) []byte { return ir.TableAnnouncement(u) }

// AnnounceRead reads an announcement into this direction's vocabulary
// (docs/SPEC-TABLES.md §3.3).
//
// THE TWO BOUNDS ARE CHECKED BEFORE ANYTHING IS ALLOCATED: the vocabulary
// field's own length is compared against the byte bound before an entry is
// touched, and the entry count is refused at the entry that passes the
// capacity. Everything else is §3's ordinary FILE read, since every malformed
// rule already covers the announcement because it IS a file, with TWO STRICT
// CHECKS over the body: the BUILD VERSION present, exactly once, under kind
// `9`, eight bytes wide, and the VOCABULARY present, exactly once, under kind
// `14` over element kind `6`. Every other field is ordinary and tolerant, so
// an unknown one is skipped and counted and the announcement can GAIN a field
// in a later minor without a lockstep redeploy. A FAILED STRICT CHECK IS
// MALFORMED: the two checks are the two facts the body must carry, so a body
// without them is not an announcement.
//
// A HOSTILE SHAPE IS A HOSTILE WIDTH, and every width is checked here and
// never again: a `bits` above 128, a `min` or a `max` above what its kind can
// hold, an array whose `min` exceeds its `max`, an element kind outside the
// closed set, an element kind of `12` or `33`, two entries that agree on all
// three parts, and a shape running past the vocabulary field's own length are
// each malformed on the announcement, which is a file and takes §3's rule that
// a wire it cannot read whole is malformed whole.
//
// A refused announcement sets NO VOCABULARY, and a malformed one sets none
// either. The BUILD VERSION is kept the moment it is read, refusal or not, so
// that a refusal on this connection names it.
func (v *Vocabulary) AnnounceRead(data []byte, report *tabletext.Report) error {
	// THE FIRST ANNOUNCEMENT SETS THE VOCABULARY, AND IT IS THE ONLY ONE THAT
	// CAN. A second does not replace it, does not amend it and changes nothing.
	// REFUSAL IS TERMINAL: a connection whose first announcement was refused,
	// for any reason, carries no vocabulary for its life, and every
	// announcement after it is refused as second_announcement whether or not
	// the first set anything, so a peer cannot buy a second resolve by having
	// its first refused (§3.3).
	if v.announced || v.refused {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonSecondAnnouncement, BuildVersion: v.buildVersion}
	}
	err := v.announceRead(data, report)
	if !v.announced {
		v.refused = true
	}
	return err
}

// announceRead is the one read a connection gets.
func (v *Vocabulary) announceRead(data []byte, report *tabletext.Report) error {
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
	if len(data) < 9 {
		report.Malformed = true
		return nil
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
	version, vocabulary, read, oversize := announcementFields(body, ids, v.byteBound(), report)
	// THE BUILD VERSION IS KEPT THE MOMENT IT IS READ, refusal or not, so that
	// a refusal on this connection NAMES IT (§3.3). It is not the vocabulary,
	// and a refused announcement still sets none.
	v.buildVersion = version
	if oversize {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonVocabularyTooLarge, BuildVersion: version}
	}
	// A FAILED STRICT CHECK IS DAMAGE, not a refusal (§3.3). The announcement
	// is a table body and the two checks are the two facts that body must
	// carry: a build version that is absent, doubled, under another kind or
	// not eight bytes wide, and a vocabulary that is absent, doubled or not a
	// run of bytes, each say the bytes are not an announcement rather than
	// that this peer declined to announce.
	if !read {
		report.Malformed = true
		return nil
	}
	entries, good := decodeVocabulary(vocabulary)
	if !good {
		report.Malformed = true
		return nil
	}
	if len(entries) > v.bound() {
		report.Refused = true
		return &MessageRefusal{Reason: ReasonVocabularyTooLarge, BuildVersion: version}
	}
	v.entries = entries
	v.announced = true
	return nil
}

// announcementFields walks the announcement's body under §4's ordinary
// tolerance and applies the two strict checks. `oversize` is the byte bound,
// read off the vocabulary field's own length before an entry is touched.
func announcementFields(body []byte, ids []uint64, byteBound int, report *tabletext.Report) (version uint64, vocabulary []byte, read, oversize bool) {
	r := &wireReader{buf: body, report: report, ids: ids}
	seenVersion, seenVocabulary := 0, 0
	for {
		ref, ok := r.leb()
		if !ok {
			report.Malformed = true
			return 0, nil, false, false
		}
		if ref == 0 {
			return version, vocabulary, seenVersion == 1 && seenVocabulary == 1, false
		}
		id, named := r.id(ref)
		if !named || !r.has(1) {
			report.Malformed = true
			return 0, nil, false, false
		}
		kind := r.u8()
		switch id {
		case ir.TableBuildVersionWireId:
			if kind != ir.TableKindU64 || !r.has(8) {
				return 0, nil, false, false
			}
			version = r.u64()
			seenVersion++
		case ir.TableMessageVocabularyWireId:
			// the VOCABULARY: kind 14 over element kind 6, which is §3's
			// spelling for an opaque run of bytes
			n, ok := r.leb()
			if kind != ir.TableKindArray || !ok || !r.has(int(n)) {
				return 0, nil, false, false
			}
			at, end := r.off, r.off+int(n)
			r.off = end
			if at >= end || body[at] != ir.TableKindU8 {
				return 0, nil, false, false
			}
			at++
			length, next, good := readLeb(body, at)
			if !good || next+int(length) != end {
				return 0, nil, false, false
			}
			if int(length) > byteBound {
				return 0, nil, false, true
			}
			vocabulary = body[next : next+int(length)]
			seenVocabulary++
		default:
			report.Unknown++
			if !r.skip(kind) {
				report.Malformed = true
				return 0, nil, false, false
			}
		}
	}
}

// decodeVocabulary reads the entries back: an id, a kind, and the shape the
// kind names, until the bytes are consumed. A width no kind can hold, an array
// whose `min` exceeds its `max`, an element kind outside the closed set and a
// shape running past the field are each malformed here, once, and never again.
//
// SO ARE THE RESERVED IDS WHERE THEY DO NOT BELONG (§3.3): the announcement's
// own two ids never take a slot, and the node-table id takes exactly one, so a
// vocabulary carrying 0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD or a SECOND
// 0xFFFFFFFFFFFFFFFF is malformed whole and sets nothing.
func decodeVocabulary(in []byte) ([]ir.TableVocabularyEntry, bool) {
	var out []ir.TableVocabularyEntry
	nodeTable := 0
	for at := 0; at < len(in); {
		if at+9 > len(in) {
			return nil, false
		}
		e := ir.TableVocabularyEntry{Id: binary.LittleEndian.Uint64(in[at:]), Kind: in[at+8]}
		at += 9
		switch e.Id {
		case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
			return nil, false
		case ir.TableNodeWireId:
			nodeTable++
			if nodeTable > 1 {
				return nil, false
			}
		}
		if !ir.TableMessageKnownKind(e.Kind) {
			return nil, false
		}
		shape, n, ok := ir.DecodeShape(in[at:], e.Kind)
		if !ok {
			return nil, false
		}
		e.Shape = shape
		// A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE, so two entries that
		// agree on the id, the kind and every fact of the shape are malformed
		// (§3.3): no writer this wire has produces one, and a reader that took
		// it would carry two slots that name one thing. The scan is quadratic
		// in the entry count, and the entry count is bounded above at 4096, so
		// it is at most eight million compares on a path that runs ONCE a
		// connection and never again.
		for _, seen := range out {
			if seen.Id == e.Id && seen.Kind == e.Kind && seen.Key() == e.Key() {
				return nil, false
			}
		}
		at += n
		out = append(out, e)
	}
	return out, true
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
