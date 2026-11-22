module schematables

go 1.26

require (
	blockdemo v0.0.0
	graphdemo v0.0.0
	tabledemo v0.0.0
	tblp1 v0.0.0
	tblp3 v0.0.0
	tblv1 v0.0.0
	tblv2 v0.0.0
)

require github.com/mas-bandwidth/serialize.go/v2 v2.0.0

// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)
require github.com/mas-bandwidth/schema/v2 v2.0.0

replace github.com/mas-bandwidth/schema/v2 => ../..

// THIS MODULE DOES NOT BUILD FROM A PLAIN CLONE, and cannot: its units are
// the generated Go of the tables corpora, written by
// `make build/tables-generated-go/.stamp`, and its serialize.go runtime is
// the sibling checkout ../serialize.go that `make deps` clones. `go test`
// here fails at setup naming the missing replacement directory. Run
//
//	make deps build/tables-generated-go/.stamp
//
// first. `make test-go`, the lane that runs this module, builds the generated
// units itself; the sibling is a bench prerequisite. Every test inside builds
// the rest of what it reads itself.
replace tabledemo => ../../build/tables-generated-go/examples

replace tblv1 => ../../build/tables-generated-go/v1

replace tblv2 => ../../build/tables-generated-go/v2

replace tblp1 => ../../build/tables-generated-go/p1

replace tblp3 => ../../build/tables-generated-go/p3

replace blockdemo => ../../build/tables-generated-go/block

replace graphdemo => ../../build/tables-generated-go/pointers

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../serialize.go
