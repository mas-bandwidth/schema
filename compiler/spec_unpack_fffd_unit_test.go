package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecUnpackFffdUnitIsOneAnswer(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	writer := specTablesSection(t, page,
		"### 16.3 What the writer refuses", "### 16.4 ")
	unpack := specTablesSection(t, page,
		"### 17.3 What comes out", "### 17.4 ")

	if !strings.Contains(writer, "one per maximal subpart") {
		t.Error("§16.3 must set the ill-formed UTF-8 writer unit to one per maximal subpart")
	}
	if !strings.Contains(unpack, "ill-formed UTF-8 case handled under §16.3") {
		t.Error("§17.3 must defer the unpack writer's ill-formed UTF-8 handling to §16.3")
	}
	if strings.Contains(unpack, "one per bad byte") {
		t.Error("§17.3 must not assign a second replacement unit to unpack")
	}
}
