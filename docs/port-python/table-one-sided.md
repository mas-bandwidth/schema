# One-sided integer ranges

The checker fills the omitted bound with the integer type's storage bound.
The Python table descriptor and integer codec use those same two bounds for
variable-form reads and writes. `TestPythonOneSidedRange` checks boundary and
out-of-range values against the Go engine, with an independently generated C++
writer, under both pinned interpreters. Its negative control removes the emitted
runtime's read-range check.
