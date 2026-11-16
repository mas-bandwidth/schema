package compiler

import "testing"

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryIntegrity) }

func TestPythonIntegrityPublicDriver(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		"package crcprobe\ntable Root | integrity=\"crc32c\" { x int32 }\n",
		"package crcprobe\nfixed table Root | integrity=\"crc32c\" { x int32 }\n",
	} {
		if _, err := New().Generate(unitFromSource(t, schema), "python", nil); err != nil {
			t.Fatalf("Python refuses CRC root: %v", err)
		}
	}
}
