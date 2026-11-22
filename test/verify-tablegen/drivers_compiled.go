package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func compileObjectsParallel(workDir, compiler string, baseFlags []string, srcs []string) ([]string, error) {
	objDir := filepath.Join(workDir, "objs")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		return nil, err
	}
	objs := make([]string, len(srcs))
	sem := make(chan struct{}, max(1, runtime.NumCPU()))
	var wg sync.WaitGroup
	errs := make(chan error, len(srcs))
	for i, src := range srcs {
		obj := filepath.Join(objDir, fmt.Sprintf("obj_%03d_%s.o", i, filepath.Base(src)))
		objs[i] = obj
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			args := append(append([]string{}, baseFlags...), "-c", src, "-o", obj)
			if out, err := runChecked(workDir, nil, compiler, args...); err != nil {
				errs <- fmt.Errorf("compile %s: %w\n%s", filepath.Base(src), err, out)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return nil, err
	}
	return objs, nil
}

func toSnake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toLowerFirst(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	rs[0] = unicode.ToLower(rs[0])
	return string(rs)
}

func toUpperFirst(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}

// ============================================================================
// 1. C++ (-Wall -Wextra -Werror, including *Table.h, *Block.h, *Cook.h)
// ============================================================================

func verifyCPP(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "cpp", units, genRoot); err != nil {
		return err
	}
	serDir := resolveExistingDir(repoRoot, "SERIALIZE", "../serialize", "build/serialize")

	var inc strings.Builder
	var disp strings.Builder
	var cppFiles []string

	for _, u := range units {
		fmt.Fprintf(&inc, "#include \"%s/%sTable.h\"\n", u.Pkg, u.FileBase)
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Block.h")) {
			fmt.Fprintf(&inc, "#include \"%s/%sBlock.h\"\n", u.Pkg, u.FileBase)
		}
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Cook.h")) {
			fmt.Fprintf(&inc, "#include \"%s/%sCook.h\"\n", u.Pkg, u.FileBase)
		}
		cppFiles = append(cppFiles, filepath.Join(genRoot, u.Pkg, u.FileBase+"Table.cpp"))

		fmt.Fprintf(&disp, `    if ( c.pkg == %q ) {
        if ( c.root == %q ) {
            if ( c.op == 1 ) {
                int64_t need = %s::%sLoadMeasure( c.in.data(), (int64_t)c.in.size(), nullptr, nullptr );
                if ( need < 0 ) die( c.id, "LoadMeasure" );
                std::vector<uint8_t> reg( need > 0 ? (size_t)need : 1 );
                %s::TableReport rep;
                const %s::%s * r = %s::%sLoad( reg.data(), need, c.in.data(), (int64_t)c.in.size(), &rep );
                if ( !r || rep.damaged || rep.refused ) die( c.id, "Load" );
                int64_t sz = %s::%sMeasure( r, %s::TableDefaultAllocator() );
                if ( sz < 0 ) die( c.id, "Measure" );
                std::vector<uint8_t> out( sz > 0 ? (size_t)sz : 1 );
                if ( %s::%sSave( r, out.data(), sz, %s::TableDefaultAllocator() ) != sz ) die( c.id, "Save" );
                check_bytes( c.id, out.data(), (size_t)sz, c.want1 );
                return;
            }
            if ( c.op == 2 ) {
                static std::vector<uint8_t> rbuf( 1 << 20 );
                std::vector<%s::TableRetain::Id> rids( 1 << 17 );
                %s::TableRetain ret;
                ret.bytes = rbuf.data(); ret.capacity = (int64_t)rbuf.size();
                ret.ids = rids.data(); ret.id_capacity = (int32_t)rids.size();
                int64_t need = %s::%sLoadMeasure( c.in.data(), (int64_t)c.in.size(), nullptr, nullptr );
                if ( need < 0 ) die( c.id, "LoadMeasure retain" );
                std::vector<uint8_t> reg( need > 0 ? (size_t)need : 1 );
                %s::TableReport rep;
                const %s::%s * r = %s::%sLoadRetain( reg.data(), need, c.in.data(), (int64_t)c.in.size(), &ret, &rep );
                if ( !r || rep.damaged || rep.refused ) die( c.id, "LoadRetain" );
                int64_t sz = %s::%sMeasureRetain( r, &ret, %s::TableDefaultAllocator() );
                if ( sz < 0 ) die( c.id, "MeasureRetain" );
                std::vector<uint8_t> out( sz > 0 ? (size_t)sz : 1 );
                if ( %s::%sSaveRetain( r, &ret, out.data(), sz, &rep ) != sz ) die( c.id, "SaveRetain" );
                check_bytes( c.id, out.data(), (size_t)sz, c.want1 );
                return;
            }
            if ( c.op == 6 ) {
                %s::%sBuilder b;
                %s::TableReport rep;
                if ( !%s::%sFromJson( b, (const char *)c.in.data(), (int64_t)c.in.size(), &rep ) || rep.damaged || rep.refused ) die( c.id, "FromJson var" );
                if ( !b.Lock() ) die( c.id, "Lock var" );
                int64_t jsz = %s::%sToJsonMeasure( b.AsConst() );
                if ( jsz < 0 ) die( c.id, "ToJsonMeasure var" );
                std::vector<char> jout( jsz > 0 ? (size_t)jsz : 1 );
                if ( %s::%sToJson( b.AsConst(), jout.data(), jsz ) != jsz ) die( c.id, "ToJson var" );
                check_bytes( c.id, (const uint8_t *)jout.data(), (size_t)jsz, c.want1 );
                if ( !c.want2.empty() ) {
                    int64_t wsz = %s::%sMeasure( b.AsConst(), %s::TableDefaultAllocator() );
                    std::vector<uint8_t> wout( wsz > 0 ? (size_t)wsz : 1 );
                    if ( %s::%sSave( b.AsConst(), wout.data(), wsz, %s::TableDefaultAllocator() ) != wsz ) die( c.id, "Save after FromJson" );
                    check_bytes( c.id, wout.data(), (size_t)wsz, c.want2 );
                }
                return;
            }
        }
`,
			u.Pkg, u.VarRoot,
			u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot, u.Pkg,
			u.Pkg, u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot,
			u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.Pkg, u.VarRoot, u.Pkg,
		)

		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			fmt.Fprintf(&disp, `        if ( c.root == %q ) {
            if ( c.op == 4 || c.op == 5 ) {
                %s::%s val;
                std::vector<%s::TableFixedEntry> plan( 4096 );
                %s::TableReport rep;
                int64_t n = %s::%sFixedLoad( &val, 1, c.in.data(), (int64_t)c.in.size(), plan.data(), 4096, nullptr, &rep );
                if ( c.op == 5 ) {
                    if ( n == 1 && !rep.damaged && !rep.refused ) die( c.id, "expected historical writer-bound refusal" );
                    return;
                }
                if ( n != 1 || rep.damaged || rep.refused ) die( c.id, "FixedLoad" );
                int64_t sz = %s::%sFixedMeasure( 1 );
                std::vector<uint8_t> out( sz > 0 ? (size_t)sz : 1 );
                if ( %s::%sFixedSave( &val, 1, out.data(), sz ) != sz ) die( c.id, "FixedSave" );
                check_bytes( c.id, out.data(), (size_t)sz, c.want1 );
                return;
            }
            if ( c.op == 3 ) {
                std::vector<%s::TableMessageEntry> entries( 4096 );
                %s::TableVocabulary vocab( entries.data(), 4096 );
                %s::TableReport arep;
                if ( c.aux.empty() ) {
                    int64_t asz = %s::AnnounceMeasure();
                    std::vector<uint8_t> abuf( (size_t)asz );
                    if ( %s::Announce( abuf.data(), asz ) != asz || !%s::AnnounceRead( vocab, abuf.data(), asz, &arep ) ) die( c.id, "own AnnounceRead" );
                } else {
                    if ( !%s::AnnounceRead( vocab, c.aux.data(), (int64_t)c.aux.size(), &arep ) ) die( c.id, "peer AnnounceRead" );
                }
                std::vector<%s::%s> vals( 16 );
                %s::TableReport lrep;
                int64_t cnt = 16;
                if ( !%s::%sLoadMessages( vals.data(), &cnt, vocab, c.in.data(), (int64_t)c.in.size(), &lrep ) || lrep.damaged || lrep.refused ) die( c.id, "LoadMessages" );
                %s::TableReport srep;
                int64_t msz = %s::%sMeasureMessages( vals.data(), cnt, &srep );
                if ( msz < 0 ) die( c.id, "MeasureMessages" );
                std::vector<uint8_t> out( (size_t)msz );
                if ( %s::%sSaveMessages( vals.data(), cnt, out.data(), msz, &srep ) != msz ) die( c.id, "SaveMessages" );
                check_bytes( c.id, out.data(), (size_t)msz, c.want1 );
                return;
            }
            if ( c.op == 6 || c.op == 7 ) {
                %s::%s val;
                %s::TableReport rep;
                if ( !%s::%sFromJson( val, (const char *)c.in.data(), (int64_t)c.in.size(), &rep ) || rep.damaged || rep.refused ) die( c.id, "FromJson fix" );
                int64_t jsz = %s::%sToJsonMeasure( val );
                if ( jsz < 0 ) die( c.id, "ToJsonMeasure fix" );
                std::vector<char> jout( jsz > 0 ? (size_t)jsz : 1 );
                if ( %s::%sToJson( val, jout.data(), jsz ) != jsz ) die( c.id, "ToJson fix" );
                check_bytes( c.id, (const uint8_t *)jout.data(), (size_t)jsz, c.want1 );
                if ( c.op == 7 ) {
                    %s::TableReport srep;
                    int64_t msz = %s::%sMeasureMessages( &val, 1, &srep );
                    if ( msz < 0 ) die( c.id, "MeasureMessages after FromJson" );
                    std::vector<uint8_t> mout( msz > 0 ? (size_t)msz : 1 );
                    if ( %s::%sSaveMessages( &val, 1, mout.data(), msz, &srep ) != msz ) die( c.id, "SaveMessages after FromJson" );
                    check_bytes( c.id, mout.data(), (size_t)msz, c.want2 );
                }
                return;
            }
        }
`,
				fr,
				u.Pkg, fr, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, fr, u.Pkg, fr,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, u.Pkg, fr, u.Pkg, u.Pkg, fr, u.Pkg, fr,
				u.Pkg, fr, u.Pkg, u.Pkg, fr, u.Pkg, fr, u.Pkg, fr,
				u.Pkg, u.Pkg, fr, u.Pkg, fr,
			)
		}
		disp.WriteString("    }\n")
	}

	cppSrc := fmt.Sprintf(`#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <string>
#include <vector>
%s

struct Case {
    std::string id, pkg, root;
    uint8_t op;
    std::vector<uint8_t> aux, in, want1, want2;
};

static void die( const std::string & id, const char * why ) {
    std::fprintf( stderr, "FAILED: %%s: %%s\n", id.c_str(), why );
    std::exit( 1 );
}

static void check_bytes( const std::string & id, const uint8_t * got, size_t n, const std::vector<uint8_t> & want ) {
    if ( n != want.size() || ( n > 0 && std::memcmp( got, want.data(), n ) != 0 ) ) {
        std::fprintf( stderr, "FAILED: %%s: byte mismatch got=%%zu want=%%zu\n", id.c_str(), n, want.size() );
        std::exit( 1 );
    }
}

static uint32_t ru32( const uint8_t * p ) {
    return (uint32_t)p[0] | ( (uint32_t)p[1] << 8 ) | ( (uint32_t)p[2] << 16 ) | ( (uint32_t)p[3] << 24 );
}
static uint16_t ru16( const uint8_t * p ) {
    return (uint16_t)p[0] | (uint16_t)( (uint16_t)p[1] << 8 );
}

static void run_case( const Case & c ) {
%s
    die( c.id, "unhandled pkg/root/op" );
}

int main( int argc, char ** argv ) {
    if ( argc != 2 ) return 2;
    std::ifstream f( argv[1], std::ios::binary | std::ios::ate );
    if ( !f ) return 2;
    auto sz = (size_t)f.tellg(); f.seekg( 0 );
    std::vector<uint8_t> raw( sz );
    if ( sz > 0 ) f.read( (char *)raw.data(), (std::streamsize)sz );
    size_t at = 0;
    auto rstr = [&]() -> std::string {
        uint16_t n = ru16( raw.data() + at ); at += 2;
        std::string s( (const char *)( raw.data() + at ), n ); at += n;
        return s;
    };
    auto rvec = [&]() -> std::vector<uint8_t> {
        uint32_t n = ru32( raw.data() + at ); at += 4;
        std::vector<uint8_t> v( raw.data() + at, raw.data() + at + n ); at += n;
        return v;
    };
    uint32_t cnt = ru32( raw.data() ); at += 4;
    for ( uint32_t i = 0; i < cnt; ++i ) {
        Case c;
        c.id = rstr(); c.pkg = rstr(); c.root = rstr();
        c.op = raw[at++];
        c.aux = rvec(); c.in = rvec(); c.want1 = rvec(); c.want2 = rvec();
        run_case( c );
    }
    std::printf( "TABLEGEN_OK cpp %%u\n", cnt );
    return 0;
}
`, inc.String(), disp.String())

	mainCPP := filepath.Join(workDir, "main.cpp")
	if err := os.WriteFile(mainCPP, []byte(cppSrc), 0o644); err != nil {
		return err
	}
	binPath := filepath.Join(workDir, "runner")
	cxx := envOr("CXX", "c++")
	compileFlags := []string{"-std=c++17", "-O1", "-ffp-contract=off", "-Wall", "-Wextra", "-Werror", "-I" + serDir, "-I" + genRoot}
	objs, err := compileObjectsParallel(workDir, cxx, compileFlags, append([]string{mainCPP}, cppFiles...))
	if err != nil {
		return fmt.Errorf("compile cpp: %w", err)
	}
	linkArgs := append(append([]string{}, objs...), "-o", binPath)
	if out, err := runChecked(workDir, nil, cxx, linkArgs...); err != nil {
		return fmt.Errorf("link cpp: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, packFile)
	return requireToken(out, err, "cpp", caseCount)
}

// ============================================================================
// 2. C (-std=c99 -Wall -Wextra -Werror, including *Table.h, *Block.h, *Cook.h)
// ============================================================================

func verifyC(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "c", units, genRoot); err != nil {
		return err
	}
	serC := resolveExistingDir(repoRoot, "SERIALIZE_C", "../serialize.c", "build/serialize.c")

	var inc strings.Builder
	var disp strings.Builder
	var cFiles []string

	for _, u := range units {
		fmt.Fprintf(&inc, "#include \"%s/%sTable.h\"\n", u.Pkg, u.FileBase)
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Block.h")) {
			fmt.Fprintf(&inc, "#include \"%s/%sBlock.h\"\n", u.Pkg, u.FileBase)
		}
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Cook.h")) {
			fmt.Fprintf(&inc, "#include \"%s/%sCook.h\"\n", u.Pkg, u.FileBase)
		}
		cFiles = append(cFiles, filepath.Join(genRoot, u.Pkg, u.FileBase+"Table.c"))

		pUp := strings.ToUpper(u.Pkg)
		vSn := toSnake(u.VarRoot)
		fmt.Fprintf(&disp, `    if ( strcmp( c->pkg, %q ) == 0 ) {
        if ( strcmp( c->root, %q ) == 0 ) {
            if ( c->op == 1 ) {
                int64_t need = %s_%s_load_measure( c->in, (int64_t)c->in_len );
                if ( need < 0 ) die( c->id, "load_measure" );
                uint8_t * reg = (uint8_t *)calloc( need > 0 ? (size_t)need : 1, 1 );
                %s_table_report_t rep; memset( &rep, 0, sizeof(rep) );
                const %s_%s_t * r = %s_%s_load( reg, need, c->in, (int64_t)c->in_len, &rep );
                if ( !r || rep.refused ) die( c->id, "load" );
                int64_t sz = %s_%s_measure( NULL, r );
                if ( sz < 0 ) die( c->id, "measure" );
                uint8_t * out = (uint8_t *)calloc( sz > 0 ? (size_t)sz : 1, 1 );
                if ( %s_%s_save( NULL, r, out, sz ) != sz ) die( c->id, "save" );
                check_bytes( c->id, out, (size_t)sz, c->w1, c->w1_len );
                free( out ); free( reg ); return;
            }
            if ( c->op == 2 ) {
                int64_t need = %s_%s_load_measure( c->in, (int64_t)c->in_len );
                if ( need < 0 ) die( c->id, "load_measure retain" );
                uint8_t * reg = (uint8_t *)calloc( need > 0 ? (size_t)need : 1, 1 );
                %s_table_retain_t ret; memset( &ret, 0, sizeof(ret) );
                ret.capacity = 1 << 20; ret.bytes = (uint8_t *)calloc( (size_t)ret.capacity, 1 );
                ret.id_capacity = 1 << 17; ret.ids = (%s_table_retain_id_t *)calloc( (size_t)ret.id_capacity, sizeof(%s_table_retain_id_t) );
                %s_table_report_t rep; memset( &rep, 0, sizeof(rep) );
                const %s_%s_t * r = %s_%s_load_retain( reg, need, c->in, (int64_t)c->in_len, &ret, &rep );
                if ( !r || rep.refused ) die( c->id, "load_retain" );
                int64_t sz = %s_%s_measure_retain( r, &ret );
                if ( sz < 0 ) die( c->id, "measure_retain" );
                uint8_t * out = (uint8_t *)calloc( sz > 0 ? (size_t)sz : 1, 1 );
                if ( %s_%s_save_retain( r, &ret, out, sz, &rep ) != sz ) die( c->id, "save_retain" );
                check_bytes( c->id, out, (size_t)sz, c->w1, c->w1_len );
                free( out ); free( ret.ids ); free( ret.bytes ); free( reg ); return;
            }
            if ( c->op == 6 ) {
                %s_%s_builder_t b; memset( &b, 0, sizeof(b) );
                if ( !%s_%s_builder_init( &b ) ) die( c->id, "builder_init" );
                %s_table_report_t rep; memset( &rep, 0, sizeof(rep) );
                if ( !%s_%s_from_json( &b, (const char *)c->in, (int64_t)c->in_len, &rep ) || rep.refused ) die( c->id, "from_json var" );
                if ( !%s_%s_builder_lock( &b ) ) die( c->id, "builder_lock" );
                const %s_%s_t * r = (const %s_%s_t *)b.region;
                int64_t jsz = %s_%s_to_json_measure( r );
                if ( jsz < 0 ) die( c->id, "to_json_measure var" );
                char * jout = (char *)calloc( (size_t)jsz + 1, 1 );
                if ( %s_%s_to_json( r, jout, jsz + 1 ) != jsz ) die( c->id, "to_json var" );
                check_bytes( c->id, (const uint8_t *)jout, (size_t)jsz, c->w1, c->w1_len );
                if ( c->w2_len > 0 ) {
                    int64_t wsz = %s_%s_measure( NULL, r );
                    uint8_t * wout = (uint8_t *)calloc( (size_t)wsz + 1, 1 );
                    if ( %s_%s_save( NULL, r, wout, wsz ) != wsz ) die( c->id, "save after from_json" );
                    check_bytes( c->id, wout, (size_t)wsz, c->w2, c->w2_len );
                    free( wout );
                }
                free( jout ); %s_%s_builder_shutdown( &b ); return;
            }
        }
`,
			u.Pkg, u.VarRoot,
			u.Pkg, vSn, u.Pkg, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn,
			u.Pkg, vSn, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn,
			u.Pkg, vSn, u.Pkg, vSn, u.Pkg, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn,
		)

		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			fSn := toSnake(fr)
			fmt.Fprintf(&disp, `        if ( strcmp( c->root, %q ) == 0 ) {
            if ( c->op == 4 || c->op == 5 ) {
                %s_%s_t val; memset( &val, 0, sizeof(val) ); %s_%s_reset( &val );
                %s_table_fixed_entry_t * plan = (%s_table_fixed_entry_t *)calloc( 4096, sizeof(%s_table_fixed_entry_t) );
                %s_table_report_t rep; memset( &rep, 0, sizeof(rep) );
                int64_t n = %s_%s_fixed_load( &val, 1, c->in, (int64_t)c->in_len, plan, 4096, NULL, &rep );
                free( plan );
                if ( c->op == 5 ) {
                    if ( n == 1 && !rep.refused ) die( c->id, "expected historical writer-bound refusal" );
                    return;
                }
                if ( n != 1 || rep.refused ) die( c->id, "fixed_load" );
                int64_t sz = %s_%s_fixed_measure( 1 );
                uint8_t * out = (uint8_t *)calloc( (size_t)sz + 1, 1 );
                if ( %s_%s_fixed_save( &val, 1, out, sz ) != sz ) die( c->id, "fixed_save" );
                check_bytes( c->id, out, (size_t)sz, c->w1, c->w1_len );
                free( out ); return;
            }
            if ( c->op == 3 ) {
                %s_table_message_entry_t * entries = (%s_table_message_entry_t *)calloc( 4096, sizeof(%s_table_message_entry_t) );
                %s_table_vocabulary_t vocab = %s_table_vocabulary( entries, 4096 );
                %s_table_report_t arep; memset( &arep, 0, sizeof(arep) );
                if ( c->aux_len == 0 ) {
                    if ( !%s_announce_read( &vocab, %s_K_TABLE_ANNOUNCE, %s_K_TABLE_ANNOUNCE_BYTES, &arep ) ) die( c->id, "own announce_read" );
                } else {
                    if ( !%s_announce_read( &vocab, c->aux, (int64_t)c->aux_len, &arep ) ) die( c->id, "peer announce_read" );
                }
                %s_%s_t * vals = (%s_%s_t *)calloc( 16, sizeof(%s_%s_t) );
                for ( int k = 0; k < 16; ++k ) %s_%s_reset( &vals[k] );
                %s_table_report_t lrep; memset( &lrep, 0, sizeof(lrep) );
                int64_t cnt = 16;
                if ( !%s_%s_load_messages( vals, &cnt, &vocab, c->in, (int64_t)c->in_len, &lrep ) || lrep.refused ) die( c->id, "load_messages" );
                %s_table_report_t srep; memset( &srep, 0, sizeof(srep) );
                int64_t msz = %s_%s_measure_messages( vals, cnt, &srep );
                if ( msz < 0 ) die( c->id, "measure_messages" );
                uint8_t * out = (uint8_t *)calloc( (size_t)msz + 1, 1 );
                if ( %s_%s_save_messages( vals, cnt, out, msz, &srep ) != msz ) die( c->id, "save_messages" );
                check_bytes( c->id, out, (size_t)msz, c->w1, c->w1_len );
                free( out ); free( vals ); free( entries ); return;
            }
            if ( c->op == 6 || c->op == 7 ) {
                %s_%s_t val; memset( &val, 0, sizeof(val) ); %s_%s_reset( &val );
                %s_table_report_t rep; memset( &rep, 0, sizeof(rep) );
                if ( !%s_%s_from_json( &val, (const char *)c->in, (int64_t)c->in_len, &rep ) || rep.refused ) die( c->id, "from_json fix" );
                int64_t jsz = %s_%s_to_json_measure( &val );
                if ( jsz < 0 ) die( c->id, "to_json_measure fix" );
                char * jout = (char *)calloc( (size_t)jsz + 1, 1 );
                if ( %s_%s_to_json( &val, jout, jsz + 1 ) != jsz ) die( c->id, "to_json fix" );
                check_bytes( c->id, (const uint8_t *)jout, (size_t)jsz, c->w1, c->w1_len );
                if ( c->op == 7 ) {
                    %s_table_report_t srep; memset( &srep, 0, sizeof(srep) );
                    int64_t msz = %s_%s_measure_messages( &val, 1, &srep );
                    if ( msz < 0 ) die( c->id, "measure_messages after from_json" );
                    uint8_t * mout = (uint8_t *)calloc( (size_t)msz + 1, 1 );
                    if ( %s_%s_save_messages( &val, 1, mout, msz, &srep ) != msz ) die( c->id, "save_messages after from_json" );
                    check_bytes( c->id, mout, (size_t)msz, c->w2, c->w2_len );
                    free( mout );
                }
                free( jout ); return;
            }
        }
`,
				fr,
				u.Pkg, fSn, u.Pkg, fSn, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, pUp, pUp, u.Pkg, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, u.Pkg, fSn, u.Pkg, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, fSn, u.Pkg, fSn, u.Pkg, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, u.Pkg, fSn, u.Pkg, fSn,
			)
		}
		disp.WriteString("    }\n")
	}

	cSrc := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
%s

typedef struct {
    char id[128], pkg[64], root[64];
    uint8_t op;
    const uint8_t *aux, *in, *w1, *w2;
    size_t aux_len, in_len, w1_len, w2_len;
} case_t;

static void die( const char * id, const char * why ) {
    fprintf( stderr, "FAILED: %%s: %%s\n", id, why );
    exit( 1 );
}

static void check_bytes( const char * id, const uint8_t * got, size_t n, const uint8_t * want, size_t wn ) {
    if ( n != wn || ( n > 0 && memcmp( got, want, n ) != 0 ) ) {
        fprintf( stderr, "FAILED: %%s: byte mismatch got=%%zu want=%%zu\n", id, n, wn );
        exit( 1 );
    }
}

static uint32_t ru32( const uint8_t * p ) {
    return (uint32_t)p[0] | ( (uint32_t)p[1] << 8 ) | ( (uint32_t)p[2] << 16 ) | ( (uint32_t)p[3] << 24 );
}
static uint16_t ru16( const uint8_t * p ) {
    return (uint16_t)p[0] | (uint16_t)( (uint16_t)p[1] << 8 );
}

static void run_case( const case_t * c ) {
%s
    die( c->id, "unhandled pkg/root/op" );
}

int main( int argc, char ** argv ) {
    FILE * f; long sz; uint8_t * raw; size_t at = 0; uint32_t cnt, i;
    if ( argc != 2 ) return 2;
    f = fopen( argv[1], "rb" ); if ( !f ) return 2;
    fseek( f, 0, SEEK_END ); sz = ftell( f ); fseek( f, 0, SEEK_SET );
    raw = (uint8_t *)calloc( (size_t)sz + 1, 1 );
    if ( sz > 0 && fread( raw, 1, (size_t)sz, f ) != (size_t)sz ) return 2;
    fclose( f );
    cnt = ru32( raw ); at += 4;
    for ( i = 0; i < cnt; ++i ) {
        case_t c; uint16_t sl;
        memset( &c, 0, sizeof(c) );
        sl = ru16( raw + at ); at += 2; memcpy( c.id, raw + at, sl ); c.id[sl] = 0; at += sl;
        sl = ru16( raw + at ); at += 2; memcpy( c.pkg, raw + at, sl ); c.pkg[sl] = 0; at += sl;
        sl = ru16( raw + at ); at += 2; memcpy( c.root, raw + at, sl ); c.root[sl] = 0; at += sl;
        c.op = raw[at++];
        c.aux_len = ru32( raw + at ); at += 4; c.aux = raw + at; at += c.aux_len;
        c.in_len = ru32( raw + at ); at += 4; c.in = raw + at; at += c.in_len;
        c.w1_len = ru32( raw + at ); at += 4; c.w1 = raw + at; at += c.w1_len;
        c.w2_len = ru32( raw + at ); at += 4; c.w2 = raw + at; at += c.w2_len;
        run_case( &c );
    }
    printf( "TABLEGEN_OK c %%u\n", cnt );
    free( raw );
    return 0;
}
`, inc.String(), disp.String())

	mainC := filepath.Join(workDir, "main.c")
	if err := os.WriteFile(mainC, []byte(cSrc), 0o644); err != nil {
		return err
	}
	binPath := filepath.Join(workDir, "runner")
	cc := envOr("CC", "cc")
	compileFlags := []string{"-std=c99", "-O1", "-ffp-contract=off", "-Wall", "-Wextra", "-Werror", "-I" + genRoot, "-I" + serC}
	objs, err := compileObjectsParallel(workDir, cc, compileFlags, append([]string{mainC, filepath.Join(serC, "serialize.c")}, cFiles...))
	if err != nil {
		return fmt.Errorf("compile c: %w", err)
	}
	linkArgs := append(append([]string{}, objs...), "-o", binPath, "-lm")
	if out, err := runChecked(workDir, nil, cc, linkArgs...); err != nil {
		return fmt.Errorf("link c: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, packFile)
	return requireToken(out, err, "c", caseCount)
}

// ============================================================================
// 3 & 4. C# and Unity (<TreatWarningsAsErrors>true</TreatWarningsAsErrors>)
// ============================================================================

func buildCSOrUnityProgram(units []UnitSpec, isUnity bool) string {
	langTag := "cs"
	if isUnity {
		langTag = "unity"
	}
	var disp strings.Builder
	for _, u := range units {
		ns := toUpperFirst(u.Pkg)
		if !isUnity {
			fmt.Fprintf(&disp, `        if (c.Pkg == %q)
        {
            if (c.Root == %q)
            {
                if (c.Op == 1)
                {
                    var v = new %s.%s();
                    var rep = new %s.TableReport();
                    if (!%s.Schema.%sLoad(v, c.In, rep) || rep.Damaged || rep.Refused) Die(c.Id, "Load");
                    long sz = %s.Schema.%sMeasure(v);
                    if (sz < 0) Die(c.Id, "Measure");
                    byte[] outBuf = new byte[sz];
                    if (%s.Schema.%sSave(v, outBuf) != sz) Die(c.Id, "Save");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 2)
                {
                    long need = %s.Schema.%sLoadMeasure(c.In, out _, out _, out _);
                    if (need < 0) Die(c.Id, "LoadMeasure retain");
                    IntPtr reg = System.Runtime.InteropServices.Marshal.AllocHGlobal((int)Math.Max(need, 64) + 64);
                    try
                    {
                        IntPtr aligned = (IntPtr)(((long)reg + 15L) & ~15L);
                        byte[] rbuf = new byte[1 << 20];
                        var rids = new %s.TableRetain.Id[1 << 17];
                        var rep = new %s.TableReport();
                        unsafe
                        {
                            fixed (byte* pBytes = rbuf)
                            fixed (%s.TableRetain.Id* pIds = rids)
                            {
                                var ret = new %s.TableRetain { Bytes = pBytes, Capacity = rbuf.Length, Ids = pIds, IdCapacity = rids.Length };
                                if (%s.Schema.%sLoadRetain(aligned, Math.Max(need, 1), c.In, ref ret, rep) == null || rep.Damaged || rep.Refused)
                                    Die(c.Id, "LoadRetain");
                                long sz = %s.Schema.%sMeasureRetain(aligned, ref ret);
                                if (sz < 0) Die(c.Id, "MeasureRetain");
                                byte[] outBuf = new byte[sz];
                                if (%s.Schema.%sSaveRetain(aligned, ref ret, outBuf, rep) != sz) Die(c.Id, "SaveRetain");
                                CheckBytes(c.Id, outBuf, c.Want1);
                                return;
                            }
                        }
                    }
                    finally { System.Runtime.InteropServices.Marshal.FreeHGlobal(reg); }
                }
                if (c.Op == 6)
                {
                    var v = new %s.%s();
                    var rep = new %s.TableReport();
                    if (!%s.Schema.%sFromJson(v, c.In, rep) || rep.Damaged || rep.Refused) Die(c.Id, "FromJson var");
                    long jsz = %s.Schema.%sToJsonMeasure(v);
                    if (jsz < 0) Die(c.Id, "ToJsonMeasure var");
                    byte[] jout = new byte[jsz];
                    if (%s.Schema.%sToJson(v, jout) != jsz) Die(c.Id, "ToJson var");
                    CheckBytes(c.Id, jout, c.Want1);
                    if (c.Want2.Length > 0)
                    {
                        long wsz = %s.Schema.%sMeasure(v);
                        byte[] wout = new byte[wsz];
                        if (%s.Schema.%sSave(v, wout) != wsz) Die(c.Id, "Save after FromJson");
                        CheckBytes(c.Id, wout, c.Want2);
                    }
                    return;
                }
            }
`,
				u.Pkg, u.VarRoot,
				ns, u.VarRoot, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
				ns, u.VarRoot, ns, ns, ns, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
				ns, u.VarRoot, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
			)
			for _, fr := range []string{u.FixRoot, u.BlkRoot} {
				fmt.Fprintf(&disp, `            if (c.Root == %q)
            {
                if (c.Op == 4 || c.Op == 5)
                {
                    var v = new %s.%s();
                    var plan = new %s.TableFixedEntry[4096];
                    var rep = new %s.TableReport();
                    long n = %s.Schema.%sFixedLoad(v, c.In, plan, rep);
                    if (c.Op == 5)
                    {
                        if (n == 1 && !rep.Damaged && !rep.Refused) Die(c.Id, "expected historical writer-bound refusal");
                        return;
                    }
                    if (n != 1 || rep.Damaged || rep.Refused) Die(c.Id, "FixedLoad");
                    long sz = %s.Schema.%sFixedMeasure(1);
                    byte[] outBuf = new byte[sz];
                    if (%s.Schema.%sFixedSave(v, outBuf) != sz) Die(c.Id, "FixedSave");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 3)
                {
                    var vocab = new %s.TableVocabulary();
                    var arep = new %s.TableReport();
                    ReadOnlySpan<byte> ann = c.Aux.Length == 0 ? %s.Schema.Announce() : c.Aux;
                    if ((int)%s.Schema.AnnounceRead(vocab, ann, arep) != 0 || arep.Damaged || arep.Refused) Die(c.Id, "AnnounceRead");
                    var vals = new %s.%s[16];
                    for (int k = 0; k < 16; k++) vals[k] = new %s.%s();
                    var lrep = new %s.TableReport();
                    int cnt = 0;
                    if ((int)%s.Schema.%sLoadMessages(vals, c.In, vocab, lrep, out cnt) != 0 || lrep.Damaged || lrep.Refused) Die(c.Id, "LoadMessages");
                    var slice = vals.AsSpan(0, cnt).ToArray();
                    long msz = %s.Schema.%sMeasureMessages(slice);
                    if (msz < 0) Die(c.Id, "MeasureMessages");
                    byte[] outBuf = new byte[msz];
                    if (%s.Schema.%sSaveMessages(slice, outBuf) != msz) Die(c.Id, "SaveMessages");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 6 || c.Op == 7)
                {
                    var v = new %s.%s();
                    var rep = new %s.TableReport();
                    if (!%s.Schema.%sFromJson(v, c.In, rep) || rep.Damaged || rep.Refused) Die(c.Id, "FromJson fix");
                    long jsz = %s.Schema.%sToJsonMeasure(v);
                    if (jsz < 0) Die(c.Id, "ToJsonMeasure fix");
                    byte[] jout = new byte[jsz];
                    if (%s.Schema.%sToJson(v, jout) != jsz) Die(c.Id, "ToJson fix");
                    CheckBytes(c.Id, jout, c.Want1);
                    if (c.Op == 7)
                    {
                        var one = new[] { v };
                        long msz = %s.Schema.%sMeasureMessages(one);
                        if (msz < 0) Die(c.Id, "MeasureMessages after FromJson");
                        byte[] mout = new byte[msz];
                        if (%s.Schema.%sSaveMessages(one, mout) != msz) Die(c.Id, "SaveMessages after FromJson");
                        CheckBytes(c.Id, mout, c.Want2);
                    }
                    return;
                }
            }
`,
					fr,
					ns, fr, ns, ns, ns, fr, ns, fr, ns, fr,
					ns, ns, ns, ns, ns, fr, ns, fr, ns, ns, fr, ns, fr, ns, fr,
					ns, fr, ns, ns, fr, ns, fr, ns, fr,
					ns, fr, ns, fr,
				)
			}
			disp.WriteString("        }\n")
		} else {
			// Unity dialect
			fmt.Fprintf(&disp, `        if (c.Pkg == %q)
        {
            if (c.Root == %q)
            {
                if (c.Op == 1)
                {
                    var v = new %s.%s();
                    var rep = default(%s.Schema.TableReport);
                    if (!%s.Schema.%sLoad(ref v, c.In, ref rep) || rep.Damaged || rep.Refused) Die(c.Id, "Load");
                    long sz = %s.Schema.%sMeasure(v);
                    if (sz < 0) Die(c.Id, "Measure");
                    byte[] outBuf = new byte[sz];
                    if (%s.Schema.%sSave(v, outBuf) != sz) Die(c.Id, "Save");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 2)
                {
                    var v = new %s.%s();
                    var ret = new %s.Schema.TableRetain(1 << 20, 1 << 17);
                    var rep = default(%s.Schema.TableReport);
                    if (!%s.Schema.%sLoadRetain(ref v, c.In, ret, ref rep) || rep.Damaged || rep.Refused) Die(c.Id, "LoadRetain");
                    long sz = %s.Schema.%sMeasureRetain(v, ret);
                    if (sz < 0) Die(c.Id, "MeasureRetain");
                    byte[] outBuf = new byte[sz];
                    if (%s.Schema.%sSaveRetain(v, ret, outBuf, ref rep) != sz) Die(c.Id, "SaveRetain");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 6)
                {
                    var v = new %s.%s();
                    var rep = default(%s.Schema.TableReport);
                    if (!%s.Schema.%sFromJson(ref v, c.In, ref rep) || rep.Damaged || rep.Refused) Die(c.Id, "FromJson var");
                    long jsz = %s.Schema.%sToJsonMeasure(v);
                    if (jsz < 0) Die(c.Id, "ToJsonMeasure var");
                    byte[] jout = new byte[jsz];
                    if (%s.Schema.%sToJson(v, jout) != jsz) Die(c.Id, "ToJson var");
                    CheckBytes(c.Id, jout, c.Want1);
                    if (c.Want2.Length > 0)
                    {
                        long wsz = %s.Schema.%sMeasure(v);
                        byte[] wout = new byte[wsz];
                        if (%s.Schema.%sSave(v, wout) != wsz) Die(c.Id, "Save after FromJson");
                        CheckBytes(c.Id, wout, c.Want2);
                    }
                    return;
                }
            }
`,
				u.Pkg, u.VarRoot,
				ns, u.VarRoot, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
				ns, u.VarRoot, ns, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
				ns, u.VarRoot, ns, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot, ns, u.VarRoot,
			)
			for _, fr := range []string{u.FixRoot, u.BlkRoot} {
				fmt.Fprintf(&disp, `            if (c.Root == %q)
            {
                if (c.Op == 4 || c.Op == 5)
                {
                    var vals = new %s.%s[1];
                    vals[0] = default(%s.%s);
                    var rep = default(%s.Schema.TableReport);
                    long n = %s.Schema.%sFixedLoad(vals, c.In, ref rep, 4096);
                    if (c.Op == 5)
                    {
                        if (n == 1 && !rep.Damaged && !rep.Refused) Die(c.Id, "expected historical writer-bound refusal");
                        return;
                    }
                    if (n != 1 || rep.Damaged || rep.Refused) Die(c.Id, "FixedLoad");
                    long sz = %s.Schema.%sFixedMeasure(1);
                    byte[] outBuf = new byte[sz];
                    if (%s.Schema.%sFixedSave(vals, outBuf) != sz) Die(c.Id, "FixedSave");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 3)
                {
                    var vocab = new %s.Schema.TableVocabulary();
                    var arep = default(%s.Schema.TableReport);
                    ReadOnlySpan<byte> ann = c.Aux.Length == 0 ? %s.Schema.Announce() : c.Aux;
                    if (!%s.Schema.AnnounceRead(vocab, ann, ref arep) || arep.Damaged || arep.Refused) Die(c.Id, "AnnounceRead");
                    var vals = new %s.%s[16];
                    for (int k = 0; k < 16; k++) vals[k] = default(%s.%s);
                    var lrep = default(%s.Schema.TableReport);
                    int cnt = 0;
                    if (!%s.Schema.%sLoadMessages(vals, c.In, vocab, ref lrep, out cnt) || lrep.Damaged || lrep.Refused) Die(c.Id, "LoadMessages");
                    var slice = vals.AsSpan(0, cnt);
                    long msz = %s.Schema.%sMeasureMessages(slice);
                    if (msz < 0) Die(c.Id, "MeasureMessages");
                    byte[] outBuf = new byte[msz];
                    if (%s.Schema.%sSaveMessages(slice, outBuf) != msz) Die(c.Id, "SaveMessages");
                    CheckBytes(c.Id, outBuf, c.Want1);
                    return;
                }
                if (c.Op == 6 || c.Op == 7)
                {
                    var v = default(%s.%s);
                    var rep = default(%s.Schema.TableReport);
                    if (!%s.Schema.%sFromJson(ref v, c.In, ref rep) || rep.Damaged || rep.Refused) Die(c.Id, "FromJson fix");
                    long jsz = %s.Schema.%sToJsonMeasure(ref v);
                    if (jsz < 0) Die(c.Id, "ToJsonMeasure fix");
                    byte[] jout = new byte[jsz];
                    if (%s.Schema.%sToJson(ref v, jout) != jsz) Die(c.Id, "ToJson fix");
                    CheckBytes(c.Id, jout, c.Want1);
                    if (c.Op == 7)
                    {
                        var one = new[] { v };
                        long msz = %s.Schema.%sMeasureMessages(one.AsSpan());
                        if (msz < 0) Die(c.Id, "MeasureMessages after FromJson");
                        byte[] mout = new byte[msz];
                        if (%s.Schema.%sSaveMessages(one.AsSpan(), mout) != msz) Die(c.Id, "SaveMessages after FromJson");
                        CheckBytes(c.Id, mout, c.Want2);
                    }
                    return;
                }
            }
`,
					fr,
					ns, fr, ns, fr, ns, ns, fr, ns, fr, ns, fr,
					ns, ns, ns, ns, ns, fr, ns, fr, ns, ns, fr, ns, fr, ns, fr,
					ns, fr, ns, ns, fr, ns, fr, ns, fr,
					ns, fr, ns, fr,
				)
			}
			disp.WriteString("        }\n")
		}
	}

	return fmt.Sprintf(`using System;
using System.Buffers.Binary;
using System.IO;
using System.Text;

static class Program
{
    sealed class Case
    {
        public string Id = "", Pkg = "", Root = "";
        public byte Op;
        public byte[] Aux = Array.Empty<byte>(), In = Array.Empty<byte>(), Want1 = Array.Empty<byte>(), Want2 = Array.Empty<byte>();
    }

    static void Die(string id, string why)
    {
        Console.Error.WriteLine("FAILED: " + id + ": " + why);
        Environment.Exit(1);
    }

    static void CheckBytes(string id, byte[] got, byte[] want)
    {
        if (!got.AsSpan().SequenceEqual(want))
            Die(id, "byte mismatch got=" + got.Length + " want=" + want.Length);
    }

    static void RunCase(Case c)
    {
%s
        Die(c.Id, "unhandled pkg/root/op");
    }

    static int Main(string[] args)
    {
        byte[] raw = File.ReadAllBytes(args[0]);
        int at = 0;
        string ReadStr()
        {
            int n = BinaryPrimitives.ReadUInt16LittleEndian(raw.AsSpan(at, 2)); at += 2;
            string s = Encoding.UTF8.GetString(raw, at, n); at += n;
            return s;
        }
        byte[] ReadBytes()
        {
            int n = (int)BinaryPrimitives.ReadUInt32LittleEndian(raw.AsSpan(at, 4)); at += 4;
            byte[] b = raw.AsSpan(at, n).ToArray(); at += n;
            return b;
        }
        uint cnt = BinaryPrimitives.ReadUInt32LittleEndian(raw.AsSpan(0, 4)); at = 4;
        for (uint i = 0; i < cnt; i++)
        {
            var c = new Case { Id = ReadStr(), Pkg = ReadStr(), Root = ReadStr(), Op = raw[at++] };
            c.Aux = ReadBytes(); c.In = ReadBytes(); c.Want1 = ReadBytes(); c.Want2 = ReadBytes();
            RunCase(c);
        }
        Console.WriteLine("TABLEGEN_OK %s " + cnt);
        return 0;
    }
}
`, disp.String(), langTag)
}

func verifyCS(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "cs", units, genRoot); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	csproj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, serCS, serCS)
	if err := os.WriteFile(filepath.Join(workDir, "app.csproj"), []byte(csproj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "Program.cs"), []byte(buildCSOrUnityProgram(units, false)), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(workDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(workDir, "app.csproj"), "--", packFile)
	return requireToken(out, err, "cs", caseCount)
}

func verifyUnity(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "unity", units, genRoot); err != nil {
		return err
	}
	serCS := resolveExistingDir(repoRoot, "SERIALIZE_CS", "../serialize.cs", "build/serialize.cs")
	unityProps := filepath.Join(repoRoot, "test", "unity", "UnityDialect.props")
	hostProps := filepath.Join(repoRoot, "test", "unity", "HostDialect.props")
	libDir := filepath.Join(workDir, "lib")
	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	libProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <SerializeCsRoot>%s</SerializeCsRoot>
  </PropertyGroup>
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>netstandard2.1</TargetFramework>
    <AssemblyName>tablegenunity</AssemblyName>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="../gen/**/*.cs" />
  </ItemGroup>
</Project>
`, serCS, unityProps)
	appProj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <Import Project="%s" />
  <PropertyGroup>
    <TargetFramework>net10.0</TargetFramework>
    <AssemblyName>tablegen-unity-app</AssemblyName>
    <TreatWarningsAsErrors>true</TreatWarningsAsErrors>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="Program.cs" />
    <ProjectReference Include="../lib/lib.csproj" />
  </ItemGroup>
</Project>
`, hostProps)
	if err := os.WriteFile(filepath.Join(libDir, "lib.csproj"), []byte(libProj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.csproj"), []byte(appProj), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "Program.cs"), []byte(buildCSOrUnityProgram(units, true)), 0o644); err != nil {
		return err
	}
	dotnet := envOr("DOTNET", "dotnet")
	out, err := runChecked(appDir, nil, dotnet, "run", "-c", "Release", "--project", filepath.Join(appDir, "app.csproj"), "--", packFile)
	return requireToken(out, err, "unity", caseCount)
}

// ============================================================================
// 5. Go (go vet ./... + full runtime verification)
// ============================================================================

func verifyGo(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "go", units, genRoot); err != nil {
		return err
	}
	serGo := resolveExistingDir(repoRoot, "SERIALIZE_GO", "../serialize.go", "build/serialize.go")
	shimRoot := filepath.Join(workDir, "schemashim")
	if err := os.MkdirAll(filepath.Join(shimRoot, "tables"), 0o755); err != nil {
		return err
	}
	shimMod := fmt.Sprintf("module github.com/mas-bandwidth/schema/v2\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %s\n", serGo)
	if err := os.WriteFile(filepath.Join(shimRoot, "go.mod"), []byte(shimMod), 0o644); err != nil {
		return err
	}
	if err := os.Symlink(filepath.Join(repoRoot, "tables", "goruntime"), filepath.Join(shimRoot, "tables", "goruntime")); err != nil && !os.IsExist(err) {
		return err
	}
	modContent := fmt.Sprintf("module tablegenapp\n\ngo 1.23\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace (\n\tgithub.com/mas-bandwidth/schema/v2 => %s\n\tgithub.com/mas-bandwidth/serialize.go/v2 => %s\n)\n", shimRoot, serGo)
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte(modContent), 0o644); err != nil {
		return err
	}

	var imp strings.Builder
	var disp strings.Builder
	for _, u := range units {
		fmt.Fprintf(&imp, "\t%s \"tablegenapp/gen/%s\"\n", u.Pkg, u.Pkg)
		gv := ir.GoExportName(u.VarRoot)
		fmt.Fprintf(&disp, `	if c.pkg == %q {
		if c.root == %q {
			if c.op == 1 {
				need := %s.%sLoadMeasure(c.in)
				if need < 0 { die(c.id, "LoadMeasure") }
				reg := make([]byte, max(1, int(need)))
				var rep %s.TableReport
				r := %s.%sLoad(reg, c.in, &rep)
				if r == nil || rep.Refused() { die(c.id, "Load") }
				sz := %s.%sMeasure(r)
				if sz < 0 { die(c.id, "Measure") }
				out := make([]byte, sz)
				if %s.%sSave(r, out) != sz { die(c.id, "Save") }
				checkBytes(c.id, out, c.w1)
				return
			}
			if c.op == 2 {
				need := %s.%sLoadMeasure(c.in)
				if need < 0 { die(c.id, "LoadMeasure retain") }
				reg := make([]byte, max(1, int(need)))
				store := %s.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]%s.TableRetainID, 1<<17)}
				var rep %s.TableReport
				r := %s.%sLoadRetain(reg, c.in, &store, &rep)
				if r == nil || rep.Refused() { die(c.id, "LoadRetain") }
				sz := %s.%sMeasureRetain(r, &store)
				if sz < 0 { die(c.id, "MeasureRetain") }
				out := make([]byte, sz)
				if %s.%sSaveRetain(r, &store, out, &rep) != sz { die(c.id, "SaveRetain") }
				checkBytes(c.id, out, c.w1)
				return
			}
			if c.op == 6 {
				var b %s.%sBuilder
				if !b.Init() { die(c.id, "Init") }
				defer b.Shutdown()
				var rep %s.TableReport
				if !%s.%sFromJSON(&b, c.in, &rep) || rep.Refused() { die(c.id, "FromJSON var") }
				if !b.Lock() { die(c.id, "Lock") }
				r := b.AsConst()
				jsz := %s.%sToJSONMeasure(r)
				if jsz < 0 { die(c.id, "ToJSONMeasure var") }
				jout := make([]byte, jsz)
				if %s.%sToJSON(r, jout) != jsz { die(c.id, "ToJSON var") }
				checkBytes(c.id, jout, c.w1)
				if len(c.w2) > 0 {
					wsz := %s.%sMeasure(r)
					wout := make([]byte, wsz)
					if %s.%sSave(r, wout) != wsz { die(c.id, "Save after FromJSON") }
					checkBytes(c.id, wout, c.w2)
				}
				return
			}
		}
`,
			u.Pkg, u.VarRoot,
			u.Pkg, gv, u.Pkg, u.Pkg, gv, u.Pkg, gv, u.Pkg, gv,
			u.Pkg, gv, u.Pkg, u.Pkg, u.Pkg, u.Pkg, gv, u.Pkg, gv, u.Pkg, gv,
			u.Pkg, gv, u.Pkg, u.Pkg, gv, u.Pkg, gv, u.Pkg, gv, u.Pkg, gv, u.Pkg, gv,
		)
		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			gf := ir.GoExportName(fr)
			fmt.Fprintf(&disp, `		if c.root == %q {
			if c.op == 4 || c.op == 5 {
				vals := make([]%s.%s, 1)
				%s.%sReset(&vals[0])
				plan := make([]%s.TableFixedEntry, 4096)
				var rep %s.TableReport
				n := %s.%sFixedLoad(vals, c.in, plan, &rep)
				if c.op == 5 {
					if n == 1 && !rep.Refused() { die(c.id, "expected historical writer-bound refusal") }
					return
				}
				if n != 1 || rep.Refused() { die(c.id, "FixedLoad") }
				sz := %s.%sFixedMeasure(1)
				out := make([]byte, sz)
				if %s.%sFixedSave(vals, out) != sz { die(c.id, "FixedSave") }
				checkBytes(c.id, out, c.w1)
				return
			}
			if c.op == 3 {
				var vocab %s.TableVocabulary
				vocab.Init(make([]%s.TableMessageEntry, 4096))
				var arep %s.TableReport
				if len(c.aux) == 0 {
					abuf := make([]byte, %s.AnnounceMeasure())
					%s.Announce(abuf)
					if !%s.AnnounceRead(&vocab, abuf, &arep) || arep.Refused() { die(c.id, "own AnnounceRead") }
				} else {
					if !%s.AnnounceRead(&vocab, c.aux, &arep) || arep.Refused() { die(c.id, "peer AnnounceRead") }
				}
				vals := make([]%s.%s, 16)
				for k := range vals { %s.%sReset(&vals[k]) }
				var lrep %s.TableReport
				cnt, ok := %s.%sLoadMessages(vals, &vocab, c.in, &lrep)
				if !ok || cnt < 1 || lrep.Refused() { die(c.id, "LoadMessages") }
				var srep %s.TableReport
				ptrs := ptrSlice(vals[:cnt])
				msz := %s.%sMeasureMessages(ptrs, &srep)
				if msz < 0 { die(c.id, "MeasureMessages") }
				out := make([]byte, msz)
				if %s.%sSaveMessages(ptrs, out, &srep) != msz { die(c.id, "SaveMessages") }
				checkBytes(c.id, out, c.w1)
				return
			}
			if c.op == 6 || c.op == 7 {
				var val %s.%s
				%s.%sReset(&val)
				var rep %s.TableReport
				if !%s.%sFromJSON(&val, c.in, &rep) || rep.Refused() { die(c.id, "FromJSON fix") }
				jsz := %s.%sToJSONMeasure(&val)
				if jsz < 0 { die(c.id, "ToJSONMeasure fix") }
				jout := make([]byte, jsz)
				if %s.%sToJSON(&val, jout) != jsz { die(c.id, "ToJSON fix") }
				checkBytes(c.id, jout, c.w1)
				if c.op == 7 {
					var srep %s.TableReport
					ptrs := ptrSlice([]%s.%s{val})
					msz := %s.%sMeasureMessages(ptrs, &srep)
					if msz < 0 { die(c.id, "MeasureMessages after FromJSON") }
					mout := make([]byte, msz)
					if %s.%sSaveMessages(ptrs, mout, &srep) != msz { die(c.id, "SaveMessages after FromJSON") }
					checkBytes(c.id, mout, c.w2)
				}
				return
			}
		}
`,
				fr,
				u.Pkg, gf, u.Pkg, gf, u.Pkg, u.Pkg, u.Pkg, gf, u.Pkg, gf, u.Pkg, gf,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, gf, u.Pkg, gf, u.Pkg, u.Pkg, gf, u.Pkg, u.Pkg, gf, u.Pkg, gf,
				u.Pkg, gf, u.Pkg, gf, u.Pkg, u.Pkg, gf, u.Pkg, gf, u.Pkg, gf,
				u.Pkg, u.Pkg, gf, u.Pkg, gf, u.Pkg, gf,
			)
		}
		disp.WriteString("\t}\n")
	}

	goSrc := fmt.Sprintf(`package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

%s)

type packCase struct {
	id, pkg, root string
	op            uint8
	aux, in       []byte
	w1, w2        []byte
}

func die(id, why string) {
	fmt.Fprintf(os.Stderr, "FAILED: %%s: %%s\n", id, why)
	os.Exit(1)
}

func ptrSlice[T any](s []T) []*T {
	out := make([]*T, len(s))
	for i := range s {
		out[i] = &s[i]
	}
	return out
}

func checkBytes(id string, got, want []byte) {
	if !bytes.Equal(got, want) {
		fmt.Fprintf(os.Stderr, "FAILED: %%s: byte mismatch got=%%d want=%%d\n", id, len(got), len(want))
		os.Exit(1)
	}
}

func runCase(c packCase) {
%s	die(c.id, "unhandled pkg/root/op")
}

func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil { panic(err) }
	at := 0
	rstr := func() string {
		n := int(binary.LittleEndian.Uint16(raw[at:]))
		at += 2
		s := string(raw[at : at+n])
		at += n
		return s
	}
	rbytes := func() []byte {
		n := int(binary.LittleEndian.Uint32(raw[at:]))
		at += 4
		b := raw[at : at+n]
		at += n
		return b
	}
	cnt := binary.LittleEndian.Uint32(raw[:4])
	at = 4
	for range cnt {
		var c packCase
		c.id = rstr()
		c.pkg = rstr()
		c.root = rstr()
		c.op = raw[at]
		at++
		c.aux = rbytes()
		c.in = rbytes()
		c.w1 = rbytes()
		c.w2 = rbytes()
		runCase(c)
	}
	fmt.Printf("TABLEGEN_OK go %%d\n", cnt)
}
`, imp.String(), disp.String())

	if err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte(goSrc), 0o644); err != nil {
		return err
	}
	if out, err := runChecked(workDir, nil, "go", "vet", "./..."); err != nil {
		return fmt.Errorf("go vet: %w\n%s", err, out)
	}
	binPath := filepath.Join(workDir, "runner")
	if out, err := runChecked(workDir, nil, "go", "build", "-o", binPath, "."); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, binPath, packFile)
	return requireToken(out, err, "go", caseCount)
}

// ============================================================================
// 6. Rust (RUSTFLAGS="-D warnings", features = ["cook", "block"])
// ============================================================================

func verifyRust(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	serRS := resolveExistingDir(repoRoot, "SERIALIZE_RS", "../serialize.rs", "build/serialize.rs")

	// Generate each unit into gen/<pkg>/src
	srcUnits := make([]UnitSpec, len(units))
	copy(srcUnits, units)
	if err := os.MkdirAll(genRoot, 0o755); err != nil {
		return err
	}
	for _, u := range units {
		uSrc := filepath.Join(genRoot, u.Pkg, "src")
		if err := os.MkdirAll(uSrc, 0o755); err != nil {
			return err
		}
		if out, err := runChecked(repoRoot, nil, schemaBin, "generate", "--lang", "rust", "--out", uSrc, u.SchemaDir); err != nil {
			return fmt.Errorf("generate rust (%s): %w\n%s", u.Pkg, err, out)
		}
		uToml := fmt.Sprintf("[package]\nname = %q\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[features]\ncook = []\nblock = []\n\n[dependencies]\nserialize = { package = \"serialize-official\", path = %q }\n", u.Pkg, serRS)
		if err := os.WriteFile(filepath.Join(genRoot, u.Pkg, "Cargo.toml"), []byte(uToml), 0o644); err != nil {
			return err
		}
	}

	appDir := filepath.Join(workDir, "app")
	if err := os.MkdirAll(filepath.Join(appDir, "src"), 0o755); err != nil {
		return err
	}
	var deps strings.Builder
	var disp strings.Builder
	for _, u := range units {
		fmt.Fprintf(&deps, "%s = { path = \"../gen/%s\", features = [\"cook\", \"block\"] }\n", u.Pkg, u.Pkg)
		vSn := toSnake(u.VarRoot)
		fmt.Fprintf(&disp, `    if c.pkg == %q {
        if c.root == %q {
            if c.op == 1 {
                let mut rep = %s::TableReport::default();
                let r = %s::%s_load(&c.inp, &mut rep).unwrap_or_else(|| die(&c.id, "load"));
                if rep.refused { die(&c.id, "load refused"); }
                let out = %s::%s_save_checked(&r).unwrap_or_else(|_| die(&c.id, "save"));
                check_bytes(&c.id, &out, &c.w1);
                return;
            }
            if c.op == 2 {
                let mut ret = %s::TableRetain {
                    bytes: Vec::with_capacity(1 << 20),
                    ids: Vec::with_capacity(1 << 17),
                    nodes: std::collections::HashMap::new(),
                };
                let mut rep = %s::TableReport::default();
                let r = %s::%s_load_retain(&c.inp, &mut ret, &mut rep).unwrap_or_else(|| die(&c.id, "load_retain"));
                if rep.refused { die(&c.id, "load_retain refused"); }
                let out = %s::%s_save_retain(&r, &mut ret, &mut rep);
                if rep.refused { die(&c.id, "save_retain refused"); }
                check_bytes(&c.id, &out, &c.w1);
                return;
            }
            if c.op == 6 {
                let mut rep = %s::TableReport::default();
                let r = %s::%s_from_json_bytes(&c.inp, &mut rep).unwrap_or_else(|| die(&c.id, "from_json var"));
                if rep.refused { die(&c.id, "from_json var refused"); }
                let jout = %s::%s_to_json_checked(&r).unwrap_or_else(|| die(&c.id, "to_json var"));
                check_bytes(&c.id, jout.as_bytes(), &c.w1);
                if !c.w2.is_empty() {
                    let wout = %s::%s_save_checked(&r).unwrap_or_else(|_| die(&c.id, "save after from_json"));
                    check_bytes(&c.id, &wout, &c.w2);
                }
                return;
            }
        }
`,
			u.Pkg, u.VarRoot,
			u.Pkg, u.Pkg, vSn, u.Pkg, vSn,
			u.Pkg, u.Pkg, u.Pkg, vSn, u.Pkg, vSn,
			u.Pkg, u.Pkg, vSn, u.Pkg, vSn, u.Pkg, vSn,
		)
		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			fSn := toSnake(fr)
			fmt.Fprintf(&disp, `        if c.root == %q {
            if c.op == 4 || c.op == 5 {
                let mut vals = vec![%s::%sRow::default(); 1];
                let mut plan = vec![%s::TableFixedEntry::default(); 4096];
                let mut remap = vec![0u16; 4096];
                let mut rep = %s::TableFixedReport::default();
                let loaded = %s::%s_fixed_load(&mut vals, &c.inp, &mut plan, &mut remap, &mut rep);
                if c.op == 5 {
                    if loaded == Some(1) && !rep.damaged && !rep.refused { die(&c.id, "expected historical writer-bound refusal"); }
                    return;
                }
                if loaded != Some(1) || rep.damaged || rep.refused { die(&c.id, "fixed_load"); }
                let sz = %s::%s_fixed_measure(1);
                let mut out = vec![0u8; sz];
                if %s::%s_fixed_save(&vals, &mut out) != Some(sz) { die(&c.id, "fixed_save"); }
                check_bytes(&c.id, &out, &c.w1);
                return;
            }
            if c.op == 3 {
                let mut entries = vec![%s::TableMessageEntry::default(); 4096];
                let mut vocab = %s::TableVocabulary::new(&mut entries);
                let mut arep = %s::TableReport::default();
                if c.aux.is_empty() {
                    let mut abuf = vec![0u8; %s::announce_measure()];
                    %s::announce(&mut abuf);
                    if !%s::announce_read(&mut vocab, &abuf, &mut arep) || arep.refused { die(&c.id, "own announce_read"); }
                } else {
                    if !%s::announce_read(&mut vocab, &c.aux, &mut arep) || arep.refused { die(&c.id, "peer announce_read"); }
                }
                let mut vals = vec![%s::%sRow::default(); 16];
                let mut lrep = %s::TableReport::default();
                let (cnt, ok) = %s::%s_load_messages(&mut vals, &vocab, &c.inp, &mut lrep);
                if !ok || cnt < 1 || lrep.refused { die(&c.id, "load_messages"); }
                let mut srep = %s::TableReport::default();
                let msz = %s::%s_measure_messages(&vals[..cnt], &mut srep);
                if msz < 0 { die(&c.id, "measure_messages"); }
                let mut out = vec![0u8; msz as usize];
                if %s::%s_save_messages(&vals[..cnt], &mut out, &mut srep) != msz { die(&c.id, "save_messages"); }
                check_bytes(&c.id, &out, &c.w1);
                return;
            }
            if c.op == 6 || c.op == 7 {
                let mut rep = %s::TableReport::default();
                let r = %s::%s_from_json_bytes(&c.inp, &mut rep).unwrap_or_else(|| die(&c.id, "from_json fix"));
                if rep.refused { die(&c.id, "from_json fix refused"); }
                let jout = %s::%s_to_json_checked(&r).unwrap_or_else(|| die(&c.id, "to_json fix"));
                check_bytes(&c.id, jout.as_bytes(), &c.w1);
                if c.op == 7 {
                    let mut srep = %s::TableReport::default();
                    let row = %s::%sRow::from(&r);
                    let one = std::slice::from_ref(&row);
                    let msz = %s::%s_measure_messages(one, &mut srep);
                    if msz < 0 { die(&c.id, "measure_messages after from_json"); }
                    let mut mout = vec![0u8; msz as usize];
                    if %s::%s_save_messages(one, &mut mout, &mut srep) != msz { die(&c.id, "save_messages after from_json"); }
                    check_bytes(&c.id, &mout, &c.w2);
                }
                return;
            }
        }
`,
				fr,
				u.Pkg, fr, u.Pkg, u.Pkg, u.Pkg, fSn, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, u.Pkg, fSn, u.Pkg, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, u.Pkg, fSn, u.Pkg, fSn,
				u.Pkg, u.Pkg, fr, u.Pkg, fSn, u.Pkg, fSn,
			)
		}
		disp.WriteString("    }\n")
	}

	appToml := fmt.Sprintf("[package]\nname = \"tablegenapp\"\nversion = \"0.0.0\"\nedition = \"2024\"\n\n[dependencies]\n%s", deps.String())
	if err := os.WriteFile(filepath.Join(appDir, "Cargo.toml"), []byte(appToml), 0o644); err != nil {
		return err
	}
	rsSrc := fmt.Sprintf(`use std::fs;

struct Case {
    id: String, pkg: String, root: String, op: u8,
    aux: Vec<u8>, inp: Vec<u8>, w1: Vec<u8>, w2: Vec<u8>,
}

fn die(id: &str, why: &str) -> ! {
    eprintln!("FAILED: {}: {}", id, why);
    std::process::exit(1);
}

fn check_bytes(id: &str, got: &[u8], want: &[u8]) {
    if got != want {
        eprintln!("FAILED: {}: byte mismatch got={} want={}", id, got.len(), want.len());
        std::process::exit(1);
    }
}

fn run_case(c: &Case) {
%s    die(&c.id, "unhandled pkg/root/op");
}

fn rstr(raw: &[u8], at: &mut usize) -> String {
    let n = u16::from_le_bytes([raw[*at], raw[*at + 1]]) as usize;
    *at += 2;
    let s = std::str::from_utf8(&raw[*at..*at + n]).unwrap().to_string();
    *at += n;
    s
}

fn rvec(raw: &[u8], at: &mut usize) -> Vec<u8> {
    let n = u32::from_le_bytes([raw[*at], raw[*at + 1], raw[*at + 2], raw[*at + 3]]) as usize;
    *at += 4;
    let v = raw[*at..*at + n].to_vec();
    *at += n;
    v
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let raw = fs::read(&args[1]).unwrap();
    let cnt = u32::from_le_bytes([raw[0], raw[1], raw[2], raw[3]]);
    let mut at = 4usize;
    for _ in 0..cnt {
        let id = rstr(&raw, &mut at);
        let pkg = rstr(&raw, &mut at);
        let root = rstr(&raw, &mut at);
        let op = raw[at];
        at += 1;
        let aux = rvec(&raw, &mut at);
        let inp = rvec(&raw, &mut at);
        let w1 = rvec(&raw, &mut at);
        let w2 = rvec(&raw, &mut at);
        run_case(&Case { id, pkg, root, op, aux, inp, w1, w2 });
    }
    println!("TABLEGEN_OK rust {}", cnt);
}
`, disp.String())
	if err := os.WriteFile(filepath.Join(appDir, "src", "main.rs"), []byte(rsSrc), 0o644); err != nil {
		return err
	}
	extraEnv := []string{"RUSTFLAGS=-D warnings"}
	if rb := os.Getenv("RUSTUP_BIN"); rb != "" {
		extraEnv = append(extraEnv, "PATH="+rb+":"+os.Getenv("PATH"))
	}
	out, err := runChecked(appDir, extraEnv, "cargo", "run", "--quiet", "--release", "--", packFile)
	return requireToken(out, err, "rust", caseCount)
}

// ============================================================================
// 7. Java (javac --release 17 -Xlint:all -Werror)
// ============================================================================

func verifyJava(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "java", units, genRoot); err != nil {
		return err
	}
	var disp strings.Builder
	var javaFiles []string
	for _, u := range units {
		entries, _ := filepath.Glob(filepath.Join(genRoot, u.Pkg, "*.java"))
		javaFiles = append(javaFiles, entries...)
		fmt.Fprintf(&disp, `        if (c.pkg.equals(%q)) {
            if (c.root.equals(%q)) {
                if (c.op == 1) {
                    var rep = new %s.TableVariable.Report();
                    var v = %s.%sVariable.load(c.in, rep);
                    if (v == null || rep.damaged || rep.refused) die(c.id, "load");
                    byte[] out = %s.%sVariable.save(v);
                    if (out == null) die(c.id, "save");
                    checkBytes(c.id, out, c.w1);
                    return;
                }
                if (c.op == 2) {
                    var store = new %s.TableRetain(1 << 20, 1 << 17);
                    var rep = new %s.TableVariable.Report();
                    var v = %s.%sRetain.loadRetain(c.in, store, rep);
                    if (v == null || rep.damaged || rep.refused) die(c.id, "loadRetain");
                    byte[] out = %s.%sRetain.saveRetain(v, store, rep);
                    if (out == null || rep.damaged || rep.refused) die(c.id, "saveRetain");
                    checkBytes(c.id, out, c.w1);
                    return;
                }
                if (c.op == 6) {
                    var rep = new %s.TableVariable.Report();
                    var v = %s.%sVariable.fromJson(c.in, rep);
                    if (v == null || rep.damaged || rep.refused) die(c.id, "fromJson var");
                    byte[] jout = %s.%sVariable.toJson(v);
                    if (jout == null) die(c.id, "toJson var");
                    checkBytes(c.id, jout, c.w1);
                    if (c.w2.length > 0) {
                        byte[] wout = %s.%sVariable.save(v);
                        if (wout == null) die(c.id, "save after fromJson");
                        checkBytes(c.id, wout, c.w2);
                    }
                    return;
                }
            }
`,
			u.Pkg, u.VarRoot,
			u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot,
			u.Pkg, u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot,
			u.Pkg, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot, u.Pkg, u.VarRoot,
		)
		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			fmt.Fprintf(&disp, `            if (c.root.equals(%q)) {
                if (c.op == 4 || c.op == 5) {
                    var vals = new %s.%sFixed.Value[] { new %s.%sFixed.Value() };
                    var plan = new %s.TableFixed.Entry[4096];
                    for (int k = 0; k < 4096; k++) plan[k] = new %s.TableFixed.Entry();
                    var rep = new %s.TableFixed.Report();
                    int n = %s.%sFixed.load(vals, 1, c.in, plan, new short[4096], new byte[65536], rep);
                    if (c.op == 5) {
                        if (n == 1 && !rep.damaged && !rep.refused) die(c.id, "expected historical writer-bound refusal");
                        return;
                    }
                    if (n != 1 || rep.damaged || rep.refused) die(c.id, "Fixed.load");
                    int sz = %s.%sFixed.measure(1);
                    byte[] out = new byte[sz];
                    if (%s.%sFixed.save(vals, 1, out) != sz) die(c.id, "Fixed.save");
                    checkBytes(c.id, out, c.w1);
                    return;
                }
                if (c.op == 3) {
                    var vocab = new %s.TableVocabulary();
                    vocab.reset(4096);
                    var arep = new %s.TableVocabulary.Report();
                    if (c.aux.length == 0) {
                        byte[] abuf = new byte[%s.TableVocabulary.announceMeasure()];
                        %s.TableVocabulary.announce(abuf);
                        if (!%s.TableVocabulary.announceRead(vocab, abuf, arep) || arep.damaged || arep.refused) die(c.id, "own announceRead");
                    } else {
                        if (!%s.TableVocabulary.announceRead(vocab, c.aux, arep) || arep.damaged || arep.refused) die(c.id, "peer announceRead");
                    }
                    var vals = new %s.%sFixed.Value[16];
                    for (int k = 0; k < 16; k++) vals[k] = new %s.%sFixed.Value();
                    var lrep = new %s.TableVocabulary.Report();
                    int cnt = %s.%sMessage.loadMessages(vals, 16, vocab, c.in, lrep);
                    if (cnt < 1 || lrep.damaged || lrep.refused) die(c.id, "loadMessages");
                    var srep = new %s.TableVocabulary.Report();
                    int msz = %s.%sMessage.measureMessages(vals, cnt, srep);
                    if (msz < 1 || srep.refused) die(c.id, "measureMessages");
                    byte[] out = new byte[msz];
                    if (%s.%sMessage.saveMessages(vals, cnt, out, srep) != msz) die(c.id, "saveMessages");
                    checkBytes(c.id, out, c.w1);
                    return;
                }
                // op 7's message half has no Java counterpart: the text codec
                // lives on the variable form's Value (lists), the message
                // writer takes the fixed form's Value (arrays), and the
                // generated code has no conversion between them, so there is
                // no record the text lands in that the writer can take. The
                // text half is checked as the other targets check it.
                if (c.op == 6 || c.op == 7) {
                    var rep = new %s.TableVariable.Report();
                    var v = %s.%sVariable.fromJson(c.in, rep);
                    if (v == null || rep.damaged || rep.refused) die(c.id, "fromJson fix");
                    byte[] jout = %s.%sVariable.toJson(v);
                    if (jout == null) die(c.id, "toJson fix");
                    checkBytes(c.id, jout, c.w1);
                    return;
                }
            }
`,
				fr,
				u.Pkg, fr, u.Pkg, fr, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, fr, u.Pkg, fr,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, fr, u.Pkg, u.Pkg, fr, u.Pkg, u.Pkg, fr, u.Pkg, fr,
				u.Pkg, u.Pkg, fr, u.Pkg, fr,
			)
		}
		disp.WriteString("        }\n")
	}

	javaSrc := fmt.Sprintf(`import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Main {
    static final class Case {
        String id, pkg, root;
        int op;
        byte[] aux, in, w1, w2;
    }

    static void die(String id, String why) {
        System.err.println("FAILED: " + id + ": " + why);
        System.exit(1);
    }

    static void checkBytes(String id, byte[] got, byte[] want) {
        if (!Arrays.equals(got, want)) {
            die(id, "byte mismatch got=" + got.length + " want=" + want.length);
        }
    }

    static void runCase(Case c) {
%s        die(c.id, "unhandled pkg/root/op");
    }

    static String readStr(ByteBuffer bb) {
        int n = Short.toUnsignedInt(bb.getShort());
        byte[] b = new byte[n];
        bb.get(b);
        return new String(b, StandardCharsets.UTF_8);
    }

    static byte[] readBytes(ByteBuffer bb) {
        int n = bb.getInt();
        byte[] b = new byte[n];
        bb.get(b);
        return b;
    }

    public static void main(String[] args) throws Exception {
        byte[] raw = Files.readAllBytes(Path.of(args[0]));
        ByteBuffer bb = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN);
        int cnt = bb.getInt();
        for (int i = 0; i < cnt; i++) {
            Case c = new Case();
            c.id = readStr(bb); c.pkg = readStr(bb); c.root = readStr(bb);
            c.op = Byte.toUnsignedInt(bb.get());
            c.aux = readBytes(bb); c.in = readBytes(bb); c.w1 = readBytes(bb); c.w2 = readBytes(bb);
            runCase(c);
        }
        System.out.println("TABLEGEN_OK java " + cnt);
    }
}
`, disp.String())

	mainJava := filepath.Join(workDir, "Main.java")
	if err := os.WriteFile(mainJava, []byte(javaSrc), 0o644); err != nil {
		return err
	}
	classesDir := filepath.Join(workDir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return err
	}
	javac := resolveTool(repoRoot, "JAVAC", "dist/jdk-21.0.12.1/Contents/Home/bin/javac", "javac")
	javaBin := resolveTool(repoRoot, "JAVA", "dist/jdk-21.0.12.1/Contents/Home/bin/java", "java")
	args := append([]string{"--release", "17", "-Xlint:all", "-Werror", "-d", classesDir, mainJava}, javaFiles...)
	if out, err := runChecked(workDir, nil, javac, args...); err != nil {
		return fmt.Errorf("javac: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, javaBin, "-ea", "-cp", classesDir, "Main", packFile)
	return requireToken(out, err, "java", caseCount)
}

// ============================================================================
// 8. Dart (dart analyze --fatal-infos --fatal-warnings + dart --enable-asserts)
// ============================================================================

func verifyDart(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "dart", units, genRoot); err != nil {
		return err
	}
	pubspec := "name: tablegendart\nenvironment:\n  sdk: '>=3.0.0 <4.0.0'\n"
	if err := os.WriteFile(filepath.Join(workDir, "pubspec.yaml"), []byte(pubspec), 0o644); err != nil {
		return err
	}

	var imp strings.Builder
	var disp strings.Builder
	for _, u := range units {
		upPkg := toUpperFirst(u.Pkg)
		fmt.Fprintf(&imp, "import './gen/%s/%sWire.dart' as %s_w;\n", u.Pkg, upPkg, u.Pkg)
		fmt.Fprintf(&imp, "import './gen/%s/%sFixed.dart' as %s_f;\n", u.Pkg, u.FileBase, u.Pkg)
		fmt.Fprintf(&imp, "import './gen/%s/%sFixed.dart' as %s_fr;\n", u.Pkg, upPkg, u.Pkg)
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Block.dart")) {
			fmt.Fprintf(&imp, "// ignore: unused_import\nimport './gen/%s/%sBlock.dart' as %s_b;\n", u.Pkg, u.FileBase, u.Pkg)
		}
		if fileExists(filepath.Join(genRoot, u.Pkg, u.FileBase+"Cook.dart")) {
			fmt.Fprintf(&imp, "// ignore: unused_import\nimport './gen/%s/%sCook.dart' as %s_c;\n", u.Pkg, u.FileBase, u.Pkg)
		}

		vLc := toLowerFirst(u.VarRoot)
		fmt.Fprintf(&disp, `  if (c.pkg == %q) {
    if (c.root == %q) {
      if (c.op == 1) {
        final rep = %s_w.TableWireReport();
        final v = %s_w.%sWireLoad(c.inp, rep);
        if (rep.refused) die(c.id, 'WireLoad');
        final out = %s_w.%sWireSave(v);
        checkBytes(c.id, out, c.w1);
        return;
      }
      if (c.op == 2) {
        final ret = %s_w.TableRetain(1 << 20, List<int>.filled(1 << 17, 0));
        final rep = %s_w.TableWireReport();
        final v = %s_w.%sWireLoadRetain(c.inp, ret, rep);
        if (rep.refused) die(c.id, 'WireLoadRetain');
        final out = %s_w.%sWireSaveRetain(v, ret, rep);
        if (rep.refused) die(c.id, 'WireSaveRetain');
        checkBytes(c.id, out, c.w1);
        return;
      }
      if (c.op == 6) {
        final rep = %s_w.TableTextReport();
        final v = %s_w.%sFromJson(utf8.decode(c.inp), rep);
        if (rep.refused) die(c.id, 'FromJson var');
        final jout = Uint8List.fromList(utf8.encode(%s_w.%sToJson(v)));
        checkBytes(c.id, jout, c.w1);
        if (c.w2.isNotEmpty) {
          final wout = %s_w.%sWireSave(v);
          checkBytes(c.id, wout, c.w2);
        }
        return;
      }
    }
`,
			u.Pkg, u.VarRoot,
			u.Pkg, u.Pkg, vLc, u.Pkg, vLc,
			u.Pkg, u.Pkg, u.Pkg, vLc, u.Pkg, vLc,
			u.Pkg, u.Pkg, vLc, u.Pkg, vLc, u.Pkg, vLc,
		)
		for _, fr := range []string{u.FixRoot, u.BlkRoot} {
			fLc := toLowerFirst(fr)
			fmt.Fprintf(&disp, `    if (c.root == %q) {
      if (c.op == 4 || c.op == 5) {
        final vals = <%s_fr.%s>[%s_fr.%s()];
        final plan = %s_f.%sFixedNewPlan();
        final rep = %s_fr.TableFixedReport();
        final n = %s_f.%sFixedLoad(vals, 1, c.inp, c.inp.length, plan, rep);
        if (c.op == 5) {
          if (n == 1 && rep.refused == 0) die(c.id, 'expected historical writer-bound refusal');
          return;
        }
        if (n != 1 || rep.refused != 0) die(c.id, 'FixedLoad');
        final out = Uint8List(%s_f.%sFixedMeasure(1));
        if (%s_f.%sFixedSave(vals, 1, out) != out.length) die(c.id, 'FixedSave');
        checkBytes(c.id, out, c.w1);
        return;
      }
      if (c.op == 3) {
        final vocab = %s_fr.TableVocabulary(entries: List<%s_fr.TableMessageEntry>.generate(4096, (_) => %s_fr.TableMessageEntry(0, 0)));
        final arep = %s_fr.TableReport();
        if (c.aux.isEmpty) {
          final abuf = Uint8List(%s_fr.announceMeasure());
          %s_fr.announce(abuf);
          if (!%s_fr.announceRead(vocab, abuf, arep) || arep.refused) die(c.id, 'own announceRead');
        } else {
          if (!%s_fr.announceRead(vocab, c.aux, arep) || arep.refused) die(c.id, 'peer announceRead');
        }
        final vals = List<%s_fr.%s>.generate(16, (_) => %s_fr.%s());
        final lrep = %s_fr.TableReport();
        final cnt = %s_f.%sLoadMessages(vals, c.inp, vocab, lrep);
        if (cnt < 1 || lrep.refused) die(c.id, 'LoadMessages');
        final slice = vals.sublist(0, cnt);
        final srep = %s_fr.TableReport();
        final msz = %s_f.%sMeasureMessages(slice, srep);
        if (msz < 0 || srep.refused) die(c.id, 'MeasureMessages');
        final out = Uint8List(msz);
        if (%s_f.%sSaveMessages(slice, out, srep) != msz) die(c.id, 'SaveMessages');
        checkBytes(c.id, out, c.w1);
        return;
      }
      // op 7's message half has no Dart counterpart: the text codec is the
      // wire library's FixRoot, the message writer takes the fixed library's
      // FixRoot (a distinct class), and the generated code has no conversion
      // between them. The text half is checked as the other targets check it.
      if (c.op == 6 || c.op == 7) {
        final rep = %s_w.TableTextReport();
        final v = %s_w.%sFromJson(utf8.decode(c.inp), rep);
        if (rep.refused) die(c.id, 'FromJson fix');
        final jout = Uint8List.fromList(utf8.encode(%s_w.%sToJson(v)));
        checkBytes(c.id, jout, c.w1);
        return;
      }
    }
`,
				fr,
				u.Pkg, fr, u.Pkg, fr, u.Pkg, fLc, u.Pkg, u.Pkg, fLc, u.Pkg, fLc, u.Pkg, fLc,
				u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, u.Pkg, fr, u.Pkg, fr, u.Pkg, u.Pkg, fLc, u.Pkg, u.Pkg, fLc, u.Pkg, fLc,
				u.Pkg, u.Pkg, fLc, u.Pkg, fLc,
			)
		}
		disp.WriteString("  }\n")
	}

	dartSrc := fmt.Sprintf(`import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
%s
class Case {
  String id = '', pkg = '', root = '';
  int op = 0;
  Uint8List aux = Uint8List(0), inp = Uint8List(0), w1 = Uint8List(0), w2 = Uint8List(0);
}

Never die(String id, String why) {
  stderr.writeln('FAILED: $id: $why');
  exit(1);
}

void checkBytes(String id, Uint8List got, Uint8List want) {
  if (got.length != want.length) die(id, 'len got=${got.length} want=${want.length}');
  for (var i = 0; i < got.length; i++) {
    if (got[i] != want[i]) die(id, 'diff at $i');
  }
}

void runCase(Case c) {
%s  die(c.id, 'unhandled pkg/root/op');
}

void main(List<String> args) {
  final raw = File(args[0]).readAsBytesSync();
  final bd = ByteData.sublistView(raw);
  var at = 0;
  String rstr() {
    final n = bd.getUint16(at, Endian.little);
    at += 2;
    final s = utf8.decode(Uint8List.sublistView(raw, at, at + n));
    at += n;
    return s;
  }
  Uint8List rbytes() {
    final n = bd.getUint32(at, Endian.little);
    at += 4;
    final b = Uint8List.fromList(Uint8List.sublistView(raw, at, at + n));
    at += n;
    return b;
  }
  final cnt = bd.getUint32(0, Endian.little);
  at = 4;
  for (var i = 0; i < cnt; i++) {
    final c = Case()
      ..id = rstr()
      ..pkg = rstr()
      ..root = rstr()
      ..op = raw[at++];
    c.aux = rbytes();
    c.inp = rbytes();
    c.w1 = rbytes();
    c.w2 = rbytes();
    runCase(c);
  }
  stdout.writeln('TABLEGEN_OK dart $cnt');
}
`, imp.String(), disp.String())

	mainDart := filepath.Join(workDir, "main.dart")
	if err := os.WriteFile(mainDart, []byte(dartSrc), 0o644); err != nil {
		return err
	}
	dartBin := resolveTool(repoRoot, "DART", "dist/dart-sdk-3.13.2/bin/dart", "dart")
	if out, err := runChecked(workDir, nil, dartBin, "analyze", "--fatal-infos", "--fatal-warnings", genRoot); err != nil {
		return fmt.Errorf("dart analyze: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, nil, dartBin, "--enable-asserts", mainDart, packFile)
	return requireToken(out, err, "dart", caseCount)
}

// ============================================================================
// 9. Elixir (elixir-strict.sh elixirc --warnings-as-errors)
// ============================================================================

func verifyElixir(repoRoot, schemaBin, packFile, workDir string, units []UnitSpec, caseCount int) error {
	genRoot := filepath.Join(workDir, "gen")
	if err := generateAllUnits(repoRoot, schemaBin, "", "elixir", units, genRoot); err != nil {
		return err
	}
	ebinDir := filepath.Join(workDir, "ebin")
	if err := os.MkdirAll(ebinDir, 0o755); err != nil {
		return err
	}
	beamPath := envOr("BEAM_PATH", filepath.Join(repoRoot, "dist/otp-29.0.5/bin")+":"+filepath.Join(repoRoot, "dist/elixir-1.20.4/bin"))
	extraEnv := []string{"PATH=" + beamPath + ":" + os.Getenv("PATH")}
	elixircBin := resolveTool(repoRoot, "ELIXIRC", "dist/elixir-1.20.4/bin/elixirc", "elixirc")
	elixirBin := resolveTool(repoRoot, "ELIXIR", "dist/elixir-1.20.4/bin/elixir", "elixir")

	exRunner := `defmodule TablegenRunner do
  def die(id, why) do
    IO.puts(:stderr, "FAILED: #{id}: #{why}")
    System.halt(1)
  end

  def check_bytes(id, got, want) do
    if got != want do
      die(id, "byte mismatch got=#{byte_size(got)} want=#{byte_size(want)}")
    end
  end

  def run_case(id, pkg, root, op, aux, inp, w1, w2) do
    ns = Macro.camelize(pkg)
    tmod = Module.concat([ns, "Table"])
    case op do
      1 ->
        case tmod.load(root, inp) do
          {:ok, v, _rep} ->
            {:ok, out} = tmod.save(root, v)
            check_bytes(id, out, w1)
          other -> die(id, "load: #{inspect(other)}")
        end

      2 ->
        store = %{records: [], used: 0, cap: Bitwise.bsl(1, 20), ids: [], id_cap: Bitwise.bsl(1, 17)}
        rep0 = %{unknown: 0, widened: 0, retained: 0, retain_lost: 0, reason: "", missing_required: 0}
        case tmod.load_retain(root, inp, store, rep0) do
          {:ok, v, _rep, store2} ->
            case tmod.save_retain(root, v, store2, rep0) do
              {:ok, out, _rep2, _store3} -> check_bytes(id, out, w1)
              other -> die(id, "save_retain: #{inspect(other)}")
            end
          other -> die(id, "load_retain: #{inspect(other)}")
        end

      4 ->
        fb = fixed_base(pkg)
        fmod = Module.concat([ns, "#{fb}Fixed"])
        sn = Macro.underscore(root)
        case apply(fmod, :"#{sn}_fixed_load", [inp]) do
          {:ok, [row], _rep} ->
            out = apply(fmod, :"#{sn}_fixed_save", [[row]])
            check_bytes(id, out, w1)
          other -> die(id, "fixed_load: #{inspect(other)}")
        end

      5 ->
        fb = fixed_base(pkg)
        fmod = Module.concat([ns, "#{fb}Fixed"])
        sn = Macro.underscore(root)
        case apply(fmod, :"#{sn}_fixed_load", [inp]) do
          {:ok, [_row], _rep} -> die(id, "expected historical writer-bound refusal")
          _ -> :ok
        end

      3 ->
        mmod = Module.concat([ns, "TableMessage"])
        {:ok, own_ann} = mmod.announce_read(mmod.announce())
        {:ok, own_voc} = mmod.for_root(own_ann, root)
        {:ok, read_voc} =
          if byte_size(aux) == 0 do
            {:ok, own_voc}
          else
            {:ok, peer_ann} = mmod.announce_read(aux)
            mmod.for_root(peer_ann, root)
          end
        bodies =
          case mmod.load_messages(read_voc, inp) do
            {:ok, b, _rep} -> b
            {:ok, b} -> b
            other -> die(id, "load_messages: #{inspect(other)}")
          end
        rebound =
          case mmod.rebind_messages(read_voc, own_voc, bodies) do
            {:ok, rb} -> rb
            other -> die(id, "rebind_messages: #{inspect(other)}")
          end
        out =
          case mmod.save_messages(own_voc, rebound) do
            {:ok, ob, _rep} -> ob
            {:ok, ob} -> ob
            other -> die(id, "save_messages: #{inspect(other)}")
          end
        check_bytes(id, out, w1)

      6 ->
        case tmod.from_json(root, inp) do
          {:ok, v, _rep} ->
            {:ok, jout} = tmod.to_json(root, v)
            check_bytes(id, jout, w1)
            if byte_size(w2) > 0 do
              {:ok, wout} = tmod.save(root, v)
              check_bytes(id, wout, w2)
            end
          other -> die(id, "from_json: #{inspect(other)}")
        end

      # op 7's message half has no Elixir counterpart: the Elixir message
      # writer takes id-addressed bodies ([{ref, value}]), not a record, and
      # REFUSES a field written at its default rather than eliding it
      # (TableMessage written_at_default?), so leaving a birth value out is
      # the caller's choice and there is no record writer to feed. The text
      # half is checked as the other targets check it.
      7 ->
        case tmod.from_json(root, inp) do
          {:ok, v, _rep} ->
            {:ok, jout} = tmod.to_json(root, v)
            check_bytes(id, jout, w1)
          other -> die(id, "from_json fix: #{inspect(other)}")
        end
    end
  end

  def unpack_and_run(<<cnt::little-unsigned-32, rest::binary>>) do
    loop(cnt, rest)
    IO.puts("TABLEGEN_OK elixir #{cnt}")
  end

  defp loop(0, _), do: :ok
  defp loop(rem_cnt, bin) do
    <<id_len::little-unsigned-16, id::binary-size(id_len),
      pkg_len::little-unsigned-16, pkg::binary-size(pkg_len),
      root_len::little-unsigned-16, root::binary-size(root_len),
      op::unsigned-8,
      aux_len::little-unsigned-32, aux::binary-size(aux_len),
      in_len::little-unsigned-32, inp::binary-size(in_len),
      w1_len::little-unsigned-32, w1::binary-size(w1_len),
      w2_len::little-unsigned-32, w2::binary-size(w2_len),
      tail::binary>> = bin
    run_case(id, pkg, root, op, aux, inp, w1, w2)
    loop(rem_cnt - 1, tail)
  end
end
`
	// THE FIXED MODULE IS NAMED BY THE UNIT'S FILE BASE, which the runner is
	// told per package: a corpus unit's base is its package's first four
	// letters (Tg01), but a pinned unit's is "Pin" under package pinci32, so
	// slicing the namespace named a module (PincFixed) that does not exist
	var bases strings.Builder
	seen := map[string]bool{}
	for _, u := range units {
		if !seen[u.Pkg] {
			seen[u.Pkg] = true
			fmt.Fprintf(&bases, "  defp fixed_base(%q), do: %q\n", u.Pkg, u.FileBase)
		}
	}
	exRunner = strings.Replace(exRunner, "  def run_case(", bases.String()+"\n  def run_case(", 1)
	runnerEx := filepath.Join(workDir, "runner.ex")
	if err := os.WriteFile(runnerEx, []byte(exRunner), 0o644); err != nil {
		return err
	}
	var allEx []string
	for _, u := range units {
		matches, _ := filepath.Glob(filepath.Join(genRoot, u.Pkg, "*.ex"))
		allEx = append(allEx, matches...)
	}
	allEx = append(allEx, runnerEx)

	strictSh := filepath.Join(repoRoot, "make", "elixir-strict.sh")
	cArgs := append([]string{strictSh, elixircBin, "-o", ebinDir}, allEx...)
	if out, err := runChecked(workDir, extraEnv, "sh", cArgs...); err != nil {
		return fmt.Errorf("elixirc: %w\n%s", err, out)
	}
	out, err := runChecked(workDir, extraEnv, elixirBin, "-pa", ebinDir, "-e", "TablegenRunner.unpack_and_run(File.read!(hd(System.argv())))", "--", packFile)
	return requireToken(out, err, "elixir", caseCount)
}
