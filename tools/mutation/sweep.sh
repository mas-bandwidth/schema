#!/bin/sh
# Queue one mutation sweep per scope on a bench, each on its own nova-reserve
# slot, each with a deadline, logs and results beside the clone.
#
# usage: tools/mutation/sweep.sh <dir> <n> <seed> <hours> <extra flags or ""> <scope>...
#
# <dir> holds the built driver (<dir>/mutation), and gets logs/, results/ and
# work/. The script returns at once; the sweeps run under nohup.
set -u
dir=$1 n=$2 seed=$3 hours=$4 extra=$5
shift 5
mkdir -p "$dir/logs" "$dir/results" "$dir/work"
for scope in "$@"; do
	# shellcheck disable=SC2086
	nohup timeout "${hours}h" nova-reserve run --why "mutation-$scope" -- \
		"$dir/mutation" run -scope "$scope" -n "$n" -seed "$seed" -work "$dir/work" \
		-out "$dir/results/$scope.jsonl" $extra \
		> "$dir/logs/$scope.log" 2>&1 < /dev/null &
done
