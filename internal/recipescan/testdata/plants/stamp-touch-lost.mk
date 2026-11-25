# F-046, second half: "gen && touch $@" followed by statements that decide the
# recipe's status, so a generator exiting 7 is make exit 0 with no stamp.
build/corpus/.stamp: build/gen-bin
	@lockdir="build/corpus.lock"; \
	mkdir "$$lockdir"; \
	./build/gen-bin --corpus build/corpus && touch "$@"; \
	rmdir "$$lockdir" 2>/dev/null || true
