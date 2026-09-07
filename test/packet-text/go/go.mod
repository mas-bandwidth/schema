module packettexttest

go 1.23

require (
	github.com/mas-bandwidth/serialize.go v0.0.0
	packettext v0.0.0
)

replace packettext => ../../../build/packet-text/go

replace github.com/mas-bandwidth/serialize.go => ../../../../serialize.go
