# derived-good names only declared tests through a derived list. derived-bogus
# adds one literal stale name beside it. derived-junk's list holds a token that
# is no test. The audit must refuse exactly TestBogus and notatest.
.PHONY: derived-good derived-bogus derived-junk
derived-good:
	sh test/slowgate/proof derived-good \
		'$(ROWS)' \
		./testpkg -run '^Test' -count=1
derived-bogus:
	sh test/slowgate/proof derived-bogus \
		'$(ROWS) TestBogus' \
		./testpkg -run '^Test' -count=1
derived-junk:
	sh test/slowgate/proof derived-junk \
		'$(JUNK)' \
		./testpkg -run '^Test' -count=1
