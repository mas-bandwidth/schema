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
// TableRefuseOutOfRange: a value outside a range the read holds it to
// (docs/SPEC-TABLES.md §4, §6.5), the ordinal the retired spelling clamped held.
TableRefuseOutOfRange
TableRefuseKindMismatch
TableRefuseBase64Damaged
TableRefuseBase64NotCanonical
TableRefuseIntegrityMismatch
// TableRefuseNonFiniteFloat: a NaN or infinity float, refused by every read and
// writer (docs/SPEC-TABLES.md §3.3); the retired non_finite's ordinal (§6.5).
TableRefuseNonFiniteFloat
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
// TableRefuseMissingRequired is a record that lacks a | required field
// (docs/SPEC-TABLES.md §2.3). It is a report-only member: Err answers the typed
// TableMissingRequired naming the fields, never this value.
const TableRefuseMissingRequired TableRefuseReason = 255
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
case TableRefuseOutOfRange:return "out_of_range"
case TableRefuseKindMismatch:return "kind_mismatch"
case TableRefuseBase64Damaged:return "base64_damaged"
case TableRefuseBase64NotCanonical:return "base64_not_canonical"
case TableRefuseIntegrityMismatch:return "integrity_mismatch"
case TableRefuseNonFiniteFloat:return "non_finite_float"
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
case TableRefuseMissingRequired:return "missing_required"
};return "invalid refusal reason"}
// tableTruncatedMalformed is the typed error a truncated fixed load answers
// (docs/SPEC-TABLES.md §3.4): truncated by name, and matched by
// errors.Is(err, TableRefuseWireDamaged) as the damage it is. It is a value, not
// the caller's *TableReport, so the error keeps its answer when the report is
// reused; the fixed writer's short output buffer returns TableRefuseTruncated
// itself.
type tableTruncatedMalformed struct{}
func(tableTruncatedMalformed) Error() string {return TableRefuseTruncated.Error()}
func(tableTruncatedMalformed) Is(target error) bool {return target==TableRefuseTruncated||target==TableRefuseWireDamaged}
func tableRefuseError(reason TableRefuseReason)error{if reason==TableRefuseOk{return nil};return reason}
// TableBuildVersionMismatch is the typed error a strict announcement refusal
// reports (docs/SPEC-TABLES.md §3.3): the endpoint's build version and the
// reader's own, each a 64-bit number.
type TableBuildVersionMismatch struct { Remote, Own uint64 }
func(m TableBuildVersionMismatch) Error() string {const digits="0123456789abcdef";var remote,own [16]byte;p,o:=m.Remote,m.Own;for i:=15;i>=0;i-- {remote[i]=digits[p&0xf];p>>=4};for i:=15;i>=0;i-- {own[i]=digits[o&0xf];o>>=4};return "build_version_mismatch: remote 0x"+string(remote[:])+" own 0x"+string(own[:])}
// TableMissingRequired is the typed error a strict read reports for a record
// that lacks a | required field (docs/SPEC-TABLES.md §2.3): Fields names the
// missing fields, comma-separated in declaration order, at whatever depth of
// the record they were missing, and Error() is the report's own reason.
type TableMissingRequired struct { Fields string }
func(m TableMissingRequired) Error() string {return "missing_required: "+m.Fields}
// TableOutOfRange is a read's out_of_range refusal naming the field whose value
// lies outside the range the read holds it to (docs/SPEC-TABLES.md §4, §6.5):
// errors.Is(err, TableRefuseOutOfRange) holds.
type TableOutOfRange struct{ Field string }
func (e TableOutOfRange) Error() string { return "out_of_range: " + e.Field }
func (e TableOutOfRange) Is(target error) bool { return target == TableRefuseOutOfRange }
// TableMalformedField is the JSON read's malformed refusal naming the field it
// refused (docs/SPEC-TABLES.md §16.2): errors.Is(err, TableRefuseMalformed) holds.
type TableMalformedField struct{ Field string }
func (e TableMalformedField) Error() string { return "malformed: " + e.Field }
func (e TableMalformedField) Is(target error) bool { return target == TableRefuseMalformed }
// Err is the report's typed error (docs/SPEC-TABLES.md §6.5): nil on a read that
// returned, and the refusal otherwise, as an error value whose detail names the
// reason and, where it has one, the field. errors.Is matches the
// TableRefuseReason member; errors.As matches the typed errors
// (TableMissingRequired, TableOutOfRange, TableBuildVersionMismatch).
// Unknown, Widened and Retained never make it non-nil.
func(r *TableReport) Err() error {
if r==nil {return nil}
if r.Reason=="build_version_mismatch" {return TableBuildVersionMismatch{Remote:r.BuildVersionRemote,Own:r.BuildVersionOwn}}
// The fixed form's named refusals (docs/SPEC-TABLES.md §6.5, §21.3) answer as
// their TableRefuseReason member, so errors.Is(err, TableRefuseLayoutUnknown)
// holds for a fixed load that refused. The form-byte refusals (previous_form,
// newer_form, message_form_as_file) answer TableRefuseUnknownForm below.
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
	case "out_of_range": return TableOutOfRange{Field:r.Field}
	case "truncated": return TableRefuseTruncated
	case "reserved_not_zero": return TableRefuseReservedNotZero
	case "wire_damaged": return TableRefuseWireDamaged
	case "non_finite_float": return TableRefuseNonFiniteFloat
	case "kind_mismatch": return TableRefuseKindMismatch
	}
	return TableRefuseUnknownForm
}
// A short fixed file keeps the truncated name and answers the damage class
// through a value error; a save's short output buffer is truncated alone.
if r.Reason=="truncated" {if r.Refusal!=TableRefuseOk {return tableTruncatedMalformed{}};return TableRefuseTruncated}
if r.Reason=="reserved_not_zero" {return TableRefuseReservedNotZero}
if r.Verdict==TableOpenRefused || r.Reason=="previous_form" || r.Reason=="newer_form" || r.Reason=="message_form_as_file" {return TableRefuseUnknownForm}
if r.Reason=="integrity_mismatch" {return TableRefuseIntegrityMismatch}
if r.Reason=="count_over_extent_cap" {return TableRefuseCountOverExtentCap}
if r.Verdict==TableOpenDamaged && r.Refusal==TableRefuseOk {return TableRefuseWireDamaged}
switch r.Refusal {
case TableRefuseOk: return nil
case TableRefuseMissingRequired: return TableMissingRequired{Fields:r.Field}
case TableRefuseOutOfRange: return TableOutOfRange{Field:r.Field}
case TableRefuseMalformed: if r.Field!="" {return TableMalformedField{Field:r.Field}}
}
return r.Refusal}

`
