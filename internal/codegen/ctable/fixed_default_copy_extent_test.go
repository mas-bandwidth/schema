package ctable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// An actual generated older writer supplies the file. Each newer declaration
// appends native default storage across the word-copy size boundaries; its
// reader must land the old value and initialize every added byte.
func TestCFixedDefaultCopyNativeExtents(t *testing.T) {
	slowtest.Gate(t, "the C compiler (ordinary generated-writer evolution)")
	const older = "package probe\nfixed table Root { keep uint8 = 42 }\n"
	file := filepath.Join(t.TempDir(), "older.bin")
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	t14Run(t, cc, older, nil, fmt.Sprintf(`
int main( void )
{
    Root value;
    uint8_t data[512];
    int64_t n;
    FILE * file;
    root_reset( &value );
    n = root_fixed_save( &value, 1, data, sizeof( data ) );
    if ( n < 1 ) { return 1; }
    file = fopen( %q, "wb" );
    if ( file == NULL ) { return 1; }
    if ( fwrite( data, 1, (size_t) n, file ) != (size_t) n ) { fclose( file ); return 1; }
    return fclose( file ) != 0;
}
`, file), "")
	for _, size := range []int{1, 3, 16, 17, 31, 32, 33, 40, 64, 65} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			newer := fmt.Sprintf("package probe\nfixed table Root { keep uint8 = 42\n added [%d]uint8 }\n", size)
			out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, `
    size_t i;
    if ( n != 1 || r.refused || back[0].keep != 42 ) { return 1; }
    for ( i = 0; i < sizeof( back[0].added ); ++i )
    {
        if ( back[0].added[i] != 0 ) { return 1; }
    }
`, "    memset( back[0].added, 0xA5, sizeof( back[0].added ) );")
			if err != nil {
				t.Fatalf("ordinary older-writer default fill: %v\n%s", err, out)
			}
		})
	}
}
