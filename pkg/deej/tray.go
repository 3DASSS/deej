package deej

import (
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/nik9play/deej/frontend"
	"github.com/nik9play/deej/pkg/deej/util"
	"github.com/nik9play/deej/pkg/icon"
)

// trayState holds the wails application that powers the tray icon and settings window
type trayState struct {
	app          *application.App
	shutdownDone chan struct{}
}

func getConfigItemText(d *Deej) (string, string) {
	configTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "EditConfigTitle",
			Other: "Edit configuration",
		},
	})
	configDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "EditConfigDescription",
			Other: "Open config file with notepad",
		},
	})

	return configTitle, configDescription
}

func getSettingsItemText(d *Deej) (string, string) {
	configTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "SettingsTitle",
			Other: "Settings",
		},
	})
	configDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "SettingsDescription",
			Other: "Settings",
		},
	})

	return configTitle, configDescription
}

func getAutostartItemText(d *Deej) (string, string) {
	configTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "AutostartTitle",
			Other: "Run at startup",
		},
	})
	configDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "AutostartDescription",
			Other: "deej will launch at startup",
		},
	})

	return configTitle, configDescription
}

func getQuitItemText(d *Deej) (string, string) {
	quitTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "QuitTitle",
			Other: "Quit",
		},
	})
	quitDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "QuitDescription",
			Other: "Stop deej and quit",
		},
	})

	return quitTitle, quitDescription
}

func getStatusItemTitle(d *Deej) string {
	var title string

	if d.serial.GetState() {
		title = d.localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "StatusTrueTitle",
				Other: "Connected to {{.ComPort}}",
			},
			TemplateData: map[string]string{
				"ComPort": d.serial.CurrentComPort(),
			},
		})
	} else {
		title = d.localizer.MustLocalize(&i18n.LocalizeConfig{
			DefaultMessage: &i18n.Message{
				ID:    "StatusFalseTitle",
				Other: "Waiting for device...",
			},
		})
	}

	return title
}

func getValuesString(d *Deej) string {
	values := d.serial.CurrentSliderValues()
	strs := make([]string, len(values))
	for i, num := range values {
		strs[i] = strconv.FormatFloat((float64(num)/1023.0)*100, 'f', 0, 32)
	}
	return strings.Join(strs, " | ")
}

func getSessionsCountString(d *Deej) string {
	count := d.sessions.getSessionCount()
	return d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "AudioSessionsCount",
			One:   "{{.Count}} audio session",
			Other: "{{.Count}} audio sessions",
		},
		TemplateData: map[string]interface{}{
			"Count": count,
		},
		PluralCount: count,
	})
}

func (d *Deej) initializeTray(onDone func()) {
	logger := d.logger.Named("tray")

	d.tray.shutdownDone = make(chan struct{})

	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		logger.Errorw("Failed to open frontend assets", "error", err)
	}

	app := application.New(application.Options{
		Name: "deej",
		Icon: icon.TrayDeejLogo,
		// deej sets up its own interrupt handler
		DisableDefaultSignalHandler: true,
		// keep running with zero open windows; the tray is the app
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
		Assets:  application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Services: []application.Service{
			application.NewService(newSettingsService(d)),
		},
		PostShutdown: func() {
			close(d.tray.shutdownDone)
		},
		LogLevel: slog.LevelError,
	})
	d.tray.app = app

	tray := app.SystemTray.New()
	tray.SetIcon(icon.TrayDeejLogo)
	tray.SetTooltip("deej")

	setTooltip := func() {
		title := "deej\n" + getStatusItemTitle(d)
		if d.serial.GetState() {
			title += "\n" + getValuesString(d)
		}
		tray.SetTooltip(title)
	}

	menu := app.NewMenu()

	settingsTitle, _ := getSettingsItemText(d)
	settings := menu.AddSubmenu(settingsTitle)

	configTitle, _ := getConfigItemText(d)
	settings.Add(configTitle).OnClick(func(*application.Context) {
		logger.Info("Edit config menu item clicked, opening config for editing")

		if err := util.OpenExternal(logger, d.config.configPath); err != nil {
			logger.Warnw("Failed to open config file for editing", "error", err)
		}
	})

	if !util.Linux() {
		autostartTitle, _ := getAutostartItemText(d)
		settings.AddCheckbox(autostartTitle, util.GetAutostartState()).OnClick(func(ctx *application.Context) {
			if err := util.SetAutostartState(ctx.ClickedMenuItem().Checked()); err != nil {
				logger.Warnw("Failed to set autostart state", "error", err)
			}
		})
	}

	menu.AddSeparator()

	statusInfo := menu.Add(getStatusItemTitle(d)).SetEnabled(false)

	valuesInfo := menu.Add("...").SetEnabled(false).SetHidden(true)

	setValuesInfo := func() {
		if d.serial.GetState() {
			valuesInfo.SetLabel(getValuesString(d))
			valuesInfo.SetHidden(false)
		} else {
			valuesInfo.SetHidden(true)
		}
	}

	sessionsInfo := menu.Add(getSessionsCountString(d)).SetEnabled(false)

	if d.version != "" {
		menu.Add(d.version).SetEnabled(false)
	}

	menu.AddSeparator()

	quitTitle, _ := getQuitItemText(d)
	menu.Add(quitTitle).OnClick(func(*application.Context) {
		logger.Info("Quit menu item clicked, stopping")

		d.signalStop()
	})

	tray.SetMenu(menu)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		logger.Debug("Tray instance ready")

		setTooltip()

		sliderMovedChannel := d.serial.SubscribeToSliderMoveEvents()
		stateChangeChannel := d.serial.SubscribeToStateChangeEvent()
		sessionCountChangeChannel := d.sessions.SubscribeToSessionCountChange()

		// wait on things to happen; menu item mutations must run on the wails main thread
		go func() {
			for {
				select {
				// slider moved
				case <-sliderMovedChannel:
					setTooltip()
					application.InvokeAsync(setValuesInfo)

				// connection state changed
				case <-stateChangeChannel:
					setTooltip()
					application.InvokeAsync(func() {
						setValuesInfo()
						statusInfo.SetLabel(getStatusItemTitle(d))
					})

				// session count changed
				case <-sessionCountChangeChannel:
					application.InvokeAsync(func() {
						sessionsInfo.SetLabel(getSessionsCountString(d))
					})
				}
			}
		}()

		// actually start the main runtime
		go onDone()
	})

	// start the tray icon
	logger.Debug("Running in tray")
	if err := app.Run(); err != nil {
		logger.Errorw("Wails application exited with error", "error", err)
	}
}

func (d *Deej) stopTray() {
	if d.tray.app == nil {
		return
	}

	d.logger.Debug("Quitting tray")
	d.tray.app.Quit()

	// wait for wails to tear down the tray icon and any open windows before
	// run() exits the process, to avoid leaving a ghost tray icon behind
	select {
	case <-d.tray.shutdownDone:
	case <-time.After(5 * time.Second):
		d.logger.Warn("Timed out waiting for tray shutdown")
	}
}
