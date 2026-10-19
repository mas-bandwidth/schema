package gotable

const tableRefusalSource = `
// TableRefuseReason is the typed error shared by file and accelerator failures.
// A successful call returns nil; a failure returns its first failing clause.
type TableRefuseReason uint8
const (
TableRefuseOk TableRefuseReason = iota
TableRefuseNotACook
TableRefuseForeignOrder
TableRefuseWrongBuildVersion
TableRefuseReservedNotZero
TableRefuseBadAlignment
TableRefuseTruncated
TableRefuseUnalignedBase
TableRefuseBadLayout
TableRefuseUnknownForm
TableRefuseCountOverLength
TableRefuseCountOverExtentCap
TableRefuseBlobOverSizeCap
TableRefuseDataCycle
TableRefuseInvalidValue
TableRefuseAllocationFailed
TableRefuseWireDamaged
TableRefuseMalformed
TableRefuseClamped
TableRefuseKindMismatch
TableRefuseBase64Damaged
TableRefuseBase64NotCanonical
TableRefuseIntegrityMismatch
TableRefuseNonFinite
// TableRefuseBlobShared: one blob record named from two slots (docs/SPEC-TABLES.md
// §2.5, §16.7). Every writer refuses to emit it; the strict read refuses it.
TableRefuseBlobShared
// The fixed form's own refusals (docs/SPEC-TABLES.md §6.5, §21.3,
// docs/FIXED-FORM-ALGORITHM.md §5.3), folded into the one refusal enum: every
// fixed-form refusal name is a TableRefuseReason member, appended after
// TableRefuseBlobShared so no existing value renumbers, in §6.5's table order so
// names, order and spelling agree in all ports. Each member's Error() is its
// snake_case name, and TableReport.Err() maps a fixed report whose Reason is
// that name to the member, so errors.Is(err, TableRefuseLayoutUnknown) holds
// for a fixed load that refused. The form-byte refusals keep answering
// TableRefuseUnknownForm, as §6.5's strict error identity requires.
TableRefusePreviousForm
TableRefuseNewerForm
TableRefuseMessageFormAsFile
TableRefuseLayoutUnknown
TableRefuseLayoutUnsupported
TableRefuseLayoutNewer
TableRefuseLayoutMalformed
TableRefuseLayoutCountMismatch
TableRefuseLayoutKindUnknown
TableRefuseLayoutKindInvalid
TableRefuseLayoutSizeMismatch
TableRefuseLayoutTreeUnclosed
TableRefuseLayoutTooDeep
TableRefuseLayoutRecordTooLarge
TableRefusePlanTooLarge
TableRefuseNoLayout
TableRefuseBatchTooLarge
TableRefuseTextIllFormed
TableRefuseCountOverBound
TableRefuseLengthOverBound
)
func(r TableRefuseReason) Error()string{switch r {
case TableRefuseOk:return "ok"
case TableRefuseNotACook:return "not_a_cook"
case TableRefuseForeignOrder:return "foreign_order"
case TableRefuseWrongBuildVersion:return "wrong_build_version"
case TableRefuseReservedNotZero:return "reserved_not_zero"
case TableRefuseBadAlignment:return "bad_alignment"
case TableRefuseTruncated:return "truncated"
case TableRefuseUnalignedBase:return "unaligned_base"
case TableRefuseBadLayout:return "bad_layout"
case TableRefuseUnknownForm:return "unknown_form"
case TableRefuseCountOverLength:return "count_over_length"
case TableRefuseCountOverExtentCap:return "count_over_extent_cap"
case TableRefuseBlobOverSizeCap:return "blob_over_size_cap"
case TableRefuseDataCycle:return "data_cycle"
case TableRefuseInvalidValue:return "invalid_value"
case TableRefuseAllocationFailed:return "allocation_failed"
case TableRefuseWireDamaged:return "wire_damaged"
case TableRefuseMalformed:return "malformed"
case TableRefuseClamped:return "clamped"
case TableRefuseKindMismatch:return "kind_mismatch"
case TableRefuseBase64Damaged:return "base64_damaged"
case TableRefuseBase64NotCanonical:return "base64_not_canonical"
case TableRefuseIntegrityMismatch:return "integrity_mismatch"
case TableRefuseNonFinite:return "non_finite"
case TableRefuseBlobShared:return "blob_shared"
case TableRefusePreviousForm:return "previous_form"
case TableRefuseNewerForm:return "newer_form"
case TableRefuseMessageFormAsFile:return "message_form_as_file"
case TableRefuseLayoutUnknown:return "layout_unknown"
case TableRefuseLayoutUnsupported:return "layout_unsupported"
case TableRefuseLayoutNewer:return "layout_newer"
case TableRefuseLayoutMalformed:return "layout_malformed"
case TableRefuseLayoutCountMismatch:return "layout_count_mismatch"
case TableRefuseLayoutKindUnknown:return "layout_kind_unknown"
case TableRefuseLayoutKindInvalid:return "layout_kind_invalid"
case TableRefuseLayoutSizeMismatch:return "layout_size_mismatch"
case TableRefuseLayoutTreeUnclosed:return "layout_tree_unclosed"
case TableRefuseLayoutTooDeep:return "layout_too_deep"
case TableRefuseLayoutRecordTooLarge:return "layout_record_too_large"
case TableRefusePlanTooLarge:return "plan_too_large"
case TableRefuseNoLayout:return "no_layout"
case TableRefuseBatchTooLarge:return "batch_too_large"
case TableRefuseTextIllFormed:return "text_ill_formed"
case TableRefuseCountOverBound:return "count_over_bound"
case TableRefuseLengthOverBound:return "length_over_bound"
};return "invalid refusal reason"}
// tableFixedLoadTruncated is the typed error a FIXED LOAD's framing damage
// answers (docs/SPEC-TABLES.md §6.5 strict error identity, §3.4): the report is
// malformed by class and truncated by name, so a caller matching either sees
// it. It is report-specific, not a rule on the enum: the fixed writer's short
// output buffer returns TableRefuseTruncated itself and never matches malformed.
type tableFixedLoadTruncated struct{}
func(tableFixedLoadTruncated) Error()string{return TableRefuseTruncated.Error()}
func(tableFixedLoadTruncated) Is(target error)bool{return target==TableRefuseTruncated||target==TableRefuseMalformed}
func tableRefuseError(reason TableRefuseReason)error{if reason==TableRefuseOk{return nil};return reason}
// TableBuildVersionMismatch is the typed error a strict announcement refusal
// reports (docs/SPEC-TABLES.md §3.3): the peer's build version and the
// reader's own, each a 64-bit number.
type TableBuildVersionMismatch struct { Peer, Own uint64 }
func(m TableBuildVersionMismatch) Error() string {const digits="0123456789abcdef";var peer,own [16]byte;p,o:=m.Peer,m.Own;for i:=15;i>=0;i-- {peer[i]=digits[p&0xf];p>>=4};for i:=15;i>=0;i-- {own[i]=digits[o&0xf];o>>=4};return "build_version_mismatch: peer 0x"+string(peer[:])+" own 0x"+string(own[:])}
// TableMissingRequired is the typed error a strict read reports for a record
// that lacks a | required field (docs/SPEC-TABLES.md §2.3): Fields names the
// missing fields, comma-separated in declaration order, at whatever depth of
// the record they were missing, and Error() is the report's own reason.
type TableMissingRequired struct { Fields string }
func(m TableMissingRequired) Error() string {return "missing_required: "+m.Fields}
// Err is the report's typed error (docs/SPEC-TABLES.md §6.5, docs/VERSIONING.md
// The read report). A strict read refuses malformed, clamped, kind-mismatched,
// unknown form, wire-damaged, build-version-mismatched, missing-required
// (§2.3, naming the field), a non-finite plain float (§3.3, a plain float's
// special values: non_finite, named in Reason) and blob_shared (§2.5), each as one typed reason. Unknown, Widened and Retained alone leave it nil, as does a clean read.
func(r *TableReport) Err() error {
if r==nil {return nil}
if r.Reason=="build_version_mismatch" {return TableBuildVersionMismatch{Peer:r.BuildVersionPeer,Own:r.BuildVersionOwn}}
// The fixed form's named refusals (docs/SPEC-TABLES.md §6.5, §21.3) answer as
// their TableRefuseReason member, so errors.Is(err, TableRefuseLayoutUnknown)
// holds for a fixed load that refused. The form-byte refusals (previous_form,
// newer_form, message_form_as_file) are NOT in this switch: §6.5's strict error
// identity keeps them answering TableRefuseUnknownForm below.
if r.FixedRaised && r.Verdict==TableOpenRefused {
	switch r.Reason {
	case "layout_unknown": return TableRefuseLayoutUnknown
	case "layout_unsupported": return TableRefuseLayoutUnsupported
	case "layout_newer": return TableRefuseLayoutNewer
	case "layout_malformed": return TableRefuseLayoutMalformed
	case "layout_count_mismatch": return TableRefuseLayoutCountMismatch
	case "layout_kind_unknown": return TableRefuseLayoutKindUnknown
	case "layout_kind_invalid": return TableRefuseLayoutKindInvalid
	case "layout_size_mismatch": return TableRefuseLayoutSizeMismatch
	case "layout_tree_unclosed": return TableRefuseLayoutTreeUnclosed
	case "layout_too_deep": return TableRefuseLayoutTooDeep
	case "layout_record_too_large": return TableRefuseLayoutRecordTooLarge
	case "plan_too_large": return TableRefusePlanTooLarge
	case "no_layout": return TableRefuseNoLayout
	case "batch_too_large": return TableRefuseBatchTooLarge
	case "text_ill_formed": return TableRefuseTextIllFormed
	case "count_over_bound": return TableRefuseCountOverBound
	case "length_over_bound": return TableRefuseLengthOverBound
	case "truncated": return TableRefuseTruncated
	case "reserved_not_zero": return TableRefuseReservedNotZero
	}
	return TableRefuseUnknownForm
}
// The fixed form's framing-damage returns (docs/SPEC-TABLES.md §6.5, §3.4) set
// Malformed and their Reason without FixedRaised, because they return -1
// directly instead of through tableFixedRefuse. A short fixed file keeps the
// truncated name and answers the malformed class through the report's own
// error; the enum itself stays one name per clause, so a save's short output
// buffer is truncated and not malformed.
if r.Reason=="truncated" {if r.Malformed {return tableFixedLoadTruncated{}};return TableRefuseTruncated}
if r.Reason=="reserved_not_zero" {return TableRefuseReservedNotZero}
if r.Verdict==TableOpenRefused || r.Reason=="unsupported wire form" || r.Reason=="newer_form" || r.Reason=="message_form_as_file" || r.Reason=="message form requires an announced vocabulary and a message reader" {return TableRefuseUnknownForm}
if r.Reason=="integrity_mismatch" {return TableRefuseIntegrityMismatch}
if r.Verdict==TableOpenDamaged {return TableRefuseWireDamaged}
if r.MissingRequired>0 {const p="missing_required: ";f:=r.Reason;if len(f)>=len(p)&&f[:len(p)]==p{f=f[len(p):]};return TableMissingRequired{Fields:f}}
if r.Malformed {return TableRefuseMalformed}
// A blob named from two slots (SPEC-TABLES §2.5, §6.5): the tolerant read keeps
// the one node and counts it; the strict read refuses it by name.
if r.BlobShared>0 {return TableRefuseBlobShared}
if r.Clamped>0 {if r.ClampedField!=""{return TableClampedField{Field:r.ClampedField}};return TableRefuseClamped}
if r.KindMismatch>0 {return TableRefuseKindMismatch}
{const p="non_finite: ";if len(r.Reason)>=len(p)&&r.Reason[:len(p)]==p {return TableRefuseNonFinite}}
return nil}
// tableNonFinite names a plain float32/float64 the read landed as a NaN or an
// infinity (docs/SPEC-TABLES.md §3.3, a plain float's special values): the
// pattern lands as written and counts nothing, and the name is what the
// strict read refuses as non_finite. The first name a read records wins. It
// takes the report's Reason, not a method on TableReport, so the shared runtime
// (§8.4) can export it: generated code outside the shared set reaches no
// unexported member, and the file reader's *TableReport and the message
// reader's TableReport value both pass &r.Report.Reason.
func tableNonFinite(reason *string,field string){if *reason==""{*reason="non_finite: "+field}}
// tableTextClamped names the field whose value a read clamped (docs/SPEC-TABLES.md
// §6.5), so a strict read can say which field. The first name a read records
// wins, as for non_finite. It lands in the report's own ClampedField slot,
// apart from Reason: Reason may already carry a non_finite name, and the
// clamped refusal must not lose its field to it. The slot is named by pointer
// so the same emitted call compiles in every reader context (the table read's
// *TableReader, the message read's TableMessageReader).
func tableTextClamped(clampedField *string,field string){if clampedField!=nil&&*clampedField==""{*clampedField=field}}
// TableClampedField is a strict read's clamped refusal naming the field it clamped
// (docs/SPEC-TABLES.md §6.5): errors.Is(err, TableRefuseClamped) holds.
type TableClampedField struct{ Field string }
func (e TableClampedField) Error() string { return "clamped: " + e.Field }
func (e TableClampedField) Is(target error) bool { return target == TableRefuseClamped }

`
