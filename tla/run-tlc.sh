#!/usr/bin/env bash
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
jar=${TLC_JAR:-/opt/tla/tla2tools.jar}
if [ ! -f "$jar" ]; then
  echo "tla/run-tlc.sh: no TLC jar at $jar; set TLC_JAR" >&2
  exit 2
fi
java=${TLC_JAVA:-java}

work=$(mktemp -d "${TMPDIR:-/tmp}/tla-tlc.XXXXXX")
trap 'rm -rf "$work"' EXIT

declare -A modules outcomes exits
while IFS=$'\t' read -r config module outcome invariant states distinct depth exit_code java_version tlc_version model_sha256; do
  [ "$config" = "config" ] && continue
  [ -z "${config:-}" ] && continue
  modules[$config]=$module
  outcomes[$config]=$outcome
  exits[$config]=$exit_code
done < "$here/RUNS.tsv"

status=0
for cfg in "$here"/MC*.cfg; do
  config=$(basename "$cfg")
  module=${modules[$config]:-}
  if [ -z "$module" ]; then
    stem=${config#MC}
    stem=${stem%.cfg}
    module=
    for tla in "$here"/*.tla; do
      name=$(basename "$tla" .tla)
      name=${name#MC}
      if [[ "$stem" == "$name"* ]] && [ ${#name} -gt ${#module} ]; then
        module=$name
      fi
    done
  fi
  if [ -z "$module" ]; then
    echo "TLC FAIL config=$config module=missing" >&2
    status=1
    continue
  fi
  run_module=$module
  if [ -f "$here/MC$module.tla" ]; then
    run_module=MC$module
  fi
  outcome=${outcomes[$config]:-}
  if [ -z "$outcome" ]; then
    if grep -qE '^CONSTANT [A-Za-z0-9_]+ = TRUE$' "$cfg"; then
      outcome=counterexample
    else
      outcome=exhaustive
    fi
  fi
  expected=${exits[$config]:-}
  log="$work/$config.log"
  set +e
  (cd "$here" && "$java" -Djava.io.tmpdir="$work" -Xmx4g -cp "$jar" tlc2.TLC \
    -workers 4 -config "$config" -metadir "$work/meta-$config" "$run_module") >"$log" 2>&1
  code=$?
  set -e
  if [ "$outcome" = exhaustive ]; then
    if [ "$code" -ne 0 ] || ! grep -q 'Model checking completed. No error has been found.' "$log"; then
      echo "TLC FAIL config=$config module=$run_module exit=$code expected=exhaustive" >&2
      sed -n '1,80p' "$log" >&2
      status=1
      continue
    fi
  else
    if [ -n "$expected" ] && [ "$code" -ne "$expected" ]; then
      echo "TLC FAIL config=$config module=$run_module exit=$code expected=$expected" >&2
      sed -n '1,80p' "$log" >&2
      status=1
      continue
    fi
    if [ -z "$expected" ] && [ "$code" -eq 0 ]; then
      echo "TLC FAIL config=$config module=$run_module exit=0 expected=counterexample" >&2
      sed -n '1,80p' "$log" >&2
      status=1
      continue
    fi
  fi
  echo "TLC OK config=$config module=$run_module result=$outcome exit=$code"
done
exit "$status"
