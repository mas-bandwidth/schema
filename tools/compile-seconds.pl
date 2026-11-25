#!/usr/bin/perl
# compile-seconds.pl: run one compile and record the CPU seconds it took.
#
#   perl tools/compile-seconds.pl <seconds-file> <command> [args...]
#
# Runs the command and waits for it. When it succeeds, <seconds-file> gets one
# line, the user plus system CPU seconds of the command and every process it
# waited for (the compiler driver's cc1plus and as), to two places. CPU time
# rather than wall time: a unit's cost is what the compiler did, which a
# loaded machine running other jobs beside it does not change much.
#
# The file is removed before the command runs, so a failed compile leaves no
# timing behind; the command's own failure is this script's exit status (1
# for a command killed by a signal), and a command that cannot be started, or
# a file that cannot be written, fails naming it. Nothing is swallowed.
#
# The C++ conformance driver's units are compiled through it (Makefile,
# CONFORMANCE_DRIVER_TUS), and tools/compilebudget holds every unit's file to
# the recorded budget.
use strict;
use warnings;

die "usage: compile-seconds.pl <seconds-file> <command> [args...]\n" if @ARGV < 2;
my $out = shift @ARGV;
if ( -e $out ) {
    unlink $out or die "compile-seconds.pl: cannot remove the old $out: $!\n";
}
my $rc = system { $ARGV[0] } @ARGV;
die "compile-seconds.pl: cannot run $ARGV[0]: $!\n" if $rc == -1;
if ( $rc != 0 ) {
    my $status = $rc >> 8;
    exit( $status != 0 ? $status : 1 );
}
my @t = times;
open my $fh, '>', $out or die "compile-seconds.pl: cannot write $out: $!\n";
printf {$fh} "%.2f\n", $t[2] + $t[3] or die "compile-seconds.pl: cannot write $out: $!\n";
close $fh or die "compile-seconds.pl: cannot write $out: $!\n";
exit 0;
