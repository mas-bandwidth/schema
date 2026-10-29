# split.pl — the moo leg's OUT half (test/conformance/moo/driver).
#
#   perl split.pl <main.moo's stdout> <outdir>
#
# Every line main.moo prints for a surface is `<case> <hex>` (the answer's
# bytes, lowercase hex, possibly empty) or `<case> absent`, and becomes ONE FILE
# PER CASE in <outdir>: the bytes as <case>, or the empty <case>.absent the
# harness counts as a per-case absence (test/conformance/README.md;
# docs/PORTING.md I4). Any other line is the driver failing, never a case
# silently dropped.
use strict;
use warnings;

my ($from, $outdir) = @ARGV;
die "usage: split.pl <stdout> <outdir>\n" unless defined $outdir;
open(my $in, "<", $from) or die "split.pl: $from: $!\n";
while (my $line = <$in>) {
	chomp $line;
	next if $line eq "";
	my ($case, $answer) = $line =~ /\A(\S+) ?(.*)\z/s
		or die "split.pl: not a case line: $line\n";
	die "split.pl: a case name may not leave the output directory: $case\n"
		if $case =~ m{/} || $case =~ /\A\.\.?\z/;
	if ($answer =~ /\Aabsent(?: (\S+))?\z/) {
		# a named absence carries its reason as the file's bytes (the blob_shared
		# writer refusal, docs/SPEC-TABLES.md §2.5), a plain one is empty
		my $reason = defined $1 ? $1 : "";
		open(my $f, ">", "$outdir/$case.absent") or die "split.pl: $case: $!\n";
		print $f $reason;
		close $f or die "split.pl: $case: $!\n";
		next;
	}
	die "split.pl: $case: the answer is not hex: $answer\n"
		unless $answer =~ /\A(?:[0-9a-f]{2})*\z/;
	open(my $f, ">:raw", "$outdir/$case") or die "split.pl: $case: $!\n";
	print $f pack("H*", $answer);
	close $f or die "split.pl: $case: $!\n";
}
close $in;
