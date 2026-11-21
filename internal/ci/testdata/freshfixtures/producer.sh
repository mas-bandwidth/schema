#!/bin/sh
# A controlled fixture producer for the F-194 cases.
#   producer.sh ok <text> --work <dir>   writes <dir>/fixtures/Scene.cook holding <text>
#   producer.sh fail --work <dir>        exits 17 before writing anything
#   producer.sh empty --work <dir>       exits 0 having written nothing
mode="$1"; shift
text=""
if [ "$mode" = ok ]; then text="$1"; shift; fi
[ "$1" = "--work" ] || { echo "producer.sh: no --work" >&2; exit 2; }
work="$2"
case "$mode" in
	ok) mkdir -p "$work/fixtures" && printf '%s\n' "$text" > "$work/fixtures/Scene.cook" ;;
	fail) echo "producer: refusing before any write"; exit 17 ;;
	empty) exit 0 ;;
	*) exit 2 ;;
esac
