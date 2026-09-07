module packetwidetest

go 1.23

require (
	github.com/mas-bandwidth/serialize.go v0.0.0
	packetwide v0.0.0
)

replace packetwide => ../../../build/packet-wide/go

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
