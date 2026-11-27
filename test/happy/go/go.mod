module happytest

go 1.23

require (
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
	happy v0.0.0
)

replace happy => ../../../build/happy/go

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../../serialize.go
