# Fixture proof list: TestReal exists, TestPlanted does not. The lint must
# report TestPlanted by name and leave TestReal alone.
.PHONY: fixture-proof
fixture-proof:
	sh test/slowgate/proof fixture-proof \
		'TestReal TestPlanted' \
		./testpkg -run '^Test' -count=1
