# Fixture proof list: TestReal exists, TestReal/sub selects it through its
# parent, and TestPlanted does not. The lint must report TestPlanted by name and
# leave TestReal (and its /sub form) alone.
.PHONY: fixture-proof
fixture-proof:
	sh test/slowgate/proof fixture-proof \
		'TestReal TestReal/sub TestPlanted' \
		./testpkg -run '^Test' -count=1
