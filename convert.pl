#!/usr/bin/env perl

use strict;
use warnings;

use MaxMind::DB::Writer::Tree;
use Net::Works::Network;

my %types = (
    ufi      => 'int32',
);

my $tree = MaxMind::DB::Writer::Tree->new(
    database_type => 'IP2UFI',
    description => { en => 'IP to UFI mapping' },
    ip_version => 4,
    map_key_type_callback => sub { $types{ $_[0] } },

    # "record_size" is the record size in bits.  Either 24, 28 or 32.
    record_size => 32,
);


my $prevIP = 0;
my $lines = 0;
while (my $line = <>) {
    my ($ipTo, $ufi) = split /,/, $line;
    my $ipFrom = $prevIP + 1;

    my $fromstr = join ".", unpack "C4", pack "N", $ipFrom;
    my $tostr = join ".", unpack "C4", pack "N", $ipTo;

    my @subnets = Net::Works::Network->range_as_subnets($fromstr, $tostr);
    for my $subnet (@subnets) {
        my $network = Net::Works::Network->new_from_string( string => $subnet );
        $tree->insert_network( $network, { ufi => $ufi });
    }
    $prevIP = $ipTo;

    $lines++;
    if (($lines % 10000) == 0) {
        print scalar localtime, ": processed $lines\n";
    }
}

open my $fh, '>:raw', "ip2ufi.mmdb";
$tree->write_tree( $fh );
close $fh;
