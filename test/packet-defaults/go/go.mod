module packetdefaultstest

go 1.23

require (
	packetdefaults v0.0.0
	packetplain v0.0.0
	constanthexadecimaladdition v0.0.0
	constantunsignedsubtraction v0.0.0
	github.com/mas-bandwidth/serialize.go/v2 v2.0.0
)

replace packetdefaults => ../../../build/packet-defaults/go/defaults

replace packetplain => ../../../build/packet-defaults/go/plain

replace constanthexadecimaladdition => ../../../build/packet-defaults/go/constant-hexadecimal-addition

replace constantunsignedsubtraction => ../../../build/packet-defaults/go/constant-unsigned-subtraction

replace github.com/mas-bandwidth/serialize.go/v2 => ../../../../serialize.go
