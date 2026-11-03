//go:build !headless

package desktop

import (
	"maps"
)

func (d *Desktop) syncProfileHotkeys() {
	logger := d.logger
	next := map[string]string{}
	for _, profile := range d.config.Values().Profiles {
		if profile.Hotkey != "" {
			next[profile.Hotkey] = profile.Name
		}
	}

	// switching profiles reloads the config too, and the hotkey set is
	// unchanged then - don't churn the OS registrations over it
	if maps.Equal(d.profileHotkeys, next) {
		return
	}

	if err := d.app.GlobalShortcut.UnregisterAll(); err != nil {
		logger.Warnw("Failed to unregister profile hotkeys", "error", err)
	}

	for hotkey, name := range next {
		if err := d.app.GlobalShortcut.Register(hotkey, func() {
			logger.Infow("Profile hotkey pressed, switching profile", "profile", name)

			if err := d.config.SetActiveProfile(name); err != nil {
				logger.Warnw("Failed to switch profile", "profile", name, "error", err)
				return
			}

			// a global hotkey fires while deej is invisible, so the toast
			// is the only feedback the user gets
			d.notifier.Notify(getProfileSwitchedText(d, name))
		}); err != nil {
			// usually another application already owns the combination.
			// The entry stays in the set so an unrelated reload doesn't
			// retry (and re-churn) it; editing the hotkey does
			logger.Warnw("Failed to register profile hotkey", "hotkey", hotkey, "profile", name, "error", err)
		}
	}

	d.profileHotkeys = next
}
