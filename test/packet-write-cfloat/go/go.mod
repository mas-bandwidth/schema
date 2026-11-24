module packetwritecfloattest

go 1.23

require (
	cfloat v0.0.0
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)

replace cfloat => ../../../build/packet-write-cfloat/go/cfloat

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../../serialize.go
