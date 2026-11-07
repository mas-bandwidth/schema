module ludicroustest

go 1.23

require (
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
	ludicrous v0.0.0
)

replace ludicrous => ../../generated/go-ludicrous

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../serialize.go
