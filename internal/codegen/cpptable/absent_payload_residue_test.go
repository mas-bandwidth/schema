package cpptable

// TestCppAbsentPayloadResidueNotCopied pins docs/SPEC-TABLES.md §3.4 on the
// C++ fixed form: an optional's payload rides WHOLE whether or not it is
// present, is ZERO on write when the flag is 0, and is IGNORED on read — an
// absent optional reads as its DECLARED DEFAULT, never as the residue a peer
// left under the flag.
//
// The probe saves an absent optional (so the writer lays the template's zeros
// under the flag), stains the payload bytes on the wire with values a clean
// record never carries, and reads the file through the generated strict
// reader. The absent value must be Leaf.value's declared default (30) and its
// text length must be 0. The same payload PRESENT must still land from the
// wire, so the check discriminates on the flag and not on the writer never
// having written a payload at all.
//
// The probe also carries a UNION-BEARING payload (Outer.opt ?Holder, Holder's
// u U): the union's arm entries are guarded and ride in the plan's second half,
// so an unguarded defaults copy that ran in the first half would be overwritten
// by the arm's wire bytes. The absent read must still answer the declared
// defaults, and the same payload present must land from the wire.
//
// The negative control plants the fault in the real artifact: it removes the
// generated runtime's copy of the declared defaults under an absent present
// byte, and requires the same probe to go red.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppAbsentPayloadResidueSchema = `package absentresidue

fixed table Leaf
{
    value int32 = 30 | min = 0, max = 1000
    tag   string(8)
}

fixed table Chain
{
    name string(16)
    link ?Leaf
}

type Inner
{
    value int32 = 7 | min = 0, max = 1000
    tag   string(8)
}

type Other
{
    n int32 = 9
}

union U
{
    // a IS DECLARED LAST SO ITS DEFAULTS ARE THE UNION IMAGE'S LAST WORD: the
    // Reset lays every arm in order over the same storage, and a.value = 7 is
    // the declared default this probe looks for under an absent flag.
    b Other
    a Inner
}

type Holder
{
    u U
}

fixed table Outer
{
    name string(8)
    opt  ?Holder
}
`

const cppAbsentPayloadResidueMain = `#include <cstdio>
#include <cstring>
#include <cstdint>

int main( void )
{
    // The record body starts after the header, the layout length, the layout,
    // and the record's hash. name is string(16): 4 + 16 bytes. Then the
    // present byte, then Leaf.value (4) and Leaf.tag_length (4) + tag (8).
    static const int32_t kPresentAt = 20;
    static const int32_t kValueAt = 21;
    static const int32_t kTagLengthAt = 25;
    static const int32_t kTagAt = 29;

    Chain v;
    ChainReset( v );
    std::strcpy( v.name, "absent" );
    v.name_length = 6;
    v.link_present = false;
    v.link.value = 0x5A5A5A5A;
    v.link.tag_length = 4;
    std::strcpy( v.link.tag, "rust" );

    static uint8_t file[4096];
    const int64_t need = ChainFixedMeasure( 1 );
    if ( ChainFixedSave( &v, 1, file, (int64_t) sizeof( file ) ) != need )
    {
        std::printf( "save: the absent record did not save whole\n" );
        return 1;
    }
    uint8_t * body = file + kTableFixedHeaderBytes + 4 + ChainFixedLayoutBytes + 8;
    if ( body[kPresentAt] != 0 )
    {
        std::printf( "write: the absent present flag is %d, want 0\n", (int) body[kPresentAt] );
        return 1;
    }
    for ( int32_t i = 0; i < 16; ++i )
    {
        if ( body[kValueAt + i] != 0 )
        {
            std::printf( "write: absent payload byte %d is %d, want the template's zero\n", (int) i, (int) body[kValueAt + i] );
            return 1;
        }
    }

    // THE CLEAN FILE: the writer's zeros under an absent flag must read as the
    // payload's DECLARED DEFAULT (Leaf.value = 30), not as zero.
    {
        Chain back;
        ChainReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = ChainFixedLoad( &back, 1, file, need, plan, 256, NULL, &r );
        if ( n != 1 || back.link_present )
        {
            std::printf( "clean: n=%lld present=%d\n", (long long) n, (int) back.link_present );
            return 1;
        }
        if ( back.link.value != 30 || back.link.tag_length != 0 )
        {
            std::printf( "clean: an absent optional did not read its declared defaults: value=%d tag_length=%d\n",
                         (int) back.link.value, (int) back.link.tag_length );
            return 1;
        }
        if ( r.damaged || r.refused )
        {
            std::printf( "clean: a clean absent read moved a counter\n" );
            return 1;
        }
    }

    // THE RESIDUE: stain the payload under the absent flag. The reader must
    // still answer the declared default, not the bytes a peer left there.
    body[kValueAt + 0] = 100;
    body[kValueAt + 1] = 0;
    body[kValueAt + 2] = 0;
    body[kValueAt + 3] = 0;
    body[kTagLengthAt + 0] = 4;
    body[kTagLengthAt + 1] = 0;
    body[kTagLengthAt + 2] = 0;
    body[kTagLengthAt + 3] = 0;
    std::memcpy( body + kTagAt, "rust", 4 );
    {
        Chain back;
        ChainReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = ChainFixedLoad( &back, 1, file, need, plan, 256, NULL, &r );
        if ( n != 1 || back.link_present )
        {
            std::printf( "residue: n=%lld present=%d\n", (long long) n, (int) back.link_present );
            return 1;
        }
        if ( back.link.value != 30 || back.link.tag_length != 0 )
        {
            std::printf( "ABSENT PAYLOAD RESIDUE: value=%d tag_length=%d, want the declared defaults 30/0\n",
                         (int) back.link.value, (int) back.link.tag_length );
            return 1;
        }
        if ( r.damaged || r.refused )
        {
            std::printf( "residue: a forged payload under an absent flag is not damage\n" );
            return 1;
        }
    }

    // THE DISCRIMINATING HALF: the same payload PRESENT lands from the wire.
    {
        Chain p;
        ChainReset( p );
        std::strcpy( p.name, "present" );
        p.name_length = 7;
        p.link_present = true;
        p.link.value = 77;
        p.link.tag_length = 2;
        std::strcpy( p.link.tag, "hi" );
        static uint8_t pfile[4096];
        if ( ChainFixedSave( &p, 1, pfile, (int64_t) sizeof( pfile ) ) != need )
        {
            std::printf( "present: the record did not save whole\n" );
            return 1;
        }
        Chain back;
        ChainReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = ChainFixedLoad( &back, 1, pfile, need, plan, 256, NULL, &r );
        if ( n != 1 || !back.link_present || back.link.value != 77 || back.link.tag_length != 2 ||
             std::memcmp( back.link.tag, "hi", 2 ) != 0 )
        {
            std::printf( "present: n=%lld present=%d value=%d tag_length=%d\n", (long long) n,
                         (int) back.link_present, (int) back.link.value, (int) back.link.tag_length );
            return 1;
        }
    }

    // THE UNION-BEARING PAYLOAD: an optional whose payload is a struct holding a
    // union. The union's arm entries are GUARDED, so they ride in the plan's
    // second half; the absent-defaults entry must run after them or the arm's
    // wire bytes land on top of the defaults. Outer's body: name string(8) is
    // 4 + 8 bytes, then the present byte, the union tag, and arm a's value,
    // tag_length and buffer.
    static const int32_t kOuterPresentAt = 12;
    static const int32_t kOuterTagAt = 13;
    static const int32_t kOuterValueAt = 14;
    static const int32_t kOuterTagLengthAt = 18;
    static const int32_t kOuterTagBufferAt = 22;

    Outer o;
    OuterReset( o );
    std::strcpy( o.name, "outer" );
    o.name_length = 5;
    o.opt_present = false;
    o.opt.u.SelectA();
    o.opt.u.a.value = 0x5A5A5A5A;
    o.opt.u.a.tag_length = 4;
    std::strcpy( o.opt.u.a.tag, "rust" );

    static uint8_t ofile[4096];
    const int64_t oneed = OuterFixedMeasure( 1 );
    if ( OuterFixedSave( &o, 1, ofile, (int64_t) sizeof( ofile ) ) != oneed )
    {
        std::printf( "outer: the absent record did not save whole\n" );
        return 1;
    }
    uint8_t * obody = ofile + kTableFixedHeaderBytes + 4 + OuterFixedLayoutBytes + 8;
    if ( obody[kOuterPresentAt] != 0 )
    {
        std::printf( "outer write: the absent present flag is %d, want 0\n", (int) obody[kOuterPresentAt] );
        return 1;
    }
    for ( int32_t i = 0; i < 17; ++i )
    {
        if ( obody[kOuterTagAt + i] != 0 )
        {
            std::printf( "outer write: absent payload byte %d is %d, want the template's zero\n", (int) i, (int) obody[kOuterTagAt + i] );
            return 1;
        }
    }

    // THE CLEAN UNION PAYLOAD: the template's zeros under an absent flag read as
    // the payload's declared defaults (Inner.value = 7, tag length 0).
    {
        Outer back;
        OuterReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = OuterFixedLoad( &back, 1, ofile, oneed, plan, 256, NULL, &r );
        if ( n != 1 || back.opt_present )
        {
            std::printf( "outer clean: n=%lld present=%d\n", (long long) n, (int) back.opt_present );
            return 1;
        }
        if ( back.opt.u.type != UType::None || back.opt.u.a.value != 7 || back.opt.u.a.tag_length != 0 )
        {
            std::printf( "outer clean: an absent union payload did not read its declared defaults: type=%d value=%d tag_length=%d\n",
                         (int) back.opt.u.type, (int) back.opt.u.a.value, (int) back.opt.u.a.tag_length );
            return 1;
        }
        if ( r.damaged || r.refused )
        {
            std::printf( "outer clean: a clean absent read moved a counter\n" );
            return 1;
        }
    }

    // THE UNION RESIDUE: stain the arm's bytes under the absent flag. Selecting
    // arm a must not let its guarded entries overwrite the defaults that the
    // present-defaults entry lays after them.
    obody[kOuterTagAt] = 2;
    obody[kOuterValueAt + 0] = 100;
    obody[kOuterValueAt + 1] = 0;
    obody[kOuterValueAt + 2] = 0;
    obody[kOuterValueAt + 3] = 0;
    obody[kOuterTagLengthAt + 0] = 4;
    obody[kOuterTagLengthAt + 1] = 0;
    obody[kOuterTagLengthAt + 2] = 0;
    obody[kOuterTagLengthAt + 3] = 0;
    std::memcpy( obody + kOuterTagBufferAt, "rust", 4 );
    {
        Outer back;
        OuterReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = OuterFixedLoad( &back, 1, ofile, oneed, plan, 256, NULL, &r );
        if ( n != 1 || back.opt_present )
        {
            std::printf( "outer residue: n=%lld present=%d\n", (long long) n, (int) back.opt_present );
            return 1;
        }
        if ( back.opt.u.type != UType::None || back.opt.u.a.value != 7 || back.opt.u.a.tag_length != 0 )
        {
            std::printf( "ABSENT UNION PAYLOAD RESIDUE: type=%d value=%d tag_length=%d, want the declared defaults None/7/0\n",
                         (int) back.opt.u.type, (int) back.opt.u.a.value, (int) back.opt.u.a.tag_length );
            return 1;
        }
        if ( r.damaged || r.refused )
        {
            std::printf( "outer residue: a forged union payload under an absent flag is not damage\n" );
            return 1;
        }
    }

    // THE DISCRIMINATING HALF: the same union payload PRESENT lands from the wire.
    {
        Outer p;
        OuterReset( p );
        std::strcpy( p.name, "present" );
        p.name_length = 7;
        p.opt_present = true;
        p.opt.u.SelectA();
        p.opt.u.a.value = 77;
        p.opt.u.a.tag_length = 2;
        std::strcpy( p.opt.u.a.tag, "hi" );
        static uint8_t pfile[4096];
        if ( OuterFixedSave( &p, 1, pfile, (int64_t) sizeof( pfile ) ) != oneed )
        {
            std::printf( "outer present: the record did not save whole\n" );
            return 1;
        }
        Outer back;
        OuterReset( back );
        TableReport r;
        static TableFixedEntry plan[256];
        const int64_t n = OuterFixedLoad( &back, 1, pfile, oneed, plan, 256, NULL, &r );
        if ( n != 1 || !back.opt_present || back.opt.u.type != UType::A || back.opt.u.a.value != 77 ||
             back.opt.u.a.tag_length != 2 || std::memcmp( back.opt.u.a.tag, "hi", 2 ) != 0 )
        {
            std::printf( "outer present: n=%lld present=%d type=%d value=%d tag_length=%d\n", (long long) n,
                         (int) back.opt_present, (int) back.opt.u.type, (int) back.opt.u.a.value, (int) back.opt.u.a.tag_length );
            return 1;
        }
    }

    return 0;
}
`

func cppAbsentPayloadResidueRun(t *testing.T, undermine func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	u := cppUnitFrom(t, cppAbsentPayloadResidueSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if undermine != nil {
		undermine(files)
	}
	dir := t.TempDir()
	var header string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(name, "Table.h"):
			header = name
		case strings.HasSuffix(name, ".cpp"):
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := `#include <cstdio>
#include <cstring>
#include <cstdint>
#include "` + header + `"
using namespace absentresidue;
` + cppAbsentPayloadResidueMain
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func TestCppAbsentPayloadResidueNotCopied(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	out, err := cppAbsentPayloadResidueRun(t, nil)
	if err != nil {
		t.Fatalf("an absent optional copied payload residue: %v\n%s", err, out)
	}
}

func TestCppAbsentPayloadResidueNotCopiedNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	const guard = "if ( flag == 0 && defaults != NULL ) { TableFixedCopyRun( dst + p.aux, defaults + p.aux, p.size ); }"
	undermine := func(files map[string][]byte) {
		found := false
		for name, data := range files {
			if strings.HasSuffix(name, "Table.h") {
				text := string(data)
				if strings.Count(text, guard) != 1 {
					t.Fatalf("%s: the absent-payload defaults copy is not there exactly once", name)
				}
				files[name] = []byte(strings.ReplaceAll(text, guard, "(void) defaults;"))
				found = true
			}
		}
		if !found {
			t.Fatal("no Table.h carried the absent-payload defaults copy")
		}
	}
	out, err := cppAbsentPayloadResidueRun(t, undermine)
	if err == nil {
		t.Fatalf("with the absent-payload defaults copy removed, the probe still passed:\n%s", out)
	}
}

// TestCppAbsentPayloadResidueNotCopiedOrderingNegativeControl plants the
// ORDERING fault in the real artifact, not the absence of the copy: it lifts
// the emitted present-defaults row over the guarded payload rows in the
// identity plan, which is what the old unguarded-first coalescer did. The same
// probe must go red on the union-bearing optional, because the arm entries then
// run after the defaults and answer the wire's residue.
func TestCppAbsentPayloadResidueNotCopiedOrderingNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	undermine := func(files map[string][]byte) {
		found := false
		for name, data := range files {
			if !strings.HasSuffix(name, "Table.h") {
				continue
			}
			lines := strings.Split(string(data), "\n")
			row := -1
			open := -1
			for i, line := range lines {
				if row < 0 && strings.HasPrefix(strings.TrimSpace(line), "{ ") && strings.Contains(line, "kTableFixedPresentDefaults") {
					row = i
				}
			}
			if row < 0 {
				continue
			}
			for i := row - 1; i >= 0; i-- {
				if strings.Contains(lines[i], "FixedPlan[] = {") {
					open = i
					break
				}
			}
			if open < 0 {
				t.Fatalf("%s: the present-defaults row is not inside a fixed plan", name)
			}
			reordered := append([]string{}, lines[:open+1]...)
			reordered = append(reordered, lines[row])
			reordered = append(reordered, lines[open+1:row]...)
			reordered = append(reordered, lines[row+1:]...)
			files[name] = []byte(strings.Join(reordered, "\n"))
			found = true
		}
		if !found {
			t.Fatal("no Table.h carried a present-defaults plan row")
		}
	}
	out, err := cppAbsentPayloadResidueRun(t, undermine)
	if err == nil {
		t.Fatalf("with the present-defaults row lifted over the guarded payload, the probe still passed:\n%s", out)
	}
}
