# Ruby gem lock controls

Fixtures for make/checks/ruby-gems-lock.sh, run by
`make ruby-gems-lock-controls` (make/ruby-toolchain.mk) before every gem
install. No network: the script reads the lock and does not fetch anything.

- `good.lock`: two real lines of make/ruby-gems.lock with a final newline.
  Accepted with 2 gems.
- `no-final-newline.lock`: the same two lines with the final newline removed.
  The install's shell `read` loop used to drop the last gem, so it was never
  fetched or held to its sha256. Refused.
- `malformed-line.lock`: the second line without its sha256. Refused.
- `names-nothing.lock`: comments only. Refused.

`no-final-newline.lock` ends without a newline on purpose. internal/ci's
TestRubyGemsLockIsReadWhole applies the same rule to each fixture and to the
real lock.
