module benchgo

go 1.23

require (
	bench v0.0.0
	example v0.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

replace bench => ../../generated/bench/go

replace example => ../../generated/go

replace github.com/mas-bandwidth/serialize.go => ../../../serialize.go
