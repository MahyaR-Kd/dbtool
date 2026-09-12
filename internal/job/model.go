package job

// Config JobConfig holds a scheduled dump, restore, or sync (dump+restore) task.
type Config struct {
	Name            string // unique job name
	Type            string // "dump", "restore", or "sync"
	Schedule        string // 5-field cron expression, e.g. "0 2 * * *"
	SrcConfigName   string // source DB config name (used for dump and sync)
	SrcPassword     string // source DB password
	DstConfigName   string // destination DB config name (used for restore and sync)
	DstPassword     string // destination DB password
	OverwriteTables bool   // pass --overwrite-tables to myloader (restore and sync only)
}
