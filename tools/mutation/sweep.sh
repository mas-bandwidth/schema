#!/bin/sh
# Queue mutation sweeps on a bench: <lanes> run at once, each lane takes its
# scopes one after another, each sweep on its own nova-reserve slot and under
# a deadline, logs and results beside the clone.
#
# usage: tools/mutation/sweep.sh <dir> <n> <seed> <hours> <lanes> <extra flags or ""> <scope>...
#
# Run it from the checkout. <dir> holds the built driver (<dir>/mutation) and
# gets logs/, results/ and work/. The script returns at once; the lanes run
# under nohup. A bench shared with CI gets few lanes and MUTATION_JOBS=2: the
# sweep is a nightly-style job and CI load wins.
set -u
dir=$1 n=$2 seed=$3 hours=$4 lanes=$5 extra=$6
shift 6
mkdir -p "$dir/logs" "$dir/results" "$dir/work"
lane=0
while [ "$lane" -lt "$lanes" ]; do
	mine=""
	i=0
	for scope in "$@"; do
		if [ $((i % lanes)) -eq "$lane" ]; then mine="$mine $scope"; fi
		i=$((i + 1))
	done
	if [ -n "$mine" ]; then
		# shellcheck disable=SC2086
		nohup sh -c '
			dir=$1 n=$2 seed=$3 hours=$4 extra=$5
			shift 5
			for scope in "$@"; do
				timeout "${hours}h" nova-reserve run --why "mutation-$scope" -- \
					"$dir/mutation" run -scope "$scope" -n "$n" -seed "$seed" -work "$dir/work" \
					-out "$dir/results/$scope.jsonl" $extra > "$dir/logs/$scope.log" 2>&1
			done
		' lane "$dir" "$n" "$seed" "$hours" "$extra" $mine > /dev/null 2>&1 < /dev/null &
	fi
	lane=$((lane + 1))
done
