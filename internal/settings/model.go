package settings

// StorageType indicates where dump files are stored.
type StorageType string

const (
	StorageLocal StorageType = "local"
	StorageS3    StorageType = "s3"
)

// S3Config holds credentials and location for S3 storage.
type S3Config struct {
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Prefix    string `json:"prefix"`
	Endpoint  string `json:"endpoint"` // optional custom endpoint (e.g. MinIO)
}

// ProxyConfig holds SOCKS5 proxy settings used when connecting to databases.
type ProxyConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

// Enabled reports whether a proxy is configured (host and port are required).
func (p ProxyConfig) Enabled() bool {
	return p.Host != "" && p.Port != ""
}

// DefaultTelegramChunkSizeMB is the chunk size used when TelegramConfig.ChunkSizeMB
// is unset (0). It stays comfortably under the standard cloud Bot API's 50 MB
// per-file limit to leave headroom for multipart form overhead.
const DefaultTelegramChunkSizeMB = 49

// TelegramConfig holds credentials for delivering dump archives to a Telegram
// chat via a bot, split into chunks that respect Telegram's file-size limit.
type TelegramConfig struct {
	BotToken    string `json:"bot_token"`
	ChatID      string `json:"chat_id"`
	ChunkSizeMB int    `json:"chunk_size_mb,omitempty"` // 0 = use DefaultTelegramChunkSizeMB
}

// Enabled reports whether Telegram delivery is configured (bot token and chat ID
// are both required).
func (t TelegramConfig) Enabled() bool {
	return t.BotToken != "" && t.ChatID != ""
}

// ChunkSizeBytes returns the configured chunk size in bytes, falling back to
// DefaultTelegramChunkSizeMB when unset.
func (t TelegramConfig) ChunkSizeBytes() int64 {
	mb := t.ChunkSizeMB
	if mb <= 0 {
		mb = DefaultTelegramChunkSizeMB
	}
	return int64(mb) * 1024 * 1024
}

// Settings holds user-level tool preferences.
type Settings struct {
	WorkDir     string         `json:"work_dir"`
	StorageType StorageType    `json:"storage_type"`
	S3          S3Config       `json:"s3"`
	Proxy       ProxyConfig    `json:"proxy,omitempty"`
	Telegram    TelegramConfig `json:"telegram,omitempty"`
}
