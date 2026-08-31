module variantgen

go 1.23

require (
	bench v0.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

replace bench => ../../../generated/bench/go

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
