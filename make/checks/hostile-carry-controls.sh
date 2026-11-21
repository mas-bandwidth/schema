#!/bin/sh
# THE CARRY CONTROLS (docs/HOSTILE-CORPUS.md; F-070, F-079): a carried
# finding excuses the answer it records and no other. Each hostile-corpus
# runner is run over the committed internal/ci/hostilecorpus/testdata/carry
# corpus (the stock wire-valid input under a digest no reader computes, so
# every port disagrees by giving its true digest) with each committed findings
# file:
#
#   answer-matches       the finding records the port's own answer: carried, exit 0
#   answer-refuse        it records a refusal and the port accepts: FAIL, exit nonzero
#   answer-other-digest  it records another digest: FAIL, exit nonzero
#   no-answer            the old form, no answer: the findings file is refused
#
# usage: hostile-carry-controls.sh <port> <findings folder> <work dir> <runner command>
#
# The runner command is one shell string that reads the corpus folder from
# $CARRY_CORPUS and the findings file from $CARRY_FINDINGS. <findings folder>
# is `findings`, or `findings-lobster` for the Lobster runner's own form.
set -u
port=$1
form=$2
work=$3
cmd=$4
dir=internal/ci/hostilecorpus/testdata/carry
mkdir -p "$work" || exit 2
CARRY_CORPUS=$dir/corpus
export CARRY_CORPUS
bad=0
for control in answer-matches answer-refuse answer-other-digest no-answer; do
	CARRY_FINDINGS=$dir/$form/$control.tsv
	export CARRY_FINDINGS
	[ -f "$CARRY_FINDINGS" ] || { echo "$port carry controls: $CARRY_FINDINGS is missing: a control that is not there passes nothing" >&2; exit 2; }
	log=$work/$control.log
	sh -c "$cmd" > "$log" 2>&1
	status=$?
	# the FIX-CARD lines themselves; a summary saying "0 carried as FIX-CARD"
	# is not one
	carried=$(grep -c '^FIX-CARD' "$log")
	case $control in
	answer-matches)
		if [ "$status" -ne 0 ] || [ "$carried" -eq 0 ] || grep -q '^FAIL wire-valid' "$log"; then
			echo "$port carry controls: RED: $control: a finding that records the port's own answer was not carried (exit $status)"
			cat "$log"
			bad=1
		fi
		;;
	no-answer)
		if [ "$status" -eq 0 ] || [ "$carried" -ne 0 ]; then
			echo "$port carry controls: RED: $control: a finding that records no answer was read (exit $status)"
			cat "$log"
			bad=1
		fi
		;;
	*)
		if [ "$status" -eq 0 ] || [ "$carried" -ne 0 ] || ! grep -q '^FAIL wire-valid' "$log"; then
			echo "$port carry controls: RED: $control: a finding carried an answer it does not record (exit $status)"
			cat "$log"
			bad=1
		fi
		;;
	esac
done
[ "$bad" -eq 0 ] || exit 1
echo "$port carry controls: a finding carries the answer it records (answer-matches), and a refusal, another digest and no answer are red"
