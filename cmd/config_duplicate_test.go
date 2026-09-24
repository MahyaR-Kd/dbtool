package cmd

import (
	"dbtool/internal/types"
	"testing"
)

func TestCloneConfigIsIndependentAndGetsUniqueName(t *testing.T) {
	source := types.Config{Name: "prod", IgnoredSchemas: []string{"one"}, IgnoredTables: map[string][]string{"one": {"table"}}}
	copy := cloneConfig(source)
	copy.IgnoredSchemas[0] = "changed"
	copy.IgnoredTables["one"][0] = "changed"
	if source.IgnoredSchemas[0] != "one" || source.IgnoredTables["one"][0] != "table" {
		t.Fatal("clone shares mutable fields")
	}
	configs := []types.Config{{Name: "prod"}, {Name: "prod-copy"}}
	if got := availableCopyName("prod", configs); got != "prod-copy-2" {
		t.Fatalf("copy name = %q", got)
	}
}
