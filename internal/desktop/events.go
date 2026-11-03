//go:build !headless

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// wails events pushed to the settings window
const (
	eventSliders  = "deej:sliders"  // []float32, 0..1 per slider
	eventState    = "deej:state"    // {connected bool, comPort string}
	eventConfig   = "deej:config"   // no payload; config was (re)applied
	eventSessions = "deej:sessions" // no payload; audio sessions changed
	eventDiscord  = "deej:discord"  // no payload; the discord voice roster changed
)

func (d *Desktop) subscribeEvents(statusInfo, sessionsInfo *application.MenuItem, setTooltip, setValuesInfo, rebuildProfilesMenu func()) {
	app := d.app
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		d.runtimeMu.Lock()
		defer d.runtimeMu.Unlock()
		if d.closing.Load() || d.runtimeStarted {
			return
		}
		d.runtimeStarted = true
		d.logger.Debug("Tray instance ready")

		application.InvokeAsync(func() {
			if !d.closing.Load() {
				setTooltip()
			}
		})

		sliderMovedChannel := d.serial.SubscribeToSliderMoveEvents()
		stateChangeChannel := d.serial.SubscribeToStateChangeEvent()
		sessionCountChangeChannel := d.sessions.SubscribeToSessionCountChange()
		configReloadedChannel := d.config.SubscribeToChanges()
		discordRosterChannel := d.discord.SubscribeToRosterChange()

		emitState := func() {
			app.Event.Emit(eventState, map[string]any{
				"connected": d.serial.GetState(),
				"comPort":   d.serial.CurrentComPort(),
			})
		}

		// wait on things to happen; menu item mutations must run on the wails main thread
		d.eventWorkers.Go(func() {
			for {
				select {
				case <-d.eventsDone:
					return
				// slider moved
				case <-sliderMovedChannel:
					if d.closing.Load() {
						continue
					}
					application.InvokeAsync(func() {
						if !d.closing.Load() {
							setTooltip()
							setValuesInfo()
						}
					})
					app.Event.Emit(eventSliders, d.serial.CurrentSliderPercentValues())

				// connection state changed
				case <-stateChangeChannel:
					if d.closing.Load() {
						continue
					}
					application.InvokeAsync(func() {
						if d.closing.Load() {
							return
						}
						setTooltip()
						setValuesInfo()
						statusInfo.SetLabel(getStatusItemTitle(d))
					})
					emitState()
					app.Event.Emit(eventSliders, d.serial.CurrentSliderPercentValues())

				// someone joined or left the discord voice channel
				case <-discordRosterChannel:
					if d.closing.Load() {
						continue
					}
					app.Event.Emit(eventDiscord)

				// session count changed
				case <-sessionCountChangeChannel:
					if d.closing.Load() {
						continue
					}
					application.InvokeAsync(func() {
						if !d.closing.Load() {
							sessionsInfo.SetLabel(getSessionsCountString(d))
						}
					})
					app.Event.Emit(eventSessions)

				// config applied (GUI save or manual edit)
				case <-configReloadedChannel:
					if d.closing.Load() {
						continue
					}
					rebuildProfilesMenu()
					d.syncProfileHotkeys()
					app.Event.Emit(eventConfig)
					emitState()
					app.Event.Emit(eventSliders, d.serial.CurrentSliderPercentValues())
				}
			}
		})

		// actually start the main runtime
		d.runtimeWorkers.Go(d.runCore)
	})

}
