package config

import "testing"

func TestNormalizeDiscordRequiresCredentials(t *testing.T) {
	settings := defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true}

	problems := settings.normalize()
	if len(problems) != 2 {
		t.Fatalf("expected both credentials to be reported, got %v", problems)
	}
	if settings.Discord.Enabled {
		t.Error("enabling without credentials should leave the integration off")
	}

	// a client secret pasted into the client id field is caught up front
	settings = defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true, ClientID: "not-a-snowflake", ClientSecret: "secret"}

	if problems := settings.normalize(); len(problems) != 1 {
		t.Fatalf("expected the non-numeric client id to be reported, got %v", problems)
	}

	// the valid case is left alone, whitespace aside
	settings = defaultSettings()
	settings.Discord = DiscordSettings{Enabled: true, ClientID: " 1234567890 ", ClientSecret: " secret "}

	if problems := settings.normalize(); len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
	if settings.Discord.ClientID != "1234567890" || settings.Discord.ClientSecret != "secret" {
		t.Errorf("credentials should be trimmed, got %+v", settings.Discord)
	}
	if !settings.Discord.Enabled {
		t.Error("valid credentials should stay enabled")
	}
}
