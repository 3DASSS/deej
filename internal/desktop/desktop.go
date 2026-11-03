//go:build !headless

// Package desktop hosts the settings UI, tray menu and global shortcuts.
package desktop

import (
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nik9play/deej/internal/config"
	"github.com/nik9play/deej/internal/integrations/discord"
	"github.com/nik9play/deej/internal/integrations/obs"
	"github.com/nik9play/deej/internal/mixer"
	"github.com/nik9play/deej/internal/notify"
	"github.com/nik9play/deej/internal/serial"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
)

// Options supplies services and lifecycle operations from the bootstrap.
type Options struct {
	Config      *config.Store
	Serial      *serial.Client
	Mixer       *mixer.Mixer
	OBS         *obs.Client
	Discord     *discord.Client
	Logger      *zap.SugaredLogger
	Notifier    notify.Notifier
	Localizer   *i18n.Localizer
	Language    string
	Version     string
	Start       func()
	Wait        func()
	Stop        func() error
	RequestStop func()
}

// Desktop owns Wails resources and independent subscriptions to core events.
type Desktop struct {
	config                    *config.Store
	serial                    *serial.Client
	sessions                  *mixer.Mixer
	obs                       *obs.Client
	discord                   *discord.Client
	logger                    *zap.SugaredLogger
	notifier                  notify.Notifier
	localizer                 *i18n.Localizer
	resolvedLanguage, version string
	app                       *application.App
	settingsLock              sync.Mutex
	shutdownDone              chan struct{}
	eventsDone                chan struct{}
	eventWorkers              sync.WaitGroup
	runtimeWorkers            sync.WaitGroup
	runtimeMu                 sync.Mutex
	runtimeStarted            bool
	closing                   atomic.Bool
	eventsOnce                sync.Once
	allowQuit                 atomic.Bool
	profileHotkeys            map[string]string
	start, wait, requestStop  func()
	stop                      func() error
}

// New prepares the shell; Run must execute on the main thread.
func New(o Options) *Desktop {
	return &Desktop{
		config: o.Config, serial: o.Serial, sessions: o.Mixer, obs: o.OBS, discord: o.Discord, logger: o.Logger, notifier: o.Notifier, localizer: o.Localizer,
		resolvedLanguage: o.Language, version: o.Version, start: o.Start, wait: o.Wait, stop: o.Stop, requestStop: o.RequestStop, eventsDone: make(chan struct{}), profileHotkeys: map[string]string{},
	}
}
func (d *Desktop) stopEvents() {
	d.eventsOnce.Do(func() { close(d.eventsDone) })
	d.eventWorkers.Wait()
}
func (d *Desktop) runCore() {
	d.runCoreWithQuit(func() {
		// Quit uses a synchronous main-thread dispatch. Queue it without blocking
		// this worker so Run can still join us if its native loop already ended.
		application.InvokeAsync(func() {
			if !d.closing.Load() {
				d.app.Quit()
			}
		})
	})
}

func (d *Desktop) runCoreWithQuit(quit func()) {
	d.start()
	d.wait()
	if err := d.stop(); err != nil {
		d.logger.Warnw("Failed to stop core", "error", err)
	}
	// Serial emits its final disconnected event during Stop; retain every consumer until then.
	d.stopEvents()
	d.allowQuit.Store(true)
	if !d.closing.Load() {
		quit()
	}
}

// cleanupCore also covers failure before ApplicationStarted. Prevent later
// callbacks from creating consumers or starting a core after cleanup has begun.
func (d *Desktop) cleanupCore() {
	d.runtimeMu.Lock()
	d.closing.Store(true)
	d.runtimeMu.Unlock()
	d.requestStop()
	if err := d.stop(); err != nil {
		d.logger.Warnw("Failed to stop core", "error", err)
	}
	d.stopEvents()
	d.runtimeWorkers.Wait()
}
