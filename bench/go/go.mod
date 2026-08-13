module benchgo

go 1.23

require (
	example v0.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

replace example => ../../generated/go

replace github.com/mas-bandwidth/serialize.go => ../../../serialize.go
