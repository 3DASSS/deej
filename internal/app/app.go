// Package app composes the device, audio mixer and integrations without a GUI dependency.
package app

import (
	"path/filepath"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nik9play/deej/internal/audio"
	"github.com/nik9play/deej/internal/config"
	locali18n "github.com/nik9play/deej/internal/i18n"
	"github.com/nik9play/deej/internal/integrations/discord"
	"github.com/nik9play/deej/internal/integrations/obs"
	"github.com/nik9play/deej/internal/mixer"
	"github.com/nik9play/deej/internal/notify"
	"github.com/nik9play/deej/internal/platform"
	"github.com/nik9play/deej/internal/serial"
	"go.uber.org/zap"
)

// App owns the core services and their lifetime. Construct and Initialize before Start.
type App struct {
	logger           *zap.SugaredLogger
	notifier         notify.Notifier
	config           *config.Store
	serial           *serial.Client
	sessions         *mixer.Mixer
	obs              *obs.Client
	discord          *discord.Client
	localizer        *i18n.Localizer
	resolvedLanguage string
	lifecycle        sync.Mutex
	started          bool
	stopped          bool
	stopRequest      chan struct{}
	requestOnce      sync.Once
	watcher          sync.WaitGroup
	stopErr          error
}

// New loads the initial settings and resolves the language before constructing consumers.
func New(logger *zap.SugaredLogger, verbose bool, configPath string) (*App, error) {
	d := &App{logger: logger.Named("deej"), stopRequest: make(chan struct{})}
	catalog, err := locali18n.New(logger)
	if err != nil {
		return nil, err
	}
	d.localizer, err = catalog.SystemLocalizer()
	if err != nil {
		return nil, err
	}
	d.notifier, err = notify.NewToastNotifier(logger)
	if err != nil {
		return nil, err
	}
	d.config, err = config.New(logger, configPath, d.configOutcome)
	if err != nil {
		return nil, err
	}
	if err = d.config.Load(); err != nil {
		return nil, err
	}
	d.localizer, d.resolvedLanguage, err = catalog.Resolve(d.config.Values().Language)
	if err != nil {
		return nil, err
	}
	d.serial, err = serial.New(d.config, logger, serial.Options{Verbose: verbose, OnConnected: d.serialConnected, OnDisconnected: d.serialDisconnected})
	if err != nil {
		return nil, err
	}
	d.obs = obs.New(d.config, logger)
	d.discord = discord.New(d.config, logger, filepath.Join(filepath.Dir(d.config.Path()), discord.TokenFileName), discordVolumeSource(d.config.Values, d.serial.CurrentSliderPercentValues))
	finder, err := audio.NewSessionFinder(logger)
	if err != nil {
		_ = d.Stop()
		return nil, err
	}
	d.sessions, err = mixer.New(d.config, d.serial, logger, finder, d.obs, d.discord, platform.GetCurrentWindowProcessNames)
	if err != nil {
		_ = finder.Release()
		_ = d.Stop()
		return nil, err
	}
	return d, nil
}

// Initialize registers mixer handlers before audio discovery and before serial starts.
func (d *App) Initialize() error { return d.sessions.Initialize() }

// Start starts producers after all mixer and desktop subscriptions are registered.
func (d *App) Start() {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	if d.started || d.stopped {
		return
	}
	d.started = true
	d.watcher.Go(d.config.WatchConfigFileChanges)
	d.serial.Start()
	d.obs.Start()
	d.discord.Start()
}

// Wait blocks until a GUI action or process signal requests shutdown.
func (d *App) Wait() { <-d.stopRequest }

// RequestStop is safe for repeated concurrent quit requests.
func (d *App) RequestStop() { d.requestOnce.Do(func() { close(d.stopRequest) }) }

// Stop stops producers while their consumers are still receiving, then releases audio.
func (d *App) Stop() error {
	d.lifecycle.Lock()
	defer d.lifecycle.Unlock()
	if d.stopped {
		return d.stopErr
	}
	d.stopped = true
	d.RequestStop()
	if d.config != nil {
		d.config.StopWatchingConfigFile()
		d.watcher.Wait()
	}
	if d.serial != nil {
		d.serial.Stop()
	}
	if d.obs != nil {
		d.obs.Stop()
	}
	if d.discord != nil {
		d.discord.Stop()
	}
	if d.sessions != nil {
		d.stopErr = d.sessions.Release()
	}
	_ = d.logger.Sync()
	return d.stopErr
}

// Config returns the configuration store.
func (d *App) Config() *config.Store { return d.config }

// Serial returns the slider transport.
func (d *App) Serial() *serial.Client { return d.serial }

// Mixer returns the audio router.
func (d *App) Mixer() *mixer.Mixer { return d.sessions }

// OBS returns the OBS controller.
func (d *App) OBS() *obs.Client { return d.obs }

// Discord returns the Discord controller.
func (d *App) Discord() *discord.Client { return d.discord }

// Localizer returns the resolved notification catalog.
func (d *App) Localizer() *i18n.Localizer { return d.localizer }

// Language returns the resolved language.
func (d *App) Language() string { return d.resolvedLanguage }

// Notifier returns the system notification sink.
func (d *App) Notifier() notify.Notifier { return d.notifier }

// discordVolumeSource reads fresh snapshots whenever Discord restores volumes.
func discordVolumeSource(settings func() *config.Settings, sliderValues func() []float32) func() []discord.TargetVolume {
	return func() []discord.TargetVolume {
		values := sliderValues()
		var result []discord.TargetVolume
		for _, entry := range settings().ActiveMapping() {
			if entry.Slider < 0 || entry.Slider >= len(values) {
				continue
			}
			for _, target := range entry.Targets {
				if key, ok := mixer.DiscordTargetKey(target); ok {
					result = append(result, discord.TargetVolume{Target: key, Percent: values[entry.Slider]})
				}
			}
		}
		return result
	}
}
func (d *App) text(id, other string, data map[string]string) string {
	return d.localizer.MustLocalize(&i18n.LocalizeConfig{DefaultMessage: &i18n.Message{ID: id, Other: other}, TemplateData: data})
}
func (d *App) configOutcome(out config.Outcome) {
	var title, body string
	switch out.Kind {
	case config.Missing:
		title = d.text("ConfigNotFoundTitle", "Can't find configuration!", nil)
		body = d.text("ConfigNotFoundDescription", "{{.FilePath}} must be in the same directory as deej. Please re-launch.", map[string]string{"FilePath": out.Path})
	case config.ReadError:
		title = d.text("ConfigErrorTitle", "Error loading configuration!", nil)
		body = d.text("ConfigErrorDescription", "Please check deej's logs for more details.", nil)
	case config.Invalid:
		title = d.text("ConfigInvalidTitle", "Invalid configuration!", nil)
		body = d.text("ConfigInvalidDescription", "Please make sure {{.FilePath}} is in a valid YAML format.", map[string]string{"FilePath": out.Path})
	case config.Reloaded:
		title = d.text("ConfigReloadTitle", "Configuration reloaded!", nil)
		body = d.text("ConfigReloadDescription", "Your changes have been applied.", nil)
	default:
		return
	}
	d.notifier.Notify(title, body)
}
func (d *App) serialConnected(port string) {
	d.notifier.Notify(d.text("ComPortConnectedNotificationTitle", "Connected to {{.ComPort}}.", map[string]string{"ComPort": port}), d.text("ComPortConnectedNotificationDescription", "Succesfully connected to deej.", nil))
}
func (d *App) serialDisconnected(port string) {
	d.notifier.Notify(d.text("ComPortDisconnectedNotificationTitle", "Disconnected from {{.ComPort}} due to an error.", map[string]string{"ComPort": port}), d.text("ComPortDisconnectedNotificationDescription", "Trying to reconnect.", nil))
}
