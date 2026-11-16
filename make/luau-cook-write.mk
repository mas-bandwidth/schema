# make/luau-cook-write.mk — the Luau leg's cook-write gate and its negative
# control (test/conformance/luau/cook-write says what each requires). Included
# by the Makefile's wildcard include.
#
# conformance-luau-cook-write                   every cook-write case passes,
#                                               both byte orders, none relaxed
# conformance-luau-cook-write-negative-control  the dead-slot zeroing cut out
#                                               of a copy of the generated cook
#                                               writer turns that gate red
.PHONY: conformance-luau-cook-write conformance-luau-cook-write-negative-control
conformance-luau-cook-write: build/conformance-harness build/conformance-luau build/luau-driver/inputs.luau build/luau-driver/units.luau
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	LUAU="$(LUAU)" sh test/conformance/luau/cook-write gate

conformance-luau-cook-write-negative-control: build/conformance-harness build/conformance-luau build/luau-driver/inputs.luau build/luau-driver/units.luau
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	LUAU="$(LUAU)" sh test/conformance/luau/cook-write control

test-luau: conformance-luau-cook-write conformance-luau-cook-write-negative-control
