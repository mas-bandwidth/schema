# THE CAA SELF-TEST (the registry's tooling.caa.self-test; it was
# caa-self-test.yml): re-applies cla.yml's allowlist to main's tip, the way the
# contributor-assistant action decides. It reads GitHub, so it needs
# GITHUB_TOKEN and the network: a functional row, run per PR by ci-fast.yml,
# which supplies the token. A missing token fails by name.
.PHONY: caa-self-test
caa-self-test:
	@test -n "$$GITHUB_TOKEN" || { echo "caa-self-test: GITHUB_TOKEN is not set: it reads main's tip from GitHub"; exit 1; }
	python3 .github/caa-self-test.py
