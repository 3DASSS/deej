package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	"go.yaml.in/yaml/v3"

	"github.com/nik9play/deej/internal/atomicfile"
)

// Store provides application-wide access to configuration fields,
// as well as loading/file watching logic for deej's configuration file
type Store struct {
	current atomic.Pointer[Settings]

	logger    *zap.SugaredLogger
	onOutcome func(Outcome)

	stopWatcher chan struct{}
	stopOnce    sync.Once

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
func (cc *Store) Values() *Settings {
	return cc.current.Load()
}

// New creates a configuration store. Notifications are reported through onOutcome.
func New(logger *zap.SugaredLogger, configPath string, onOutcome func(Outcome)) (*Store, error) {
	logger = logger.Named("config")

	ex, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("get executable dir: %w", err)
	}

	// set config path to exe dir, if custom path is not provided
	if configPath == "" {
		configPath = filepath.Join(filepath.Dir(ex), "config.yaml")
	}

	cc := &Store{
		logger:          logger,
		onOutcome:       onOutcome,
		reloadConsumers: []chan bool{},
		stopWatcher:     make(chan struct{}),
		configPath:      configPath,
	}

	logger.Debug("Created config instance")

	return cc, nil
}

// Load reads deej's config file from disk and tries to parse it
func (cc *Store) Load() error {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	return cc.loadLocked()
}

func (cc *Store) loadLocked() error {
	cc.logger.Debugw("Loading config", "path", cc.configPath)

	// Bootstrap a new installation, but never reset a running configuration
	// when an editor temporarily removes its file during replacement.
	if info, err := os.Stat(cc.configPath); errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
		if errors.Is(err, os.ErrNotExist) && cc.current.Load() == nil {
			settings := defaultSettings()
			settings.normalize()
			if err := cc.writeSettings(settings); err != nil {
				return fmt.Errorf("create default config: %w", err)
			}
		} else {
			cc.logger.Warnw("Config file not found", "path", cc.configPath)
			cc.report(Missing, nil)
			return fmt.Errorf("config file doesn't exist: %s", cc.configPath)
		}
	}

	data, err := os.ReadFile(cc.configPath)
	if err != nil {
		cc.logger.Warnw("Failed to read user config", "error", err)

		cc.report(ReadError, err)

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

			cc.report(Invalid, err)

			return fmt.Errorf("parse user config: %w", err)
		}
	}

	if problems := settings.normalize(); len(problems) > 0 {
		cc.logger.Warnw("Config had invalid values, replaced with defaults", "problems", problems)
	}

	cc.current.Store(&settings)

	cc.logger.Info("Loaded config successfully")
	cc.logger.Infow("Config values",
		"activeProfile", settings.ActiveProfile,
		"profiles", len(settings.Profiles),
		"sliderMapping", settings.ActiveMapping(),
		"comPort", settings.COM.Port,
		"baudRate", settings.COM.BaudRate,
		"invertSliders", settings.InvertSliders)

	return nil
}

// SubscribeToChanges returns a channel that receives a signal whenever the
// config is (re)applied. Signals are coalesced - consumers should re-read
// Values() rather than count events
func (cc *Store) SubscribeToChanges() chan bool {
	cc.consumersLock.Lock()
	defer cc.consumersLock.Unlock()

	c := make(chan bool, 1)
	cc.reloadConsumers = append(cc.reloadConsumers, c)

	return c
}

// UserSettings returns the current contents of the user config file
func (cc *Store) UserSettings() Settings {
	return cc.Values().clone()
}

// SetActiveProfile switches to the named profile and persists the choice, so
// it survives a restart. It's the one-field write behind the profile hotkeys,
// the tray menu and the titlebar dropdown; everything else goes through
// SaveUserSettings
func (cc *Store) SetActiveProfile(name string) error {
	cc.lock.Lock()
	defer cc.lock.Unlock()
	settings := cc.Values().clone()

	resolved, ok := findProfileName(settings.Profiles, name)
	if !ok {
		return fmt.Errorf("unknown profile: %q", name)
	}

	// a hotkey can be pressed repeatedly; don't rewrite the config file (and
	// wake every consumer) when nothing would change
	if settings.ActiveProfile == resolved {
		return nil
	}

	settings.ActiveProfile = resolved

	if err := cc.saveAndReload(settings); err != nil {
		return err
	}

	cc.onConfigReloaded()

	return nil
}

// SaveUserSettings validates the settings, rewrites the user config file on
// disk and applies the new config immediately. The file is fully regenerated:
// comments, key order and unknown keys are not preserved
func (cc *Store) SaveUserSettings(settings Settings) error {
	settings = settings.clone()
	// normalize both canonicalizes (blank VID/PID -> defaults, mapping sorted
	// and filtered) and reports invalid values; a GUI save must be rejected
	// rather than silently corrected
	if problems := settings.normalize(); len(problems) > 0 {
		return fmt.Errorf("invalid settings: %s", strings.Join(problems, "; "))
	}

	cc.lock.Lock()
	defer cc.lock.Unlock()
	if err := cc.saveAndReload(settings); err != nil {
		return err
	}

	cc.onConfigReloaded()

	return nil
}

func (cc *Store) saveAndReload(settings Settings) error {
	if err := cc.writeSettings(settings); err != nil {
		return err
	}

	// apply immediately instead of relying on the watcher's debounce timing
	if err := cc.loadLocked(); err != nil {
		return fmt.Errorf("load config after save: %w", err)
	}

	return nil
}

func (cc *Store) writeSettings(settings Settings) error {
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

	if err := atomicfile.Write(cc.configPath, out); err != nil {
		cc.logger.Warnw("Failed to write config file", "error", err)
		return fmt.Errorf("write config for save: %w", err)
	}

	cc.logger.Infow("Saved user settings to config file", "path", cc.configPath)

	return nil
}

// Path returns the configuration file path, resolved relative to the executable by default.
func (cc *Store) Path() string { return cc.configPath }

func (cc *Store) report(kind OutcomeKind, err error) {
	if cc.onOutcome != nil {
		cc.onOutcome(Outcome{Kind: kind, Path: cc.configPath, Err: err})
	}
}

// OutcomeKind identifies a configuration load result requiring user feedback.
type OutcomeKind int

const (
	Missing OutcomeKind = iota
	ReadError
	Invalid
	Reloaded
)

// Outcome describes loading feedback without depending on desktop or localization.
type Outcome struct {
	Kind OutcomeKind
	Path string
	Err  error
}
