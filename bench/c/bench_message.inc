/* bench_message.inc — the per-message benchmark driver body, expanded once
   per message type by #define + #include (C's answer to the reference's
   function template, in its second form).

   Parameters, defined by the includer and consumed (undefined) here:
     BM_SUFFIX   function-name suffix: the expansion is bench_message_<BM_SUFFIX>
     BM_TYPE     the generated struct
     BM_WRITE    the generated write entry ( serialize_write_stream_t *, const BM_TYPE * )
     BM_READ     the generated read entry  ( serialize_read_stream_t *, BM_TYPE * )
     BM_VARY     the per-iteration mutation ( BM_TYPE *, uint64_t )

   Until 2026-08-15 this body was the BENCH_MESSAGE macro. The preprocessor
   output is token-for-token identical either way — the measured code does
   not change — but a backslash-continued macro puts every statement of every
   expansion on the macro invocation's single line, which made per-bench
   verdict attribution impossible: the §4 pass could see a runtime call in
   bench_message_chat and not say whether it sat in the timed write loop, the
   timed read loop, or the untimed self-checks. As an #include template every
   expansion carries this file's real line numbers, and the inline-stack walk
   (bench/tools/inline-verdict.sh, c branch — same technique as cpp) can
   attribute each remaining call to (bench, path) or exclude it as untimed.
   The verdict pass greps this file for the write-path and read-path warmup
   comments and the first report call to find the timed line ranges — do not
   reword those without updating the c branch of inline-verdict.sh. */

#define BM_CAT2( a, b ) a##b
#define BM_CAT( a, b ) BM_CAT2( a, b )
#define BM_FUNC BM_CAT( bench_message_, BM_SUFFIX )

static void BM_FUNC( const char * name, const char * golden, long base_iters,
                                    const BM_TYPE * pinned )
{
    const long iters = base_iters / IterScale;
    BM_TYPE base = *pinned;
    BM_TYPE out;
    serialize_write_stream_t ws;
    int bytes_per_op;
    uint64_t rng;
    double write_rates[MaxNumRuns];
    double read_rates[MaxNumRuns];
    int run, k;
    long i;

    /* self-check 1: the pinned instance matches its wire golden byte-for-byte */
    serialize_write_stream_init( &ws, g_buffer, BufferSize );
    if ( !BM_WRITE( &ws, &base ) )
    {
        fail( name, "write of pinned instance failed" );
        return;
    }
    serialize_write_flush( &ws );
    bytes_per_op = serialize_write_bytes_processed( &ws );
    if ( golden && !check_golden( golden, g_buffer, bytes_per_op ) )
    {
        failed = 1;
        return;
    }

    /* self-check 2: round-trip write -> read -> re-write -> identical bytes */
    {
        serialize_read_stream_t rs;
        serialize_write_stream_t tws;
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &rs, g_buffer, bytes_per_op );
        if ( !BM_READ( &rs, &out ) )
        {
            fail( name, "read of pinned instance failed" );
            return;
        }
        serialize_write_stream_init( &tws, g_twin, BufferSize );
        if ( !BM_WRITE( &tws, &out ) )
        {
            fail( name, "re-write of decoded instance failed" );
            return;
        }
        serialize_write_flush( &tws );
        if ( serialize_write_bytes_processed( &tws ) != bytes_per_op ||
             memcmp( g_buffer, g_twin, (size_t) bytes_per_op ) != 0 )
        {
            fail( name, "round-trip bytes differ" );
            return;
        }
    }

    /* variant buffers for the read path (and proof that variation keeps bytes/op constant) */
    rng = 1;
    for ( k = 0; k < NumVariants; k++ )
    {
        serialize_write_stream_t vs;
        rng = bench_rng( rng );
        BM_VARY( &base, rng );
        serialize_write_stream_init( &vs, g_variant( k ), BufferSize );
        if ( !BM_WRITE( &vs, &base ) )
        {
            fail( name, "write of varied instance failed" );
            return;
        }
        serialize_write_flush( &vs );
        if ( serialize_write_bytes_processed( &vs ) != bytes_per_op )
        {
            fail( name, "variation changed bytes/op — vary must keep structure fields fixed" );
            return;
        }
    }

    /* write path: 1 warmup + NumRuns measured */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            serialize_write_stream_t stream;
            rng = bench_rng( rng );
            BM_VARY( &base, rng );
            serialize_write_stream_init( &stream, g_buffer, BufferSize );
            if ( !BM_WRITE( &stream, &base ) )
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

    /* read path: 1 warmup + NumRuns measured; ONE decode instance hoisted out
       of the loop and reused, matching the reference (a fresh instance per
       iteration is constructed+zeroed harness overhead, not serialize work) */
    for ( run = -1; run < g_num_runs; run++ )
    {
        double start = time_now();
        double elapsed;
        for ( i = 0; i < iters; i++ )
        {
            serialize_read_stream_t stream;
            serialize_read_stream_init( &stream, g_variant( i & ( NumVariants - 1 ) ), bytes_per_op );
            if ( !BM_READ( &stream, &out ) )
            {
                fail( name, "read failed in loop" );
                return;
            }
            bench_escape( &out );   /* every decoded field is observed */
            g_sink = g_sink + 1;
        }
        elapsed = time_now() - start;
        if ( run >= 0 )
            read_rates[run] = (double) iters / elapsed;
    }

    report( name, "write", iters, bytes_per_op, run_stats( write_rates, g_num_runs ), "gen" );
    report( name, "read", iters, bytes_per_op, run_stats( read_rates, g_num_runs ), "gen" );
}

#undef BM_FUNC
#undef BM_CAT
#undef BM_CAT2
#undef BM_SUFFIX
#undef BM_TYPE
#undef BM_WRITE
#undef BM_READ
#undef BM_VARY
