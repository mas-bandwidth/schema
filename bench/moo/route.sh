#!/bin/sh
# route.sh — stages corpus bytes as MOO source (`inputs.moo`) alongside a
# generated unit and runs one MOO bench gate under the pinned interpreter.
set -eu

ROOT=$(pwd -P)
MOO=${MOO:-$ROOT/dist/moo/moo}
NAME=$1
PROGRAM=$2
GEN=$3
ROW=$4
shift 4
[ "${1:-}" = "--" ] && shift

WIRE_DIR="$ROOT/testdata/wire"
VARIANT_DIR="$ROOT/bench/corpus/variants"
GATE=0
while [ $# -gt 0 ]; do
    case "$1" in
        --gate) GATE=1; shift ;;
        --wire-dir) WIRE_DIR=$2; shift 2 ;;
        --variant-dir) VARIANT_DIR=$2; shift 2 ;;
        *) shift ;;
    esac
done

if [ "$GATE" -ne 1 ]; then
    echo "moo CLI is deterministic (no internal clock): pass --gate" >&2
    exit 2
fi

STAGE="build/bench-moo/$NAME"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp "$GEN"/*.moo "$STAGE/"
cp "$PROGRAM" "$STAGE/main.moo"

perl -e '
    my ($gpath, $vpath) = @ARGV;
    sub sl {
        open(my $f, "<:raw", $_[0]) or die "$_[0]: $!";
        local $/;
        my $d = <$f>;
        close $f;
        $d = "" unless defined $d;
        my $h = unpack("H*", $d);
        $h =~ s/(..)/\\x$1/g;
        return $h;
    }
    my $g = sl($gpath);
    my $v = sl($vpath);
    print "--!strict\nreturn table.freeze({\n\tgolden = buffer.fromstring(\"$g\"),\n\tvariants = buffer.fromstring(\"$v\"),\n})\n";
' "$WIRE_DIR/$ROW.bin" "$VARIANT_DIR/$ROW.variants.bin" > "$STAGE/inputs.moo"

"$MOO" "$STAGE/main.moo" >&2
