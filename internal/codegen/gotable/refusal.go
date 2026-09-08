package gotable

const tableRefusalSource = `
// TableRefuseReason is the typed error shared by file and accelerator refusals.
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
};return "invalid refusal reason"}
`
