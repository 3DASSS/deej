package deej

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"go.uber.org/zap"
	"go.yaml.in/yaml/v3"

	"github.com/nik9play/deej/pkg/deej/util"
	"github.com/nik9play/deej/pkg/notify"
)

// ConfigValues holds a single immutable generation of deej's configuration.
// A fresh instance is published atomically on every (re)load, so concurrent
// readers must grab a snapshot with Values and must not mutate it
type ConfigValues struct {
	// Settings is the sanitized contents of the user config file
	Settings

	// SliderMapping is the runtime slider->targets map built from Mapping
	SliderMapping *sliderMap
}

// CanonicalConfig provides application-wide access to configuration fields,
// as well as loading/file watching logic for deej's configuration file
type CanonicalConfig struct {
	current atomic.Pointer[ConfigValues]

	logger   *zap.SugaredLogger
	notifier notify.Notifier

	stopWatcher chan struct{}

	consumersLock   sync.Mutex
	reloadConsumers []chan bool

	// serializes the read-modify-write cycles of Load and SaveUserSettings
	lock sync.Mutex

	// hash of the last config content written by the GUI, so the file
	// watcher can tell our own writes apart from hand edits
	lastSelfWrite atomic.Value // string

	configPath string
}

// Values returns the current immutable snapshot of the configuration.
// Callers that read multiple fields should grab one snapshot and use it
// throughout, so all values belong to the same config generation
func (cc *CanonicalConfig) Values() *ConfigValues {
	return cc.current.Load()
}

// how long after the last file event to wait before reloading, so editors
// that write multiple times (or write partial content) settle first
const watchDebounceDelay = 250 * time.Millisecond

// NewConfig creates a config instance for the deej object
func NewConfig(logger *zap.SugaredLogger, notifier notify.Notifier, configPath string) (*CanonicalConfig, error) {
	logger = logger.Named("config")

	ex, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable dir: %w", err)
	}

	// set config path to exe dir, if custom path is not provided
	if configPath == "" {
		configPath = filepath.Join(filepath.Dir(ex), "config.yaml")
	}

	cc := &CanonicalConfig{
		logger:          logger,
		notifier:        notifier,
		reloadConsumers: []chan bool{},
		stopWatcher:     make(chan struct{}),
		configPath:      configPath,
	}

	logger.Debug("Created config instance")

	return cc, nil
}

// Load reads deej's config file from disk and tries to parse it
func (cc *CanonicalConfig) Load(localizer *i18n.Localizer) error {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	return cc.loadLocked(localizer)
}

func (cc *CanonicalConfig) loadLocked(localizer *i18n.Localizer) error {
	cc.logger.Debugw("Loading config", "path", cc.configPath)

	// make sure it exists
	if !util.FileExists(cc.configPath) {
		cc.logger.Warnw("Config file not found", "path", cc.configPath)

		configNotFoundTitle := localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "ConfigNotFoundTitle",
				Other: "Can't find configuration!",
			},
		})
		configNotFoundDescription := localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "ConfigNotFoundDescription",
				Other: "{{.FilePath}} must be in the same directory as deej. Please re-launch.",
			},
			TemplateData: map[string]string{
				"FilePath": cc.configPath,
			},
		})
		cc.notifier.Notify(configNotFoundTitle, configNotFoundDescription)

		return fmt.Errorf("config file doesn't exist: %s", cc.configPath)
	}

	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		cc.logger.Warnw("Failed to read user config", "error", err)

		configErrorTitle := localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "ConfigErrorTitle",
				Other: "Error loading configuration!",
			},
		})
		configErrorDescription := localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "ConfigErrorDescription",
				Other: "Please check deej's logs for more details.",
			},
		})
		cc.notifier.Notify(configErrorTitle, configErrorDescription)

		return fmt.Errorf("read user config: %w", err)
	}

	// missing keys keep the defaults they were initialized with
	settings := defaultSettings()
	if err := yaml.Unmarshal(data, &settings); err != nil {

		// a *yaml.TypeError means the file parsed, but some values have the
		// wrong type; those fields keep their defaults, so we can keep going
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			cc.logger.Warnw("Config file has values of unexpected types, using defaults for them", "error", err)
		} else {
			cc.logger.Warnw("Failed to parse user config", "error", err)

			configInvalidTitle := localizer.MustLocalize(&i18n.LocalizeConfig{
				DefaultMessage: &i18n.Message{
					ID:    "ConfigInvalidTitle",
					Other: "Invalid configuration!",
				},
			})
			configInvalidDescription := localizer.MustLocalize(&i18n.LocalizeConfig{
				DefaultMessage: &i18n.Message{
					ID:    "ConfigInvalidDescription",
					Other: "Please make sure {{.FilePath}} is in a valid YAML format.",
				},
				TemplateData: map[string]string{
					"FilePath": cc.configPath,
				},
			})
			cc.notifier.Notify(configInvalidTitle, configInvalidDescription)

			return fmt.Errorf("parse user config: %w", err)
		}
	}

	settings.sanitize(cc.logger)

	values := &ConfigValues{
		Settings:      settings,
		SliderMapping: sliderMapFromSettings(settings.Mapping),
	}
	cc.current.Store(values)

	cc.logger.Info("Loaded config successfully")
	cc.logger.Infow("Config values",
		"sliderMapping", values.SliderMapping,
		"comPort", values.COM.Port,
		"baudRate", values.COM.BaudRate,
		"invertSliders", values.InvertSliders)

	return nil
}

// SubscribeToChanges returns a channel that receives a signal whenever the
// config is (re)applied. Signals are coalesced - consumers should re-read
// Values() rather than count events
func (cc *CanonicalConfig) SubscribeToChanges() chan bool {
	cc.consumersLock.Lock()
	defer cc.consumersLock.Unlock()

	c := make(chan bool, 1)
	cc.reloadConsumers = append(cc.reloadConsumers, c)

	return c
}

// WatchConfigFileChanges starts watching for configuration file changes
// and attempts reloading the config when they happen
func (cc *CanonicalConfig) WatchConfigFileChanges(localizer *i18n.Localizer) {
	cc.logger.Debugw("Starting to watch user config file for changes", "path", cc.configPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		cc.logger.Warnw("Failed to create filesystem watcher", "error", err)
		return
	}
	defer watcher.Close()

	// watch the directory rather than the file itself, so atomic
	// (write-temp-then-rename) saves and editors that replace the file
	// don't break the watch
	if err := watcher.Add(filepath.Dir(cc.configPath)); err != nil {
		cc.logger.Warnw("Failed to watch config directory", "error", err)
		return
	}

	// trailing-edge debounce timer, armed on every relevant event
	debounce := time.NewTimer(time.Hour)
	if !debounce.Stop() {
		<-debounce.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if !strings.EqualFold(filepath.Clean(event.Name), filepath.Clean(cc.configPath)) {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(watchDebounceDelay)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			cc.logger.Warnw("Config file watcher error", "error", err)

		case <-debounce.C:
			cc.handleConfigFileChange(localizer)

		case <-cc.stopWatcher:
			cc.logger.Debug("Stopping user config file watcher")
			return
		}
	}
}

func (cc *CanonicalConfig) handleConfigFileChange(localizer *i18n.Localizer) {

	// ignore events caused by a GUI save: it already loaded and applied the
	// new config synchronously, and shows its own confirmation
	if data, err := os.ReadFile(cc.configPath); err == nil {
		if hash, ok := cc.lastSelfWrite.Load().(string); ok && hash == contentHash(data) {
			cc.logger.Debug("Ignoring config file event caused by GUI save")
			return
		}
	}

	cc.logger.Debug("Config file modified, attempting reload")

	if err := cc.Load(localizer); err != nil {
		cc.logger.Warnw("Failed to reload config file", "error", err)
		return
	}

	cc.logger.Info("Reloaded config successfully")

	configReloadTitle := localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ConfigReloadTitle",
			Other: "Configuration reloaded!",
		},
	})
	configReloadDescription := localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ConfigReloadDescription",
			Other: "Your changes have been applied.",
		},
	})
	cc.notifier.Notify(configReloadTitle, configReloadDescription)

	cc.onConfigReloaded()
}

// StopWatchingConfigFile signals our filesystem watcher to stop
func (cc *CanonicalConfig) StopWatchingConfigFile() {
	close(cc.stopWatcher)
}

func contentHash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (cc *CanonicalConfig) onConfigReloaded() {
	cc.logger.Debug("Notifying consumers about configuration reload")

	cc.consumersLock.Lock()
	defer cc.consumersLock.Unlock()

	for _, consumer := range cc.reloadConsumers {
		// non-blocking send: a signal already pending in the buffer tells the
		// consumer everything it needs (re-read Values), so never block on it
		select {
		case consumer <- true:
		default:
		}
	}
}
