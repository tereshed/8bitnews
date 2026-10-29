package config

import "testing"

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
