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
# that has never gone red on a leg is watching that leg. The emitted LOAD
# runtime is sabotaged in place — one report counter is forced to differ from
# the corpus expectation — so `luau / report` goes red while `luau / wire`,
# whose saved bytes are unaffected by the counter, stays green.
.PHONY: conformance-negative-control-luau
conformance-negative-control-luau: build/conformance-harness build/conformance-luau $(LUAU_CONFORMANCE_DRIVER)/inputs.luau $(LUAU_CONFORMANCE_DRIVER)/units.luau
	@set -eu; \
	if [ ! -x "$(LUAU)" ]; then $(MAKE) dist-luau; fi; \
	out=build/luau-table-control; \
	rm -rf $$out && mkdir -p $$out/backup; \
	for f in build/conformance-luau/*/load.luau; do \
		k=$${f#build/conformance-luau/}; mkdir -p $$out/backup/$$(dirname $$k); cp $$f $$out/backup/$$k; \
	done; \
	trap 'for f in build/conformance-luau/*/load.luau; do k=$${f#build/conformance-luau/}; cp $$out/backup/$$k $$f; done' EXIT; \
	for f in build/conformance-luau/*/load.luau; do \
		sed -i '/^[[:space:]]*resolve(report)$$/a\	report.unknown = report.unknown + 1' $$f; \
	done; \
	printf 'luau test/conformance/luau/driver\n' > $$out/drivers.txt; \
	if ./build/conformance-harness run --drivers $$out/drivers.txt --work $$out/work > $$out/log 2>&1; then \
		echo "negative control failed: the luau load sabotage left conformance green" >&2; \
		exit 1; \
	fi; \
	grep -q 'luau / report' $$out/log; \
	grep -q '^wire          pass' $$out/log; \
	echo "Luau table negative control: the load sabotage is caught by report and leaves wire green"
