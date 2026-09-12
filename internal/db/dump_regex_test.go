package db

import (
	"regexp"
	"testing"
)

// mydumperExcludes replicates mydumper's own check_regex (src/regex.c)
// combined with dbtool's negative-lookahead wrapping: mydumper tests
// "db.table" when checking a specific table, and the bare "db" (no dot)
// when checking whether to include the schema's own CREATE DATABASE
// statement. An object is excluded from the dump when ANY exclusion branch
// matches that subject string (matching how "^(?!(branch1|branch2|...))"
// behaves) — the branches themselves are plain RE2-compatible fragments
// with no lookahead, so Go's stdlib regexp can compile and test them
// directly even though it can't compile the outer lookahead wrapper.
func mydumperExcludes(t *testing.T, exclusions []string, database, table string) bool {
	t.Helper()
	subject := database
	if table != "" {
		subject = database + "." + table
	}
	for _, pattern := range exclusions {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("invalid exclusion branch %q: %v", pattern, err)
		}
		if re.MatchString(subject) {
			return true
		}
	}
	return false
}

func TestExclusionPatterns_ExcludesSchemaCreateStatement(t *testing.T) {
	// This is the real-world bug: a fully ignored schema's CREATE DATABASE
	// statement was leaking into the dump even though its tables were
	// correctly skipped, because the old pattern ("schema\.") never
	// matched the bare "schema" string mydumper tests when deciding
	// whether to write that statement.
	patterns := exclusionPatterns([]string{"secrets"}, nil)
	if len(patterns) == 0 {
		t.Fatal("expected at least one exclusion pattern")
	}

	if !mydumperExcludes(t, patterns, "secrets", "") {
		t.Error("schema-level CREATE DATABASE check: 'secrets' should be excluded, but was not")
	}
	if !mydumperExcludes(t, patterns, "secrets", "users") {
		t.Error("table-level check: 'secrets.users' should be excluded, but was not")
	}
	if mydumperExcludes(t, patterns, "public", "") {
		t.Error("schema-level check: 'public' should be included, but was excluded")
	}
	if mydumperExcludes(t, patterns, "public", "users") {
		t.Error("table-level check: 'public.users' should be included, but was excluded")
	}
}

func TestExclusionPatterns_DoesNotFalsePositiveOnPrefix(t *testing.T) {
	// A schema named "app" must not accidentally exclude a different schema
	// named "app_logs" just because it shares a prefix.
	patterns := exclusionPatterns([]string{"app"}, nil)

	if !mydumperExcludes(t, patterns, "app", "") {
		t.Error("'app' should be excluded")
	}
	if mydumperExcludes(t, patterns, "app_logs", "") {
		t.Error("'app_logs' should NOT be excluded just because it starts with 'app'")
	}
	if mydumperExcludes(t, patterns, "app_logs", "events") {
		t.Error("'app_logs.events' should NOT be excluded")
	}
}

func TestExclusionPatterns_IndividualTables(t *testing.T) {
	patterns := exclusionPatterns(nil, map[string][]string{
		"app": {"logs", "audit"},
	})

	if !mydumperExcludes(t, patterns, "app", "logs") {
		t.Error("'app.logs' should be excluded")
	}
	if !mydumperExcludes(t, patterns, "app", "audit") {
		t.Error("'app.audit' should be excluded")
	}
	if mydumperExcludes(t, patterns, "app", "users") {
		t.Error("'app.users' should NOT be excluded (not in the ignored-tables list)")
	}
	// The schema itself must NOT be excluded — only specific tables were ignored.
	if mydumperExcludes(t, patterns, "app", "") {
		t.Error("schema 'app' should still be included — only specific tables were ignored, not the whole schema")
	}
}

func TestExclusionPatterns_CombinedSchemaAndTableExclusions(t *testing.T) {
	patterns := exclusionPatterns(
		[]string{"secrets"},
		map[string][]string{"app": {"logs"}},
	)

	if !mydumperExcludes(t, patterns, "secrets", "") {
		t.Error("'secrets' schema should be excluded")
	}
	if !mydumperExcludes(t, patterns, "secrets", "anytable") {
		t.Error("'secrets.anytable' should be excluded")
	}
	if !mydumperExcludes(t, patterns, "app", "logs") {
		t.Error("'app.logs' should be excluded")
	}
	if mydumperExcludes(t, patterns, "app", "") {
		t.Error("'app' schema itself should still be included")
	}
	if mydumperExcludes(t, patterns, "app", "users") {
		t.Error("'app.users' should still be included")
	}
}

func TestBuildExclusionRegex_EmptyWhenNothingIgnored(t *testing.T) {
	if got := buildExclusionRegex(nil, nil); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestBuildExclusionRegex_WrapsInNegativeLookahead(t *testing.T) {
	got := buildExclusionRegex([]string{"secrets"}, nil)
	want := `^(?!(secrets(\.|$)))`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExclusionPatterns_DeterministicAcrossMapIterations(t *testing.T) {
	tables := map[string][]string{
		"z_schema": {"t1"},
		"a_schema": {"t2"},
		"m_schema": {"t3"},
	}
	first := buildExclusionRegex(nil, tables)
	for i := 0; i < 20; i++ {
		if got := buildExclusionRegex(nil, tables); got != first {
			t.Fatalf("regex output not deterministic across calls:\n  first: %s\n  got:   %s", first, got)
		}
	}
}
