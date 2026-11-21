# Moo pinned-list controls

Fixtures for make/checks/moo-pinned-list.sh, run by
`make tables-moo-wire-fuzz-pinned-list-controls` (make/moo-fuzz.mk) before every
pinned replay. Each directory holds a list `PINNED` and the saved mutants it
names under `pins/`.

- `good/`: two saved, two listed, final newline. Accepted with 2 mutants.
- `no-final-newline/`: the same with the list's final newline removed. The
  replay's shell `read` loop used to drop that last line and pass with one
  mutant (finding F-055). Refused.
- `unlisted-saved/`, `listed-not-saved/`, `names-nothing/`: one broken
  condition each. Refused.

internal/ci's TestMooPinnedListIsReadWhole applies the same rules to each
fixture and to the real list without a toolchain. `no-final-newline/PINNED`
ends without a newline on purpose.
