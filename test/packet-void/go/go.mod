module packetvoidtest

go 1.23

require (
	packetvoid v0.0.0
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)

replace packetvoid => ../../../build/packet-void/go/packetvoid

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../../serialize.go
