package mixer

import "testing"

// the slider handler has to route discord targets away from the audio session
// path, and leave everything else alone
func TestDiscordTargetRouting(t *testing.T) {
	sessions := &Mixer{}

	handled := []string{
		"deej.discord.input",
		"deej.discord.output",
		"deej.discord:Nikita",
		"DEEJ.DISCORD:Nikita", // targets are case-insensitive
	}

	for _, target := range handled {
		if !sessions.applySpecialTargetAction(target, 0.5) {
			t.Errorf("%q should be handled as a discord target", target)
		}
	}

	notHandled := []string{"discord.exe", "master", "deej.current", "deej.discordant"}

	for _, target := range notHandled {
		if sessions.applySpecialTargetAction(target, 0.5) {
			t.Errorf("%q should fall through to the audio session path", target)
		}
	}
}
