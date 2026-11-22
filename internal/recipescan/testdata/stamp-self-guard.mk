# F-046, first half: the recipe asks only whether its own stamp exists, so a
# rebuilt generator (a newer prerequisite) never regenerates the corpus.
build/corpus/.stamp: build/gen-bin bin/schema
	@if ! test -f "$@"; then \
		./build/gen-bin --corpus build/corpus; \
		touch "$@"; \
	fi
