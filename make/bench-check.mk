# make bench-check: this bench or CI runner has every variable and tool schema's CI and legs expect, or it fails by name
# (make/bench-check.sh). Run it on a machine after nova-fleet sdk-toolchain.
# make exports its own defaults for the pinned tools (dist/...); the check takes the SDK's env.sh instead, as a login does.
.PHONY: bench-check
bench-check:
	@env -u PYTHON -u PYTHON_FLOOR -u MYPY -u RUBY -u RUBY_FLOOR -u RUBOCOP -u STEEP -u RBS sh make/bench-check.sh
