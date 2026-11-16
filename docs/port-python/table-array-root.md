# JSON array roots

A table declared `json = "array"` reads and writes the single field's array
as its JSON root. The same shape applies to the table nested by value, in a
list, as a map value and in a union arm. Other top-level JSON shapes refuse
`json_root_not_array`; a nested wrong shape refuses `kind_mismatch`.
`TestPythonArrayRootAgainstEngine` compares canonical JSON and binary output
with the Go engine, including a generated C++ writer for the root. Its planted
root and nested controls verify that both dispatch paths are exercised.
