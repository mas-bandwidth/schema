#!/bin/sh
# A `find` that lists the first file it finds and then fails with exit 7: the
# partial listing the Lua fuzz replay used to count as the whole directory
# (finding F-078; `find | sort` reported sort's status). The recipe
# make schema-lua-fuzz-replay-controls installs it as `find` ahead of the real
# one and replays good/, which must then be refused.
PATH=$(command -p getconf PATH) find "$@" | head -n 1
exit 7
