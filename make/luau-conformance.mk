# make/luau-conformance.mk — the LUAU leg's conformance controls
# (test/conformance/README.md; docs/PORT-LUAU.md). Included by the Makefile's
# wildcard include. The reference leg is cpp; this is a discovered, non-reference
# leg, so its absences are ordinary.
#
# The Luau interpreter has no io library, so the driver hands main.luau the
# derived manifest as a module and this file hands it the FIXTURES and the unit
# module lists the same way (docs/PORT-LUAU.md, THE HARNESS): inputs.luau
# embeds every wire a wire/report case names, and units.luau names the
# <Base>Table.luau modules each unit registers. Both are generated from the
# committed manifest into build/luau-driver/ (kept out of build/conformance-luau
# because that target's recipe wipes it).

LUAU_CONFORMANCE_DRIVER := build/luau-driver
LUAU_CONFORMANCE_MANIFEST := testdata/conformance/tables/MANIFEST.txt

# inputs.luau: every fixture a wire or report case names, as a buffer.
$(LUAU_CONFORMANCE_DRIVER)/inputs.luau: $(LUAU_CONFORMANCE_MANIFEST)
	@mkdir -p $(@D)
	perl -e 'local $$/; my $$m = <>; my %want; for my $$line (split /\n/, $$m) { my @f = split /\s+/, $$line; next unless @f; next if $$f[0] =~ /^#/; if ($$f[0] eq "instance" || $$f[0] eq "report") { $$want{$$f[4]} = 1 if defined $$f[4]; } } print "return {\n"; for my $$path (sort keys %want) { next unless -f $$path; open(my $$in, "<:raw", $$path) or die "$$path: $$!"; my $$data = <$$in>; close $$in; $$data = "" unless defined $$data; my $$h = unpack("H*", $$data); $$h =~ s/(..)/\\x$$1/g; print "\t[\"$$path\"] = buffer.fromstring(\"$$h\"),\n"; } print "}\n";' $< > $@

# units.luau: per unit, the <Base>Table.luau modules its schema files register.
$(LUAU_CONFORMANCE_DRIVER)/units.luau: $(LUAU_CONFORMANCE_MANIFEST)
	@mkdir -p $(@D)
	perl -e 'local $$/; my $$m = <>; my %units; for my $$line (split /\n/, $$m) { my @f = split /\s+/, $$line; next unless @f; next if $$f[0] =~ /^#/; next unless $$f[0] eq "unit"; my @mods; for my $$i (2..$$#f) { my $$p = $$f[$$i]; if ($$p =~ m{\.schema\z}) { my ($$b) = $$p =~ m{([^/]+)\.schema\z}; push @mods, $$b . "Table" if defined $$b; } else { for my $$s (glob($$p . "/*.schema")) { my ($$b) = $$s =~ m{([^/]+)\.schema\z}; push @mods, $$b . "Table" if defined $$b; } } } my %seen; @mods = grep { !$$seen{$$_}++ } @mods; $$units{$$f[1]} = \@mods; } print "return {\n"; for my $$k (sort keys %units) { print "\t[\"$$k\"] = { " . join(", ", map { "\"$$_\"" } @{$$units{$$k}}) . " },\n"; } print "}\n";' $< > $@

# The conformance leg builds these before the harness runs; the build of
# build/conformance-luau itself wipes its own directory, so they live apart.
build/conformance-luau: $(LUAU_CONFORMANCE_DRIVER)/inputs.luau $(LUAU_CONFORMANCE_DRIVER)/units.luau

# THE LUAU LEG's NEGATIVE CONTROL, on the same rule as the Go one: a harness
# that has never gone red on a leg is watching that leg. Two sabotages of the
# emitted units, each in a COPY of the generated tree under a mirror of the
# repository (test/conformance/luau/units-control), never in place: the
# wire-fuzz gates read build/conformance-luau under make -j. Each is required to turn EXACTLY its surfaces red and leave every other
# surface of the matrix passing:
#
#   1. the LOAD: one report counter is forced to differ from the corpus
#      expectation, so `report` and `retain` (which prints the load's unknown
#      count) go red while `wire`, whose saved bytes the counter does not
#      touch, stays green;
#   2. the ACCELERATOR READERS: the cook's reserved-words clause and the
#      block's pitch clause are cut out of Check and BlockOpen, so the two
#      forgery batteries and the two reason surfaces go red while the cook and
#      block opens, dumps and foreign refusals, which those clauses never
#      decide, stay green.
.PHONY: conformance-negative-control-luau
conformance-negative-control-luau: build/conformance-harness build/conformance-luau $(LUAU_CONFORMANCE_DRIVER)/inputs.luau $(LUAU_CONFORMANCE_DRIVER)/units.luau
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	sh test/conformance/luau/units-control

# THE EMITTER CONTROLS (docs/PORTING.md I2, I11): the two above sabotage the
# emitted units; these two sabotage the EMITTER, a sed over a copy of
# one embedded runtime file and `go build -overlay` into a private compiler,
# the units regenerated from it under a mirror of the repository, and the
# harness required red on exactly the named surfaces with the neighbours green.
# test/conformance/luau/negative-control holds both and says what each requires.
#
# conformance-negative-control-luau-emitter  the load report's unknown counter
#                                            (report and retain red, wire green)
# conformance-negative-control-luau-walk     the text walk's field-key match (I11:
#                                            json-read and json-hostile red,
#                                            json-write and wire green)
.PHONY: conformance-negative-control-luau-emitter conformance-negative-control-luau-walk
conformance-negative-control-luau-emitter: build/conformance-harness $(LUAU_CONFORMANCE_DRIVER)/units.luau
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	sh test/conformance/luau/negative-control report

conformance-negative-control-luau-walk: build/conformance-harness $(LUAU_CONFORMANCE_DRIVER)/units.luau
	@if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi
	sh test/conformance/luau/negative-control walk
