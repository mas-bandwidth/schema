module pairedtablebench

go 1.24

require (
	benchtable v0.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

replace benchtable => ../../../generated/bench/paired/go

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
