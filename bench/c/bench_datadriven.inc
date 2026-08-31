/* bench_datadriven.inc — the DATA-DRIVEN benchmark driver body (issue #191),
   expanded once per message type by #define + #include, exactly as
   bench_message.inc is.

   Parameters, defined by the includer and consumed (undefined) here:
     BM_SUFFIX   function-name suffix: the expansion is bench_datadriven_<BM_SUFFIX>
     BM_TYPE     the generated struct
     BM_WRITE    the generated write entry ( serialize_write_stream_t *, const BM_TYPE * )
     BM_READ     the generated read entry  ( serialize_read_stream_t *, BM_TYPE * )

   THE PROPERTY: nothing below names a field of the shape it measures. Shape
   knowledge lives in the committed variant DATA (bench/corpus/variants,
   emitted by bench/tools/variantgen) and in the generated codec, and nowhere
   else — so this driver cannot drift from another language's driver in what
   it measures, which is the whole reason the design exists. BM_TYPE is a TYPE
   name, not a field name; there is no BM_VARY, because there is nothing left
   to vary by hand.

   It replaces bench_message.inc for bench_mixed only. bench_message.inc still
   drives every shape whose harness code is not yet data-driven. */

#define BM_CAT2( a, b ) a##b
#define BM_CAT( a, b ) BM_CAT2( a, b )
#define BM_FUNC BM_CAT( bench_datadriven_, BM_SUFFIX )

static BM_TYPE BM_CAT( g_instances_, BM_SUFFIX )[NumVariants];

static void BM_FUNC( const char * name, const char * golden, long base_iters )
{
    const long iters = base_iters / IterScale;
    BM_TYPE * instances = BM_CAT( g_instances_, BM_SUFFIX );
    BM_TYPE out;
    int bytes_per_op;
    double write_rates[MaxNumRuns];
    double roundtrip_rates[MaxNumRuns];
    RunStats w, rt;
    double read_time;
    int run, k;
    long i;

    bytes_per_op = load_variants( name );
    if ( bytes_per_op < 0 )
    {
        failed = 1;
        return;
    }

    /* gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
       file is bound to the wire golden by one byte-compare. */
    if ( !check_golden( golden, g_variant( 0 ), bytes_per_op ) )
    {
        failed = 1;
        return;
    }

    /* gate 2: every variant decodes, re-encodes, and comes back byte-identical
       at the same length. This is stronger than the pinned-instance-only gate
       bench_message.inc applies — §1.5's named residual (the 64 varied buffers
       length-checked but never value-checked) closes here, for every variant. */
    for ( k = 0; k < NumVariants; k++ )
    {
        serialize_read_stream_t rs;
        serialize_write_stream_t ws;
        memset( &instances[k], 0, sizeof( instances[k] ) );
        serialize_read_stream_init( &rs, g_variant( k ), bytes_per_op );
        if ( !BM_READ( &rs, &instances[k] ) )
        {
            fail( name, "decode of a variant failed" );
            return;
        }
        serialize_write_stream_init( &ws, g_twin, BufferSize );
        if ( !BM_WRITE( &ws, &instances[k] ) )
        {
            fail( name, "re-encode of a decoded variant failed" );
            return;
        }
        serialize_write_flush( &ws );
        if ( serialize_write_bytes_processed( &ws ) != bytes_per_op ||
             memcmp( g_twin, g_variant( k ), (size_t) bytes_per_op ) != 0 )
        {
            fail( name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus" );
            return;
        }
    }

    /* WRITE path: 1 warmup + NumRuns measured. Encodes the 64 pre-decoded
       instances round-robin. Rotating the instances is what §2.7's
       per-iteration LCG mutation bought — the encoder never sees the same
       input twice in a row and cannot precompute scratch words — with none
       of the per-language mutation code, and with bytes/op constant by
       construction rather than by assertion. The sink is the byte fold. */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            serialize_write_stream_t stream;
            serialize_write_stream_init( &stream, g_buffer, BufferSize );
            if ( !BM_WRITE( &stream, &instances[i & ( NumVariants - 1 )] ) )
            {
                fail( name, "write failed in loop" );
                return;
            }
            serialize_write_flush( &stream );
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &stream );
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            write_rates[run] = (double) iters / elapsed;
    }

    /* ROUND-TRIP path: 1 warmup + NumRuns measured. Decodes a variant buffer,
       then re-encodes what came out. The decode needs no sink discipline of
       its own — its output IS the encode's input, so every decoded field is
       observed by construction, in every language, with no per-language fold
       to audit (§2.7's read-side sink problem dissolved rather than
       equalized). The decode target is hoisted and reused, as everywhere. */
    memset( &out, 0, sizeof( out ) );
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            serialize_read_stream_t rs;
            serialize_write_stream_t ws;
            serialize_read_stream_init( &rs, g_variant( i & ( NumVariants - 1 ) ), bytes_per_op );
            if ( !BM_READ( &rs, &out ) )
            {
                fail( name, "read failed in loop" );
                return;
            }
            serialize_write_stream_init( &ws, g_buffer, BufferSize );
            if ( !BM_WRITE( &ws, &out ) )
            {
                fail( name, "re-write failed in loop" );
                return;
            }
            serialize_write_flush( &ws );
            bench_escape( g_buffer );
            g_sink = g_sink + (uint64_t) serialize_write_bytes_processed( &ws );
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            roundtrip_rates[run] = (double) iters / elapsed;
    }

    w = run_stats( write_rates, g_num_runs );
    rt = run_stats( roundtrip_rates, g_num_runs );
    report( name, "write", iters, bytes_per_op, w, "gen" );
    report( name, "round_trip", iters, bytes_per_op, rt, "gen" );

    /* READ is DERIVED, never measured: round-trip time minus write time. It
       prints for continuity with the read rows the rest of the corpus still
       reports and is NOT a CSV row — a derived number in the CSV would be
       divided as if it had been measured. */
    read_time = 1.0 / rt.median_rate - 1.0 / w.median_rate;
    if ( read_time > 0 )
    {
        fprintf( stderr, "%-18s %-5s %10.2f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
                 name, "read", 1e-6 / read_time );
    }
}

#undef BM_FUNC
#undef BM_CAT
#undef BM_CAT2
#undef BM_SUFFIX
#undef BM_TYPE
#undef BM_WRITE
#undef BM_READ
