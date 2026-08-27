package deej

import "testing"

type testSession struct{ baseSession }

func (*testSession) GetVolume() float32      { return 0 }
func (*testSession) SetVolume(float32) error { return nil }
func (*testSession) Release()                {}

func TestSessionMappedIgnoresMixedCaseSpecialTarget(t *testing.T) {
	settings := defaultSettings()
	settings.Profiles[0].SliderMapping = SliderMappings{
		{Slider: 0, Targets: []string{"DEEJ.DISCORD:Nikita"}},
	}
	config := &CanonicalConfig{}
	config.current.Store(&settings)
	m := &sessionMap{deej: &Deej{config: config}}

	if m.sessionMapped(&testSession{baseSession: baseSession{name: "other.exe"}}) {
		t.Fatal("Discord target should not map an audio session")
	}
}
