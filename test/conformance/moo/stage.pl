# stage.pl — the moo leg's IN half (test/conformance/moo/driver).
#
#   perl stage.pl <manifest> <surface> <stage>
#
# The moo CLI has no io, so what a surface reads goes in as SOURCE:
#
#   <stage>/manifest.moo  the derived manifest's text, as one string literal
#   <stage>/inputs.moo    every fixture the surface's lines name, as a frozen
#                         table of buffers keyed by the repo-relative path the
#                         manifest spells (the working directory is the
#                         repository root, so no path is resolved here)
#
# A surface reads only its own kinds of line, so only those lines' files are
# staged: a fixture the surface never opens is never encoded. A token is staged
# when it names a regular file; a json-read instance's text lives at
# testdata/conformance/tables/json/<name>.json and a json-hostile tree's at
# <tree>/<root>.json (test/conformance/README.md, the surfaces), and those two
# are derived here the way the README states them.
use strict;
use warnings;

my ($manifest, $surface, $stage, $generated) = @ARGV;
die "usage: stage.pl <manifest> <surface> <stage> <generated>\n" unless defined $stage;

open(my $in, "<:raw", $manifest) or die "stage.pl: $manifest: $!\n";
my $text = do { local $/; <$in> };
close $in;
$text = "" unless defined $text;

sub literal {
	my ($bytes) = @_;
	my $h = unpack("H*", $bytes);
	$h =~ s/(..)/\\x$1/g;
	return "\"$h\"";
}

sub emit {
	my ($path, $body) = @_;
	open(my $out, ">:raw", "$path.tmp") or die "stage.pl: $path: $!\n";
	print $out $body;
	close $out or die "stage.pl: $path: $!\n";
	rename("$path.tmp", $path) or die "stage.pl: $path: $!\n";
}

emit("$stage/manifest.moo",
	"--!strict\n-- written by test/conformance/moo/stage.pl: the derived manifest\nreturn " . literal($text) . "\n");

# the surface modules beside main.moo (block.moo, ...), one per table form: a
# form lands its answers by adding its file, never by editing main.moo
opendir(my $dh, "test/conformance/moo") or die "stage.pl: test/conformance/moo: $!\n";
my @plugins = sort grep { /^[a-z][a-z0-9_]*$/ } map { /^(.*)\.moo$/ ? $1 : () } readdir($dh);
closedir $dh;
@plugins = grep { $_ ne "main" } @plugins;
emit("$stage/plugins.moo",
	"--!strict\n-- written by test/conformance/moo/stage.pl: the surface modules beside main.moo\nreturn table.freeze({" . join(", ", map { "\"$_\"" } @plugins) . "})\n");

# the table modules each generated unit carries (<Base>Table.moo): requiring one
# registers its descriptors, and moo cannot list a directory, so the shim does
my @units;
if (defined $generated && opendir(my $gd, $generated)) {
	for my $u (sort grep { !/^\./ && -d "$generated/$_" } readdir($gd)) {
		my @mods = sort map { /^(.*Table)\.moo$/ ? $1 : () } do { opendir(my $ud, "$generated/$u") or die "stage.pl: $generated/$u: $!\n"; readdir($ud) };
		push @units, "\t[\"$u\"] = {" . join(", ", map { "\"$_\"" } @mods) . "},\n";
	}
	closedir $gd;
}
emit("$stage/tables.moo",
	"--!strict\n-- written by test/conformance/moo/stage.pl: each unit's <Base>Table modules\nreturn table.freeze({\n" . join("", @units) . "})\n");

my %kinds = (
	"list"          => [],
	"wire"          => ["instance"],
	"json-write"    => ["instance"],
	"cook-write"    => ["instance"],
	"json-read"     => ["instance"],
	"report"        => ["report"],
	"retain"        => ["retain", "retain-message", "connection"],
	"retain-save"   => ["retain", "retain-message", "connection"],
	"message"       => ["connection", "message"],
	"json-hostile"  => ["json-hostile"],
	"cook"          => ["cook"],
	"cook-foreign"  => ["cook"],
	"block"         => ["block"],
	"block-foreign" => ["block"],
	"block-dump"    => ["block"],
	"forgery"       => ["forgery"],
	"cook-forgery"  => ["forgery"],
	"block-reason"  => ["refusal", "forgery"],
	"cook-reason"   => ["refusal", "forgery"],
);
my %want = map { $_ => 1 } @{ $kinds{$surface} || [] };

my %files;
for my $line (split /\n/, $text) {
	$line =~ s/^\s+|\s+$//g;
	next if $line eq "" || substr($line, 0, 1) eq "#";
	my @f = split /\s+/, $line;
	next unless $want{ $f[0] };
	for my $t (@f[1 .. $#f]) {
		$files{$t} = 1 if -f $t;
	}
	if ($f[0] eq "instance" && $surface eq "json-read") {
		my $j = "testdata/conformance/tables/json/$f[1].json";
		$files{$j} = 1 if -f $j;
	}
	if ($f[0] eq "json-hostile" && defined $f[4]) {
		my $j = "$f[4]/$f[3].json";
		$files{$j} = 1 if -f $j;
	}
}

my $body = "--!strict\n-- written by test/conformance/moo/stage.pl: the fixtures the $surface surface reads\nreturn table.freeze({\n";
for my $path (sort keys %files) {
	open(my $f, "<:raw", $path) or die "stage.pl: $path: $!\n";
	my $data = do { local $/; <$f> };
	close $f;
	$data = "" unless defined $data;
	$body .= "\t[" . literal($path) . "] = buffer.fromstring(" . literal($data) . "),\n";
}
$body .= "})\n";
emit("$stage/inputs.moo", $body);
