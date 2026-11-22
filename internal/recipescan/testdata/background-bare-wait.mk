# F-068: ten generators behind "&" and one bare wait. The wait returns 0
# whatever the generators did, so a failed one left the stamp written.
build/gen/.stamp: bin/gen
	@set -e; \
	./bin/gen --out build/gen/a a.schema & \
	./bin/gen --out build/gen/b b.schema & \
	wait
	@touch $@
