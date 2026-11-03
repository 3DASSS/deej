//go:build windows && !headless

package desktop

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/nik9play/deej/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.uber.org/zap"
)

// Exercise the generated numeric IDs through Wails' real JSON dispatcher. Merely
// compiling TS against regenerated model names does not validate Call.ByID.
func TestGeneratedSettingsBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("slider_mapping:\n  0: master\nlanguage: en\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.New(zap.NewNop().Sugar(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Load(); err != nil {
		t.Fatal(err)
	}
	service := newSettingsService(&Desktop{config: store, version: "Version test", resolvedLanguage: "en"})
	// Bindings.Add uses the application's logger; no window or native event loop is needed.
	application.New(application.Options{DisableDefaultSignalHandler: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bindings := application.NewBindings(nil, nil)
	if err = bindings.Add(application.NewService(service)); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile("../../frontend/bindings/github.com/nik9play/deej/internal/desktop/settingsservice.ts")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uint32{}
	pattern := regexp.MustCompile(`(?s)export function (\w+)\([^\n]*\n\s*return \$Call.ByID\((\d+)`)
	for _, match := range pattern.FindAllSubmatch(generated, -1) {
		id, parseErr := strconv.ParseUint(string(match[2]), 10, 32)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		method := bindings.GetByID(uint32(id))
		if method == nil || method.Name != string(match[1]) {
			t.Fatalf("stale generated binding %s: %d", match[1], id)
		}
		ids[string(match[1])] = uint32(id)
	}
	if len(ids) < 10 {
		t.Fatalf("expected all service bindings, got %d", len(ids))
	}
	call := func(name string, args ...json.RawMessage) (any, error) {
		t.Helper()
		method := bindings.GetByID(ids[name])
		if method == nil {
			t.Fatalf("missing %s binding", name)
		}
		return method.Call(context.Background(), args)
	}
	info, err := call("GetAppInfo")
	if err != nil {
		t.Fatal(err)
	}
	if got := info.(AppInfoDTO); got.ConfigPath != path || got.Version != "Version test" || got.ResolvedLanguage != "en" {
		t.Fatalf("app info: %+v", got)
	}
	result, err := call("GetSettings")
	if err != nil {
		t.Fatal(err)
	}
	settings := result.(config.Settings)
	settings.Profiles[0].Name = "saved"
	settings.ActiveProfile = "saved"
	payload, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = call("SaveSettings", payload); err != nil {
		t.Fatal(err)
	}
	if err = store.Load(); err != nil {
		t.Fatal(err)
	}
	result, err = call("GetSettings")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(config.Settings); got.ActiveProfile != "saved" || got.Profiles[0].Name != "saved" {
		t.Fatalf("save did not persist: %+v", got)
	}
	// A rejected save can be corrected and retried through the same bridge.
	settings.COM.BaudRate = -1
	payload, _ = json.Marshal(settings)
	if _, err = call("SaveSettings", payload); err == nil {
		t.Fatal("invalid settings accepted")
	}
	settings.COM.BaudRate = 9600
	payload, _ = json.Marshal(settings)
	if _, err = call("SaveSettings", payload); err != nil {
		t.Fatal(err)
	}
}
