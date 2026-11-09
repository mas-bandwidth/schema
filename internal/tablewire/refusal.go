package tablewire

import (
	"errors"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// THE ONE READ (docs/SPEC-TABLES.md §4, §6.5): every read returns the whole
// value or refuses the whole load. The verdict is the cross-language
// contract; the reason a refusal names is this engine's own diagnostic,
// spelled from §6.5's recommended vocabulary and never compared across
// languages. Detection order is free, so the reason is whichever condition the
// walk met first (tla/ReadOrder.tla, OneVerdict).
const (
	ReasonWireDamaged        = "wire_damaged"
	ReasonKindMismatch       = "kind_mismatch"
	ReasonOutOfRange         = "out_of_range"
	ReasonCountOverBound     = "count_over_bound"
	ReasonLengthOverBound    = "length_over_bound"
	ReasonCountOverExtentCap = "count_over_extent_cap"
	ReasonBlobOverSizeCap    = "blob_over_size_cap"
	ReasonBlobShared         = "blob_shared"
	ReasonNonFiniteFloat     = "non_finite_float"
	ReasonMissingRequired    = "missing_required"
	ReasonIntegrityMismatch  = "integrity_mismatch"
)

// Refusal is a read's refusal: the reason, and the detail that says where (a
// field as <Table>.<field>, or what the framing showed). Error() is
// "<reason>: <detail>".
type Refusal struct {
	Reason string
	Detail string
}

func (e *Refusal) Error() string {
	if e.Detail == "" {
		return e.Reason
	}
	return e.Reason + ": " + e.Detail
}

// RefusalReason is the reason name a read's error carries, for every typed
// refusal this package returns; "" for nil and for an error that is not a
// read's refusal.
func RefusalReason(err error) string {
	if err == nil {
		return ""
	}
	if r, ok := errors.AsType[*Refusal](err); ok {
		return r.Reason
	}
	if _, ok := errors.AsType[*NonFiniteRefusal](err); ok {
		return ReasonNonFiniteFloat
	}
	if _, ok := errors.AsType[*CountRefusal](err); ok {
		return ReasonCountOverExtentCap
	}
	if _, ok := errors.AsType[*tabletext.MissingRequired](err); ok {
		return ReasonMissingRequired
	}
	if fr, ok := errors.AsType[*FixedRefusal](err); ok {
		return fr.Reason
	}
	if mf, ok := errors.AsType[*MessageRefusal](err); ok {
		return mf.Reason.String()
	}
	if _, ok := errors.AsType[*FormRefusal](err); ok {
		return "newer_form"
	}
	return ""
}

// ReadRefusal reports whether an error is a read's refusal, the verdict
// "refused" (docs/SPEC-TABLES.md §6.5), rather than a caller's error.
func ReadRefusal(err error) bool { return RefusalReason(err) != "" }

// refuseReport writes a refusal into the report: the verdict, the reason as
// diagnostic text, and no counter, because nothing decoded is defined.
func refuseReport(report *tabletext.Report, err error) {
	*report = tabletext.Report{Refused: true, Reason: reportReason(err)}
}

// reportReason is the report's diagnostic for a refusal: `<reason>: <detail>`
// where the refusal names a field or a framing fault, and the reason name
// alone where it names nothing more.
func reportReason(err error) string {
	var refusal *Refusal
	var nonFinite *NonFiniteRefusal
	var missing *tabletext.MissingRequired
	if errors.As(err, &refusal) || errors.As(err, &nonFinite) || errors.As(err, &missing) {
		return err.Error()
	}
	if reason := RefusalReason(err); reason != "" {
		return reason
	}
	return err.Error()
}

// readFail is the first condition one read met, shared by every sub-reader of
// that read: once set, every walk stops and the read refuses.
type readFail struct{ err error }
