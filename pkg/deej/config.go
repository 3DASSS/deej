package deej

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// CanonicalConfig provides application-wide access to configuration fields,
// as well as loading/file watching logic for deej's configuration file
type CanonicalConfig struct {
	current atomic.Pointer[Settings]

	logger   *zap.SugaredLogger
	notifier notify.Notifier

	stopWatcher chan struct{}

	consumersLock   sync.Mutex
	reloadConsumers []chan bool

	// serializes the read-modify-write cycles of Load and SaveUserSettings
	lock sync.Mutex

	configPath string
}

// Values returns the current immutable snapshot of the configuration.
// Callers that read multiple fields should grab one snapshot and use it
// throughout, so all values belong to the same config generation. The
// returned snapshot must not be mutated
func (cc *CanonicalConfig) Values() *Settings {
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

	// missing keys keep the defaults they were initialized with. Legacy flat
	// com keys are applied first so an explicit com: section overrides them
	settings := defaultSettings()
	applyLegacyKeys(data, &settings)
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

	if problems := settings.normalize(); len(problems) > 0 {
		cc.logger.Warnw("Config had invalid values, replaced with defaults", "problems", problems)
	}

	cc.current.Store(&settings)

	cc.logger.Info("Loaded config successfully")
	cc.logger.Infow("Config values",
		"sliderMapping", settings.SliderMapping,
		"comPort", settings.COM.Port,
		"baudRate", settings.COM.BaudRate,
		"invertSliders", settings.InvertSliders)

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

	// trailing-edge debounce timer, armed on every relevant event. Go 1.23+
	// timer channels are unbuffered, so Stop/Reset never leave a stale tick
	// behind and the usual drain dance isn't needed
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()

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
	cc.logger.Debug("Config file modified, attempting reload")

	previous := cc.current.Load()

	if err := cc.Load(localizer); err != nil {
		cc.logger.Warnw("Failed to reload config file", "error", err)
		return
	}

	// a GUI save already loaded, applied and notified synchronously; the file
	// event it triggers just reloads identical content. Skip the redundant
	// toast and consumer notification whenever the config didn't actually
	// change - this also covers a hand edit that only touched comments or
	// whitespace
	if previous != nil && reflect.DeepEqual(previous, cc.current.Load()) {
		cc.logger.Debug("Config unchanged after reload, skipping notification")
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

// UserSettings returns the current contents of the user config file
func (cc *CanonicalConfig) UserSettings() Settings {
	settings := *cc.Values()
	settings.SliderMapping = settings.SliderMapping.clone()

	return settings
}

// SaveUserSettings validates the settings, rewrites the user config file on
// disk and applies the new config immediately. The file is fully regenerated:
// comments, key order and unknown keys are not preserved
func (cc *CanonicalConfig) SaveUserSettings(settings Settings, localizer *i18n.Localizer) error {
	// normalize both canonicalizes (blank VID/PID -> defaults, mapping sorted
	// and filtered) and reports invalid values; a GUI save must be rejected
	// rather than silently corrected
	if problems := settings.normalize(); len(problems) > 0 {
		return fmt.Errorf("invalid settings: %s", strings.Join(problems, "; "))
	}

	if err := cc.saveAndReload(settings, localizer); err != nil {
		return err
	}

	cc.onConfigReloaded()

	return nil
}

func (cc *CanonicalConfig) saveAndReload(settings Settings, localizer *i18n.Localizer) error {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&settings); err != nil {
		return fmt.Errorf("marshal config for save: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("marshal config for save: %w", err)
	}
	out := buf.Bytes()

	if err := writeFileAtomic(cc.configPath, out); err != nil {
		cc.logger.Warnw("Failed to write config file", "error", err)
		return fmt.Errorf("write config for save: %w", err)
	}

	cc.logger.Infow("Saved user settings to config file", "path", cc.configPath)

	// apply immediately instead of relying on the watcher's debounce timing
	if err := cc.loadLocked(localizer); err != nil {
		return fmt.Errorf("load config after save: %w", err)
	}

	return nil
}

// writeFileAtomic writes data to a temp file in the target's directory and
// renames it over the target, so a crash mid-write can't corrupt the config.
// The config holds the OBS websocket password, so it stays owner-only: the
// mode comes from os.CreateTemp, which creates at 0600, and rename preserves
// it - don't widen it
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}
