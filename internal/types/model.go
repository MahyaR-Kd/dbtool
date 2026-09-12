package types

type Config struct {
	Name           string
	Host           string
	Port           string
	User           string
	RetentionDays  int
	SSH            bool
	SSHHost        string
	SSHUser        string
	SSHPort        string
	LocalPort      string
	IgnoredSchemas []string
	IgnoredTables  map[string][]string
	NoLocks        bool
	// Password is the DB password saved for this config, if the user chose
	// to save one. It is protected by a user-chosen master password (see
	// internal/credvault), separate from the auto-generated key used for
	// other stored credentials, since this one is only ever needed for an
	// interactively-run dump/restore, never for an unattended scheduled
	// job. After Load, this holds the still-encrypted value — decrypt it
	// on demand (internal/config.AskPassword does this) rather than
	// eagerly, so read-only operations like "config list" never prompt for
	// the master password.
	Password string
}
