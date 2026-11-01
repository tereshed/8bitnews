package config

import (
	"testing"
	"time"
)

func TestParseAPIKeys(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{raw: "", want: nil},
		{raw: "a", want: []string{"a"}},
		{raw: "a,b,c", want: []string{"a", "b", "c"}},
		{raw: " a , b ,,c ", want: []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		keys := parseAPIKeys(c.raw)
		if len(keys) != len(c.want) {
			t.Fatalf("parseAPIKeys(%q) = %v, want %v", c.raw, keys, c.want)
		}
		for _, k := range c.want {
			if !keys[k] {
				t.Errorf("parseAPIKeys(%q) missing key %q", c.raw, k)
			}
		}
	}
}

func TestModerationSettings(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_MODERATION_CHAT_ID", "")
	cfg, err := Load()
	if err != nil || cfg.Moderation.BotToken != "" || cfg.Moderation.ChatID != 0 {
		t.Fatalf("without a token: %+v, %v; want the bot off", cfg.Moderation, err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	if _, err := Load(); err == nil {
		t.Fatal("a token without a chat id loaded")
	}
	t.Setenv("TELEGRAM_MODERATION_CHAT_ID", "the group")
	if _, err := Load(); err == nil {
		t.Fatal("a chat id that isn't a number loaded")
	}
	t.Setenv("TELEGRAM_MODERATION_CHAT_ID", "-5012345678")
	cfg, err = Load()
	if err != nil || cfg.Moderation.ChatID != -5012345678 || cfg.Moderation.PollInterval <= 0 {
		t.Fatalf("with both: %+v, %v", cfg.Moderation, err)
	}
}

// One source the hub can't reach goes check -> Save Page Now -> availability
// lookup, one after another; the default deadline must fit that chain and
// still answer before newsroom/tools/hublib.py gives up (120 s). With a 25 s
// deadline, a check that hung for 15 s left Save Page Now 10 s, and a CBC
// source with no archive copy yet was refused (trial 2, 2026-11-01).
func TestSourceDeadlineFitsOneSlowSource(t *testing.T) {
	for _, k := range []string{"HUB_SOURCE_CHECK_TIMEOUT", "HUB_WAYBACK_SAVE_TIMEOUT", "HUB_SOURCES_CHECK_DEADLINE"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	chain := cfg.SourceCheckTimeout + cfg.WaybackSaveTimeout + cfg.SourceCheckTimeout
	if cfg.SourcesCheckDeadline < chain || cfg.SourcesCheckDeadline >= 120*time.Second {
		t.Fatalf("deadline %s: want at least %s (check + save + lookup) and under 120s", cfg.SourcesCheckDeadline, chain)
	}
}
