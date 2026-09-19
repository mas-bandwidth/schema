package main

// THE RUN COPY'S 17..31-BYTE BRANCH, PUT BACK THE WAY IT WAS. Both legs that
// carry the fixed form in a systems language emit the same primitive, and both
// shipped the same defect: the branch's tail move was a THIRTY-TWO anchored at
// the run's end, so for a run of 17..31 bytes it began 32 - n bytes IN FRONT of
// the run. It read and wrote a neighbour's bytes and it read past the record
// body, which for a file's last record is past the buffer — and every counter
// and verdict in the report stayed clean.
//
// The control plants exactly that anchor back and requires the sanitized run
// copy test to name the overflow. A gate nobody has watched go red is a gate
// nobody knows is wired up.

func init() {
	sabotages["reference-run-copy-anchor"] = []edit{{
		old: `    if ( n <= 32 )
    {
        uint64_t w[2];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + n - 16, 16 ); memcpy( d + n - 16, w, 16 );
        return;
    }`,
		new: `    if ( n <= 32 )
    {
        uint64_t w[4];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        // SABOTAGED: a THIRTY-TWO anchored at the run's end, which for n < 32
        // begins in front of the run
        memcpy( w, s + n - 32, 32 ); memcpy( d + n - 32, w, 32 );
        return;
    }`,
	}}

	sabotages["reference-run-copy-anchor-c"] = []edit{{
		old: `    if ( n <= 32 )
    {
        uint64_t w[2];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        memcpy( w, s + n - 16, 16 ); memcpy( d + n - 16, w, 16 );
        return;
    }`,
		new: `    if ( n <= 32 )
    {
        uint64_t w[4];
        memcpy( w, s, 16 ); memcpy( d, w, 16 );
        /* SABOTAGED: a THIRTY-TWO anchored at the run's end, which for n < 32
           begins in front of the run */
        memcpy( w, s + n - 32, 32 ); memcpy( d + n - 32, w, 32 );
        return;
    }`,
	}}
}
