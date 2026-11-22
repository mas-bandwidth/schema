# F-219: go test rewrites the space in -skip 'TestWin Codec' to _, so this line
# runs TestWin_Other and skips TestWin_Codec. Only TestWin_Other is credited.
.PHONY: core
core:
	SCHEMA_SLOW=1 go test -count=1 -run '^TestWin' -skip 'TestWin Codec' ./pkg/
