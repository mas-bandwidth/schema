# answer-lines.pl — the OUT half of every driver whose program answers on
# stdout (lobster, luau, goose, moo): ONE strict parser, so a stray line is
# never read as an answer (test/conformance/README.md, "The driver's stdout").
#
#   perl answer-lines.pl [--continue] <outdir> [<file>...]     (stdin when no file)
#
# The grammar of a line, whole:
#
#   <case> <hex>              the answer's bytes, lowercase hex (possibly empty)
#   <case> absent [<reason>]  the case is ABSENT; the reason, if any, is the
#                             bytes of <case>.absent
#
# <case> is [A-Za-z0-9_.-]+ and is neither `.` nor `..`; <hex> is an even run
# of [0-9a-f]. Each answered case becomes the file <outdir>/<case>. A repeated
# case replaces the file, except under --continue (the lobster leg splits a long
# answer over several lines): then the later lines are appended to it.
#
# Any other line is the driver failing, never a case silently dropped, and
# never a wrong answer: a compiler or interpreter warning that reached stdout
# stops here as `driver printed non-protocol line: <line>`, exit 1. Blank
# lines carry nothing and are skipped.
use strict;
use warnings;

my $continue = 0;
if (@ARGV && $ARGV[0] eq "--continue") { shift @ARGV; $continue = 1; }
my $outdir = shift @ARGV;
die "usage: answer-lines.pl [--continue] <outdir> [<file>...]\n" unless defined $outdir;

my %seen;
while (my $line = <>) {
	chomp $line;
	next if $line eq "";
	my ($case, $rest) = $line =~ /\A([A-Za-z0-9_.-]+)(?: (.*))?\z/s;
	my ($reason, $hex);
	if (defined $case && $case ne "." && $case ne "..") {
		if (!defined $rest) { $hex = ""; }    # the bare case name: an empty answer
		elsif ($rest =~ /\Aabsent(?: (.+))?\z/s) { $reason = defined $1 ? $1 : ""; }
		elsif ($rest =~ /\A(?:[0-9a-f]{2})*\z/) { $hex = $rest; }
	}
	if (!defined $reason && !defined $hex) {
		print STDERR "driver printed non-protocol line: $line\n";
		exit 1;
	}
	if (defined $reason) {
		open(my $f, ">", "$outdir/$case.absent") or die "answer-lines.pl: $case: $!\n";
		print $f $reason;
		close $f or die "answer-lines.pl: $case: $!\n";
		next;
	}
	my $mode = ($continue && $seen{$case}++) ? ">>:raw" : ">:raw";
	open(my $f, $mode, "$outdir/$case") or die "answer-lines.pl: $case: $!\n";
	print $f pack("H*", $hex);
	close $f or die "answer-lines.pl: $case: $!\n";
}
