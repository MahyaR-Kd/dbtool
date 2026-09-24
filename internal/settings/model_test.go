package settings

import "testing"

func TestOptionalServicesCanBeDisabledWithoutClearingConfiguration(t *testing.T) {
	s := Settings{StorageType: StorageS3, S3: S3Config{Bucket: "b", Region: "r", AccessKey: "a", SecretKey: "s"}}
	if !s.S3Enabled() {
		t.Fatal("configured S3 should be enabled by default")
	}
	s.S3.Disabled = true
	if s.S3Enabled() || !s.S3.Configured() {
		t.Fatal("disabled S3 should retain configuration")
	}

	tg := TelegramConfig{BotToken: "token", ChatID: "chat"}
	if !tg.Enabled() {
		t.Fatal("configured Telegram should be enabled by default")
	}
	tg.Disabled = true
	if tg.Enabled() || !tg.Configured() {
		t.Fatal("disabled Telegram should retain configuration")
	}

	p := ProxyConfig{Host: "host", Port: "1080"}
	if !p.Enabled() {
		t.Fatal("configured proxy should be enabled by default")
	}
	p.Disabled = true
	if p.Enabled() || !p.Configured() {
		t.Fatal("disabled proxy should retain configuration")
	}
}
