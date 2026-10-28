module packetfinitetest

go 1.23

require (
	packetfinite v0.0.0
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)

replace packetfinite => ../../../build/packet-finite/go/generated

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../../serialize.go
