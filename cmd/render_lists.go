package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MahyaR-Kd/dbtool/internal/job"
	"github.com/MahyaR-Kd/dbtool/internal/types"
	"github.com/MahyaR-Kd/dbtool/internal/ui"
)

// Shared renderers for config and job lists, used by "config list",
// "job list" and the dashboard so the same data always looks the same.

// configHeaders are the column headers matching configRows.
var configHeaders = []string{"NAME", "HOST", "ROUTE", "RETENTION", "FLAGS"}

// configRows renders one table row per config. Passwords are only checked
// for presence, never decrypted — listing must not prompt for the master
// password.
func configRows(configs []types.Config) [][]string {
	rows := make([][]string, 0, len(configs))
	for _, c := range configs {
		rows = append(rows, []string{
			ui.Bold(c.Name),
			c.Host + ":" + c.Port,
			configRoute(c),
			configRetention(c.RetentionDays),
			configFlags(c),
		})
	}
	return rows
}

// configRoute is "direct" or "ssh user@host" (with ":port" when it isn't 22).
func configRoute(c types.Config) string {
	if !c.SSH {
		return ui.Muted("direct")
	}
	target := c.SSHHost
	if c.SSHUser != "" {
		target = c.SSHUser + "@" + target
	}
	if c.SSHPort != "" && c.SSHPort != "22" {
		target += ":" + c.SSHPort
	}
	return "ssh " + target
}

func configRetention(days int) string {
	if days <= 0 {
		return ui.Muted("forever")
	}
	return ui.Plural(days, "day", "days")
}

// configFlags joins the short markers that apply to c, or a dimmed dash.
func configFlags(c types.Config) string {
	var flags []string
	if c.Password != "" {
		flags = append(flags, "password saved")
	}
	if c.NoLocks {
		flags = append(flags, "no-locks")
	}
	if c.TelegramDisabled {
		flags = append(flags, "telegram off")
	}
	if ignores := ignoreSummary(c); ignores != "" {
		flags = append(flags, ignores)
	}
	if len(flags) == 0 {
		return ui.Muted("—")
	}
	return ui.Muted(strings.Join(flags, " · "))
}

// ignoreSummary is "ignores 2 schemas/5 tables" (only the parts that apply),
// or "" when c ignores nothing.
func ignoreSummary(c types.Config) string {
	var parts []string
	if n := len(c.IgnoredSchemas); n > 0 {
		parts = append(parts, ui.Plural(n, "schema", "schemas"))
	}
	if n := ignoredTableCount(c); n > 0 {
		parts = append(parts, ui.Plural(n, "table", "tables"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "ignores " + strings.Join(parts, "/")
}

func ignoredTableCount(c types.Config) int {
	n := 0
	for _, tables := range c.IgnoredTables {
		n += len(tables)
	}
	return n
}

// ignoreDetail spells out c's ignore rules in full —
// "schemas: a, b; tables: s.t1, s.t2" — or "" when there are none.
func ignoreDetail(c types.Config) string {
	var parts []string
	if len(c.IgnoredSchemas) > 0 {
		parts = append(parts, "schemas: "+strings.Join(c.IgnoredSchemas, ", "))
	}
	if len(c.IgnoredTables) > 0 {
		schemas := make([]string, 0, len(c.IgnoredTables))
		for schema := range c.IgnoredTables {
			schemas = append(schemas, schema)
		}
		sort.Strings(schemas)
		var tables []string
		for _, schema := range schemas {
			for _, t := range c.IgnoredTables[schema] {
				tables = append(tables, schema+"."+t)
			}
		}
		parts = append(parts, "tables: "+strings.Join(tables, ", "))
	}
	return strings.Join(parts, "; ")
}

// configOption is the picker label for a config: "name  host:port".
func configOption(c types.Config) string {
	return fmt.Sprintf("%s  %s", c.Name, ui.Muted(c.Host+":"+c.Port))
}

// jobHeaders are the column headers matching jobRows.
var jobHeaders = []string{"NAME", "TYPE", "FLOW", "SCHEDULE", "NEXT RUN"}

// jobRows renders one table row per job; NEXT RUN is relative to now.
func jobRows(jobs []job.Config, now time.Time) [][]string {
	rows := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		next := job.DescribeNext(j.Schedule, now)
		if next == "" {
			next = ui.Muted("—")
		}
		rows = append(rows, []string{
			ui.Bold(j.Name),
			jobTypeBadge(j.Type),
			jobFlow(j),
			j.Schedule,
			next,
		})
	}
	return rows
}

// jobTypeBadge colors a job type: dump blue, restore amber, sync purple.
func jobTypeBadge(t string) string {
	switch t {
	case "dump":
		return ui.Badge(t, ui.BadgeInfo)
	case "restore":
		return ui.Badge(t, ui.BadgeWarn)
	case "sync":
		return ui.Badge(t, ui.BadgeSync)
	case "":
		return ui.Muted("—")
	}
	return ui.Badge(t, ui.BadgeNeutral)
}

// jobFlow is "src" for dumps and "src → dst" for restores and syncs.
func jobFlow(j job.Config) string {
	arrow := " → "
	if ui.ASCII() {
		arrow = " -> "
	}
	src, dst := j.SrcConfigName, j.DstConfigName
	switch {
	case src != "" && dst != "" && j.Type != "dump":
		return src + ui.Muted(arrow) + dst
	case src != "":
		return src
	case dst != "":
		return ui.Muted("?"+arrow) + dst
	}
	return ui.Muted("—")
}

// jobOption is the picker label for a job: "name  type · schedule".
func jobOption(j job.Config) string {
	return fmt.Sprintf("%s  %s", j.Name, ui.Muted(j.Type+" · "+j.Schedule))
}
