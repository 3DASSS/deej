//go:build !headless

package desktop

import (
	"io/fs"
	"log/slog"
	"runtime"

	"strconv"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/nik9play/deej/frontend"
	"github.com/nik9play/deej/internal/icon"
	"github.com/nik9play/deej/internal/platform"
)

const settingsWindowName = "deej-settings"

func getConfigItemText(d *Desktop) (string, string) {
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

func getSettingsItemText(d *Desktop) (string, string) {
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

func getOpenSettingsItemText(d *Desktop) (string, string) {
	openSettingsTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "OpenSettingsTitle",
			Other: "Open settings",
		},
	})
	openSettingsDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "OpenSettingsDescription",
			Other: "Open the settings window",
		},
	})

	return openSettingsTitle, openSettingsDescription
}

func getAutostartItemText(d *Desktop) (string, string) {
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

func getProfilesItemText(d *Desktop) (string, string) {
	profilesTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ProfilesTitle",
			Other: "Profiles",
		},
	})
	profilesDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ProfilesDescription",
			Other: "Switch the active slider mapping",
		},
	})

	return profilesTitle, profilesDescription
}

func getProfileSwitchedText(d *Desktop, name string) (string, string) {
	switchedTitle := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ProfileSwitchedTitle",
			Other: "Profile switched",
		},
	})
	switchedDescription := d.localizer.MustLocalize(&i18n.LocalizeConfig{
		DefaultMessage: &i18n.Message{
			ID:    "ProfileSwitchedDescription",
			Other: "{{.Profile}} is now active.",
		},
		TemplateData: map[string]string{
			"Profile": name,
		},
	})

	return switchedTitle, switchedDescription
}

func getQuitItemText(d *Desktop) (string, string) {
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

func getStatusItemTitle(d *Desktop) string {
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

func getValuesString(d *Desktop) string {
	values := d.serial.CurrentSliderValues()
	strs := make([]string, len(values))
	for i, num := range values {
		strs[i] = strconv.FormatFloat((float64(num)/1023.0)*100, 'f', 0, 32)
	}
	return strings.Join(strs, " | ")
}

func getSessionsCountString(d *Desktop) string {
	count := d.sessions.SessionCount()
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

func (d *Desktop) Run() error {
	logger := d.logger.Named("tray")

	d.shutdownDone = make(chan struct{})
	defer d.cleanupCore()

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
		ShouldQuit:   func() bool { d.requestStop(); return d.allowQuit.Load() },
		PostShutdown: func() { close(d.shutdownDone) },
		LogLevel:     slog.LevelError,
	})
	d.app = app

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

	openSettingsTitle, _ := getOpenSettingsItemText(d)
	settings.Add(openSettingsTitle).OnClick(func(*application.Context) {
		logger.Info("Open settings menu item clicked, opening settings window")

		d.openSettingsWindow()
	})

	configTitle, _ := getConfigItemText(d)
	settings.Add(configTitle).OnClick(func(*application.Context) {
		logger.Info("Edit config menu item clicked, opening config for editing")

		if err := platform.OpenExternal(logger, d.config.Path()); err != nil {
			logger.Warnw("Failed to open config file for editing", "error", err)
		}
	})

	if runtime.GOOS != "linux" {
		autostartTitle, _ := getAutostartItemText(d)
		settings.AddCheckbox(autostartTitle, platform.GetAutostartState()).OnClick(func(ctx *application.Context) {
			if err := platform.SetAutostartState(ctx.ClickedMenuItem().Checked()); err != nil {
				logger.Warnw("Failed to set autostart state", "error", err)
			}
		})
	}

	profilesTitle, _ := getProfilesItemText(d)
	profilesMenu := menu.AddSubmenu(profilesTitle)

	// the profile list follows the config, so the submenu is rebuilt from
	// scratch on every reload. Menu mutations must run on the wails main thread
	rebuildProfilesMenu := func() {
		settings := d.config.Values()

		profilesMenu.Clear()
		for _, profile := range settings.Profiles {
			name := profile.Name

			// a tab right-aligns what follows it in a win32 menu, which is how
			// native menus lay their shortcuts out. SetAccelerator would do the
			// same, but it also reorders the modifiers and binds the shortcut a
			// second time whenever a window has focus. On the linux tray the tab
			// is just whitespace
			label := name
			if profile.Hotkey != "" {
				label += "\t" + profile.Hotkey
			}

			profilesMenu.AddRadio(label, name == settings.ActiveProfile).OnClick(func(*application.Context) {
				logger.Infow("Profile menu item clicked, switching profile", "profile", name)

				if err := d.config.SetActiveProfile(name); err != nil {
					logger.Warnw("Failed to switch profile", "profile", name, "error", err)
				}
			})
		}

		// the tray builds a native menu out of the item tree when it's handed
		// one, so rebuilding the tree isn't enough - the menu has to be set
		// again. SetMenu hops to the main thread itself, so this must not run
		// on it (InvokeSync from the main thread would deadlock)
		tray.SetMenu(menu)
	}
	rebuildProfilesMenu()

	d.syncProfileHotkeys()

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

		d.requestStop()
	})

	tray.SetMenu(menu)

	tray.OnDoubleClick(func() {
		d.openSettingsWindow()
	})

	d.subscribeEvents(statusInfo, sessionsInfo, setTooltip, setValuesInfo, rebuildProfilesMenu)

	// start the tray icon
	logger.Debug("Running in tray")

	return app.Run()
}

// openSettingsWindow creates a fresh settings window, or focuses the existing
// one if it's already open. The window is fully destroyed when closed
func (d *Desktop) openSettingsWindow() {
	d.settingsLock.Lock()
	defer d.settingsLock.Unlock()

	if win, ok := d.app.Window.GetByName(settingsWindowName); ok {
		win.Restore()
		win.Focus()
		return
	}

	settingsTitle, _ := getSettingsItemText(d)

	d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      settingsWindowName,
		Title:     "deej - " + settingsTitle,
		Width:     880,
		Height:    620,
		MinWidth:  800,
		MinHeight: 600,
		Frameless: true,
		URL:       "/",
	})
}
