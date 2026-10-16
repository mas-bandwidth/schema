#!/bin/sh
# usage: direct.sh <label>  -- runs the built read arm 7 rounds directly, best of rounds per wire
cd ~/rowan-jobs/clamp-w1/schema
echo "# $1 rev $(git rev-parse --short HEAD) load $(sysctl -n vm.loadavg) $(date -u +%FT%TZ)"
build/paired/table-cpp --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --gate --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants >/dev/null 2>&1 || { echo GATE-FAILED; exit 1; }
for r in 0 1 2 3 4 5 6; do
  build/paired/table-cpp --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --csv --round $r --iterations 4000000 --packet-iterations 4000000 --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants 2>/dev/null | grep -E "^cpp,"
done | awk -F, "{ns=1e9/\$7; k=\$2; n[k]++; s[k]=s[k]\" \"sprintf(\"%.1f\",ns); if(!(k in b)||ns<b[k])b[k]=ns; if(!(k in w)||ns>w[k])w[k]=ns} END{for(k in b) printf \"%s best %.1f worst %.1f spread %.1f%% rounds:%s\n\",k,b[k],w[k],100*(w[k]-b[k])/b[k],s[k]}"
echo "# load after $(sysctl -n vm.loadavg)"
