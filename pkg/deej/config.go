package deej

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/nik9play/deej/pkg/deej/util"
	"github.com/nik9play/deej/pkg/notify"
)

type VIDPID struct {
	VID uint64
	PID uint64
}

// ConnectionInfo describes the serial connection parameters
type ConnectionInfo struct {
	COMPort  string
	BaudRate int
}

// OBSConfig describes the OBS websocket connection parameters
type OBSConfig struct {
	Enabled  bool
	Host     string
	Port     int
	Password string
}

// ConfigValues holds a single immutable generation of deej's configuration.
// A fresh instance is published atomically on every (re)load, so concurrent
// readers must grab a snapshot with Values and must not mutate it
type ConfigValues struct {
	SliderMapping *sliderMap

	ConnectionInfo ConnectionInfo

	InvertSliders bool

	NoiseReductionLevel string

	Language string

	AutoSearchVIDPID VIDPID

	OBSConfig OBSConfig
}

// CanonicalConfig provides application-wide access to configuration fields,
// as well as loading/file watching logic for deej's configuration file
type CanonicalConfig struct {
	current atomic.Pointer[ConfigValues]

	logger             *zap.SugaredLogger
	notifier           notify.Notifier
	stopWatcherChannel chan bool
	watcherStopped     atomic.Bool

	reloadConsumers []chan bool

	userConfig     *viper.Viper
	internalConfig *viper.Viper

	// guards viper access between the file watcher and the settings GUI
	viperLock sync.Mutex

	// unix-nano timestamp of the last GUI-initiated config write, used to
	// suppress the file watcher event caused by our own save
	lastSelfWrite atomic.Int64

	configPath string
}

// Values returns the current immutable snapshot of the configuration.
// Callers that read multiple fields should grab one snapshot and use it
// throughout, so all values belong to the same config generation
func (cc *CanonicalConfig) Values() *ConfigValues {
	return cc.current.Load()
}

const (
	internalConfigName = "preferences"

	configType = "yaml"

	configKeySliderMapping       = "slider_mapping"
	configKeyInvertSliders       = "invert_sliders"
	configKeyCOMPort             = "com_port"
	configKeyBaudRate            = "baud_rate"
	configKeyNoiseReductionLevel = "noise_reduction"
	configKeyLanguage            = "language"
	configKeyComVID              = "com_vid"
	configKeyComPID              = "com_pid"
	configKeyOBSEnabled          = "obs.enabled"
	configKeyOBSHost             = "obs.host"
	configKeyOBSPort             = "obs.port"
	configKeyOBSPassword         = "obs.password"

	defaultCOMPort  = "COM4"
	defaultBaudRate = 9600
	defaultLanguage = "auto"

	// ch340 chip
	defaultVID uint64 = 0x1A86
	defaultPID uint64 = 0x7523

	defaultOBSEnabled  = false
	defaultOBSHost     = "localhost"
	defaultOBSPort     = 4455
	defaultOBSPassword = ""
)

// has to be defined as a non-constant because we're using path.Join

var defaultSliderMapping = func() *sliderMap {
	emptyMap := newSliderMap()
	emptyMap.set(0, []string{masterSessionName})

	return emptyMap
}()

// NewConfig creates a config instance for the deej object and sets up viper instances for deej's config files
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

	userConfigName := filepath.Base(configPath)
	configDir := filepath.Dir(configPath)
	internalConfigDir := filepath.Join(filepath.Dir(ex), "logs")

	cc := &CanonicalConfig{
		logger:             logger,
		notifier:           notifier,
		reloadConsumers:    []chan bool{},
		stopWatcherChannel: make(chan bool),
		configPath:         configPath,
	}

	// distinguish between the user-provided config (config.yaml) and the internal config (logs/preferences.yaml)
	userConfig := viper.New()
	userConfig.SetConfigName(userConfigName)
	userConfig.SetConfigType(configType)
	userConfig.AddConfigPath(configDir)

	userConfig.SetDefault(configKeySliderMapping, map[string][]string{})
	userConfig.SetDefault(configKeyInvertSliders, false)
	userConfig.SetDefault(configKeyCOMPort, defaultCOMPort)
	userConfig.SetDefault(configKeyBaudRate, defaultBaudRate)
	userConfig.SetDefault(configKeyLanguage, defaultLanguage)
	userConfig.SetDefault(configKeyComVID, defaultVID)
	userConfig.SetDefault(configKeyComPID, defaultPID)
	userConfig.SetDefault(configKeyOBSEnabled, defaultOBSEnabled)
	userConfig.SetDefault(configKeyOBSHost, defaultOBSHost)
	userConfig.SetDefault(configKeyOBSPort, defaultOBSPort)
	userConfig.SetDefault(configKeyOBSPassword, defaultOBSPassword)

	internalConfig := viper.New()
	internalConfig.SetConfigName(internalConfigName)
	internalConfig.SetConfigType(configType)
	internalConfig.AddConfigPath(internalConfigDir)

	cc.userConfig = userConfig
	cc.internalConfig = internalConfig

	logger.Debug("Created config instance")

	return cc, nil
}

// Load reads deej's config files from disk and tries to parse them
func (cc *CanonicalConfig) Load(localizer *i18n.Localizer) error {
	cc.viperLock.Lock()
	defer cc.viperLock.Unlock()

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

	// load the user config
	if err := cc.userConfig.ReadInConfig(); err != nil {
		cc.logger.Warnw("Viper failed to read user config", "error", err)

		// if the error is yaml-format-related, show a sensible error. otherwise, show 'em to the logs
		if strings.Contains(err.Error(), "yaml:") {
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
		} else {
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
		}

		return fmt.Errorf("read user config: %w", err)
	}

	// load the internal config - this doesn't have to exist, so it can error
	if err := cc.internalConfig.ReadInConfig(); err != nil {
		cc.logger.Debugw("Viper failed to read internal config", "error", err, "reminder", "this is fine")
	}

	// canonize the configuration with viper's helpers
	if err := cc.populateFromVipers(); err != nil {
		cc.logger.Warnw("Failed to populate config fields", "error", err)
		return fmt.Errorf("populate config fields: %w", err)
	}

	values := cc.Values()
	cc.logger.Info("Loaded config successfully")
	cc.logger.Infow("Config values",
		"sliderMapping", values.SliderMapping,
		"connectionInfo", values.ConnectionInfo,
		"invertSliders", values.InvertSliders)

	return nil
}

// SubscribeToChanges allows external components to receive updates when the config is reloaded
func (cc *CanonicalConfig) SubscribeToChanges() chan bool {
	c := make(chan bool)
	cc.reloadConsumers = append(cc.reloadConsumers, c)

	return c
}

// WatchConfigFileChanges starts watching for configuration file changes
// and attempts reloading the config when they happen
func (cc *CanonicalConfig) WatchConfigFileChanges(localizer *i18n.Localizer) {
	cc.logger.Debugw("Starting to watch user config file for changes", "path", cc.configPath)

	const (
		minTimeBetweenReloadAttempts = time.Millisecond * 500
		delayBetweenEventAndReload   = time.Millisecond * 50
	)

	lastAttemptedReload := time.Now()

	// establish watch using viper as opposed to doing it ourselves, though our internal cooldown is still required.
	// the callback must be registered before WatchConfig starts viper's watch
	// goroutine, since viper stores it in an unsynchronized field
	cc.userConfig.OnConfigChange(func(event fsnotify.Event) {

		// viper offers no way to unregister the callback safely, so once we're
		// stopped just ignore any further events
		if cc.watcherStopped.Load() {
			return
		}

		// ignore events caused by a GUI save; it already loaded and applied
		// the new config synchronously, and shows its own confirmation
		if time.Since(time.Unix(0, cc.lastSelfWrite.Load())) < selfWriteSuppressWindow {
			cc.logger.Debug("Ignoring config file event caused by GUI save")
			return
		}

		// when we get a write event...
		if event.Op&fsnotify.Write == fsnotify.Write {

			now := time.Now()

			// ... check if it's not a duplicate (many editors will write to a file twice)
			if lastAttemptedReload.Add(minTimeBetweenReloadAttempts).Before(now) {

				// and attempt reload if appropriate
				cc.logger.Debugw("Config file modified, attempting reload", "event", event)

				// wait a bit to let the editor actually flush the new file contents to disk
				time.Sleep(delayBetweenEventAndReload)

				if err := cc.Load(localizer); err != nil {
					cc.logger.Warnw("Failed to reload config file", "error", err)
				} else {
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

				// don't forget to update the time
				lastAttemptedReload = now
			}
		}
	})
	cc.userConfig.WatchConfig()

	// wait till they stop us
	<-cc.stopWatcherChannel
	cc.logger.Debug("Stopping user config file watcher")
}

// StopWatchingConfigFile signals our filesystem watcher to stop
func (cc *CanonicalConfig) StopWatchingConfigFile() {
	cc.watcherStopped.Store(true)
	cc.stopWatcherChannel <- true
}

func (cc *CanonicalConfig) populateFromVipers() error {

	values := &ConfigValues{}

	// merge the slider mappings from the user and internal configs
	values.SliderMapping = sliderMapFromConfigs(
		cc.userConfig.GetStringMapStringSlice(configKeySliderMapping),
		cc.internalConfig.GetStringMapStringSlice(configKeySliderMapping),
	)

	// get the rest of the config fields - viper saves us a lot of effort here
	values.ConnectionInfo.COMPort = cc.userConfig.GetString(configKeyCOMPort)

	values.ConnectionInfo.BaudRate = cc.userConfig.GetInt(configKeyBaudRate)
	if values.ConnectionInfo.BaudRate <= 0 {
		cc.logger.Warnw("Invalid baud rate specified, using default value",
			"key", configKeyBaudRate,
			"invalidValue", values.ConnectionInfo.BaudRate,
			"defaultValue", defaultBaudRate)

		values.ConnectionInfo.BaudRate = defaultBaudRate
	}

	values.InvertSliders = cc.userConfig.GetBool(configKeyInvertSliders)
	values.NoiseReductionLevel = cc.userConfig.GetString(configKeyNoiseReductionLevel)
	values.Language = cc.userConfig.GetString(configKeyLanguage)

	userConfigVID := cc.userConfig.GetUint64(configKeyComVID)
	userConfigPID := cc.userConfig.GetUint64(configKeyComPID)

	values.AutoSearchVIDPID = VIDPID{VID: userConfigVID, PID: userConfigPID}

	values.OBSConfig.Enabled = cc.userConfig.GetBool(configKeyOBSEnabled)
	values.OBSConfig.Host = cc.userConfig.GetString(configKeyOBSHost)
	values.OBSConfig.Port = cc.userConfig.GetInt(configKeyOBSPort)
	values.OBSConfig.Password = cc.userConfig.GetString(configKeyOBSPassword)

	cc.current.Store(values)

	cc.logger.Debugw("AutoSearchVIDPID", "val", values.AutoSearchVIDPID)
	cc.logger.Debugw("OBSConfig", "enabled", values.OBSConfig.Enabled, "host", values.OBSConfig.Host, "port", values.OBSConfig.Port)
	cc.logger.Debugw("Populated config fields from vipers")

	return nil
}

func (cc *CanonicalConfig) onConfigReloaded() {
	cc.logger.Debug("Notifying consumers about configuration reload")

	for _, consumer := range cc.reloadConsumers {
		consumer <- true
	}
}
