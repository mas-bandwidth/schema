package compiler

// docs/SPEC-TABLES.md §2.3: the Python target carries `?T` over a variable
// closure, an optional whose by-value T holds a pointer (`inner ?Inner` with
// `Inner { next *Inner }`). The table runtime's presence gate (save.py's
// _collect and the optional kind) numbers T's edges only when the value is
// present, so an absent value owns no node; internal/codegen/pythontable's
// TestPythonVtOptional holds it to the Go engine's bytes.
//
// A `?T` over a fixed closure needs no key: it is an ordinary table field. The
// optional arrays `?[N]T` and `?[..N]T` have no key in the §2 carry vocabulary
// either; they ride the plain array framing (kind 14) whenever present, zero
// count and all-default included, so the target joins the optional-array
// carriers (compiler/tablesoptionalarrays.go) and target_python.go's
// refuseOptionalArrays passes it.
var _ = carryPython(carryOptionalVariableClosure)

func init() {
	registerOptionalArrayCarrier("python")
}
