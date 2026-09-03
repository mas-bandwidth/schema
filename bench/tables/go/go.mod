module schematablesbench

go 1.24

require benchtable v0.0.0

require github.com/mas-bandwidth/serialize.go v0.0.0 // indirect

replace benchtable => ../../../generated/bench/tables/go

// the generated PACKET sources beside the table codec import the runtime; the
// table codec itself names none. A `replace` in a dependency is ignored, so
// the main module carries it — the same wiring test/go/go.mod has.
replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
