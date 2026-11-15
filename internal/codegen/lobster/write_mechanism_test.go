package lobster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

const writeMechanismSchema = `package write_mechanism

type Packet
{
    value int8 | min = 0, max = 5
}
`

func TestLobsterWriteMechanismPlaced(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeFile(t, src, "WriteMechanism.schema", writeMechanismSchema)
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", `import .WriteMechanism
import .serialize_stream

let p = write_mechanism.NewPacket()
p.Value = 6
let s = serialize_write_stream(ensure_size("", 8, 0))
write_mechanism.WritePacket(s, p)
print("write completed")
`)

	defaultOutput, defaultExit := lobsterrun.RunExitFlags(t, dir, "main.lobster", nil, nil)
	releaseOutput, releaseExit := lobsterrun.RunExitFlags(t, dir, "main.lobster", []string{"--runtime-no-asserts"}, nil)
	defaultTrapped := defaultExit != 0
	releaseTrapped := releaseExit != 0
	t.Logf("default flags: trapped=%t exit=%d output=%q", defaultTrapped, defaultExit, strings.TrimSpace(string(defaultOutput)))
	t.Logf("--runtime-no-asserts: trapped=%t exit=%d output=%q", releaseTrapped, releaseExit, strings.TrimSpace(string(releaseOutput)))
	if !defaultTrapped || releaseTrapped {
		t.Fatalf("pinned default JIT should trap the ranged write and --runtime-no-asserts should remove that assert; got default exit %d, release exit %d", defaultExit, releaseExit)
	}

	port, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "PORT-LOBSTER.md"))
	if err != nil {
		t.Fatal(err)
	}
	releaseConfigured := strings.Contains(string(port), "The release configuration is the default JIT with `--runtime-no-asserts`.")

	rulings, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "rulings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Rulings []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"rulings"`
	}
	if err := json.Unmarshal(rulings, &document); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	var source string
	for _, ruling := range document.Rulings {
		if ruling.ID == "write-refusal" {
			source = ruling.Source
			break
		}
	}
	if source == "" {
		t.Fatal("docs/rulings.json has no write-refusal source")
	}

	packetList := "refusal in every build"
	if defaultTrapped && !releaseTrapped && releaseConfigured {
		packetList = "debug-only assert"
	}
	if !containsWriteRefusalLeg(t, source, packetList, "Lobster packet writes") {
		t.Errorf("write-refusal does not place Lobster in the %q list selected by the pinned packet runs and release configuration", packetList)
	}
	if !strings.Contains(source, "table writers SaveChecked") ||
		!strings.Contains(source, "SaveChecked") || !strings.Contains(source, "save_reasoned") {
		t.Error("write-refusal does not name the table writers' every-build refusal paths")
	}
}

func containsWriteRefusalLeg(t *testing.T, source, marker, want string) bool {
	t.Helper()
	i := strings.Index(source, marker)
	if i < 0 {
		t.Fatalf("write-refusal source has no %q marker", marker)
	}
	rest := source[i:]
	open, close := strings.Index(rest, "("), strings.Index(rest, ")")
	if open < 0 || close < open {
		t.Fatalf("write-refusal source has no parenthesized list after %q", marker)
	}
	for _, leg := range strings.Split(rest[open+1:close], ",") {
		if strings.TrimSpace(leg) == want {
			return true
		}
	}
	return false
}
