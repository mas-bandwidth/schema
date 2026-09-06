module packetdefaultstest

go 1.23

require (
	packetdefaults v0.0.0
	packetplain v0.0.0
	github.com/mas-bandwidth/serialize.go v0.0.0
)

replace packetdefaults => ../../../build/packet-defaults/go/defaults

replace packetplain => ../../../build/packet-defaults/go/plain

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
