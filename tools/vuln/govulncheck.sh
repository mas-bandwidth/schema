#!/bin/bash
# govulncheck.sh — the vulnerability gate, and the classifier that decides
# whether a red one is this tree's fault.
#
# govulncheck fetches the VULNERABILITY DATABASE at run time, which is the
# whole point of the gate — a new CVE lands with no commit — and is also what
# puts somebody else's network among this gate's inputs. On 2026-09-17
# https://vuln.go.dev/index/modules.json.gz answered 503 and a pull request
# went red for an upstream outage: a red that says nothing about the diff, and
# the worst kind, because it is what teaches the next reader to discount a red
# vuln job.
#
#   tools/vuln/govulncheck.sh run [--strict]
#       Three attempts, 15 s then 45 s apart. ONLY THE TRANSPORT IS RETRIED
#       AND ONLY THE TRANSPORT IS EVER FORGIVEN: a finding fails on the first
#       attempt with no sleep and no second try, and so does every other
#       non-zero exit the classifier does not recognise as the network. Three
#       attempts that all failed to REACH the database exit 0 with a notice
#       saying the gate proved NOTHING — or, under --strict, exit 1.
#
#       The two callers differ by that flag and nothing else. The pull-request
#       gate forgives, because a contributor's diff cannot be judged by
#       vuln.go.dev's uptime; the nightly certification does NOT, because a
#       certificate is a full run at an exact SHA and an unreachable database
#       certifies nothing. If both forgave, an outage would leave no red
#       anywhere and the estate would believe it had been scanned.
#
#   tools/vuln/govulncheck.sh classify <log>
#       Print finding | transport | error for a failed run's log. One place
#       decides, and `make test` holds it to the fixtures below.
#
#   tools/vuln/govulncheck.sh selftest
#       The table in testdata/cases.txt over real log excerpts, run by
#       `make test`. A classifier that reads a compile error as an outage is
#       a gate that forgives a real failure, so the fixtures carry the two
#       that nearly happened: `gen.go:503:14: undefined: ...` and a panic
#       whose index is [504] are ERROR, never transport.
#
# WHY THE MARKERS ARE ANCHORED. The first cut of this matched a bare 500, 502,
# 503 or 504 anywhere in the log, and every Go file with 503 lines, every panic
# with [504] in an index and every `unexpected EOF` from the PARSER would have
# read as an outage. A status code is only a status code on a line that also
# names where it came from, so a number classifies as transport only beside one
# of the fetch hosts or an HTTP fetch line, and the bare markers are the ones a
# socket writes and a compiler never does: `dial tcp`, `TLS handshake timeout`,
# `no such host`, `connection refused`, `connection reset by peer`,
# `i/o timeout`, `network is unreachable`, `temporary failure in name
# resolution`.
set -o pipefail

ROOT=$(cd "$(dirname "$0")" && pwd)
VERSION=${GOVULNCHECK_VERSION:-v1.7.0}

# The hosts this gate fetches from: the database itself, and the module proxy
# `go run` pulls the pinned scanner through — an outage at either one is an
# outage between this runner and the answer, not a fact about the tree.
HOSTS='vuln\.go\.dev|proxy\.golang\.org|sum\.golang\.org|goproxy'

# A status code, only where it is one: on a line that names a fetch host, or
# on one of go/govulncheck's own HTTP lines.
HTTP_5XX="($HOSTS|HTTP GET|returned unexpected status|module lookup failed|reading https).*(\"?(500|502|503|504)\"?|Service Unavailable|Bad Gateway|Gateway Time-?out|Internal Server Error)"

# What a socket writes and a compiler does not.
SOCKET='dial tcp|TLS handshake timeout|no such host|connection refused|connection reset by peer|i/o timeout|network is unreachable|temporary failure in name resolution|server misbehaving'

# `unexpected EOF` is a Go SYNTAX error as often as a truncated response, so it
# counts only beside a host.
HOST_TRUNCATED="($HOSTS).*(unexpected EOF|context deadline exceeded|EOF$)"

# A finding, in govulncheck's own words. `Your code is affected by 0
# vulnerabilities.` is what a CLEAN run prints, so the count is part of the
# marker rather than the sentence alone.
FINDING='Vulnerability #[0-9]|Your code is affected by [1-9]'

classify() {
    if grep -qE "$FINDING" "$1"; then
        echo finding
    elif grep -qEi "$HTTP_5XX" "$1" || grep -qEi "$SOCKET" "$1" || grep -qEi "$HOST_TRUNCATED" "$1"; then
        echo transport
    else
        echo error
    fi
}

case "$1" in

# ------------------------------------------------------------------- classify
classify)
    [ -n "$2" ] || { echo "usage: tools/vuln/govulncheck.sh classify <log>" >&2; exit 2; }
    [ -f "$2" ] || { echo "govulncheck.sh: no such log: $2" >&2; exit 2; }
    classify "$2"
    ;;

# ------------------------------------------------------------------ selftest
selftest)
    CASES="$ROOT/testdata/cases.txt"
    [ -f "$CASES" ] || { echo "govulncheck.sh selftest: $CASES is missing" >&2; exit 1; }
    fails=0
    total=0
    while read -r fixture expect; do
        case "$fixture" in ''|'#'*) continue ;; esac
        total=$((total + 1))
        path="$ROOT/testdata/$fixture"
        if [ ! -f "$path" ]; then
            echo "selftest FAIL: $fixture is named in cases.txt and does not exist" >&2
            fails=$((fails + 1))
            continue
        fi
        got=$(classify "$path")
        if [ "$got" = "$expect" ]; then
            echo "selftest ok: $fixture -> $got"
        else
            echo "selftest FAIL: $fixture -> $got, expected $expect" >&2
            fails=$((fails + 1))
        fi
    done < "$CASES"
    if [ "$total" -eq 0 ]; then
        echo "selftest FAIL: cases.txt named no fixtures — a table test over an empty table is not a test" >&2
        exit 1
    fi
    if [ "$fails" -ne 0 ]; then
        echo "govulncheck selftest: $fails of $total FAILED — the classifier cannot be trusted to tell an outage from a failure; fix it before trusting this gate's green OR its red" >&2
        exit 1
    fi
    echo "govulncheck selftest: all $total fixtures classify as they must"
    ;;

# ----------------------------------------------------------------------- run
run)
    strict=0
    [ "$2" = "--strict" ] && strict=1

    # THREE ATTEMPTS AND TWO SLEEPS, inside the job's 10-minute cap with room
    # to spare: the scan itself measures 9.4 s on a 64-core Linux bench and 35 s
    # on a hosted ubuntu runner, so the worst case that still answers is
    # 3 x 35 s + 15 s + 45 s = 2:45. A scan that HANGS rather than failing is
    # what the cap is for, and it still ends the job.
    log=${RUNNER_TEMP:-${TMPDIR:-/tmp}}/govulncheck.log
    attempt=1
    while true; do
        if go run "golang.org/x/vuln/cmd/govulncheck@$VERSION" ./... 2>&1 | tee "$log"; then
            exit 0
        fi
        case "$(classify "$log")" in
        finding)
            echo "::error::govulncheck reported a vulnerability. That is a finding, not an outage — read the report above; this gate does not retry it."
            exit 1
            ;;
        error)
            echo "::error::govulncheck failed for a reason that is not a network failure. Read the output above; this gate does not retry it."
            exit 1
            ;;
        esac
        if [ "$attempt" -ge 3 ]; then
            if [ "$strict" -eq 1 ]; then
                echo "::error::govulncheck could not reach its vulnerability database in three attempts. A certificate is a full run at an exact SHA, and an unreachable database certifies nothing — this run is RED on purpose. The pull-request gate forgives the same outage; this one may not."
                exit 1
            fi
            echo "::notice::govulncheck could not reach its vulnerability database in three attempts — an upstream outage, not a finding. THIS GATE PROVED NOTHING on this run and is deliberately not failing a pull request for it; the nightly certification runs the same gate under --strict and goes RED until the database answers."
            exit 0
        fi
        echo "attempt $attempt could not reach the vulnerability database; retrying"
        sleep $((attempt == 1 ? 15 : 45))
        attempt=$((attempt + 1))
    done
    ;;

*)
    echo "usage: tools/vuln/govulncheck.sh run [--strict] | classify <log> | selftest" >&2
    exit 2
    ;;
esac
