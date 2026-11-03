package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"go.uber.org/zap"
)

func TestLoadCreatesMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var outcomes []Outcome
	store, err := New(zap.NewNop().Sugar(), path, func(outcome Outcome) {
		outcomes = append(outcomes, outcome)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 0 {
		t.Fatalf("creating defaults reported a load failure: %+v", outcomes)
	}
	want := defaultSettings()
	want.normalize()
	if !reflect.DeepEqual(*store.Values(), want) {
		t.Fatalf("initial settings = %+v, want %+v", store.Values(), want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}

	// The next launch must read the generated YAML back without losing defaults.
	reloaded, err := New(zap.NewNop().Sugar(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*reloaded.Values(), want) {
		t.Fatalf("reloaded settings = %+v, want %+v", reloaded.Values(), want)
	}
}

func TestLoadDoesNotReplaceExistingConfig(t *testing.T) {
	for _, tt := range []struct {
		name, contents string
		invalid        bool
	}{
		{"valid", "# keep this comment\nlanguage: ru\ncom:\n  port: COM9\n", false},
		{"invalid", "profiles: [\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			before := []byte(tt.contents)
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			store, err := New(zap.NewNop().Sugar(), path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Load(); (err != nil) != tt.invalid {
				t.Fatalf("load error = %v, invalid = %v", err, tt.invalid)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("load overwrote existing config")
			}
		})
	}
}

func TestLoadDoesNotRecreateConfigDuringReload(t *testing.T) {
	store := newTestConfig(t, testConfigContents)
	before := store.Values()
	if err := os.Remove(store.Path()); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err == nil {
		t.Fatal("expected missing config error on reload")
	}
	if store.Values() != before {
		t.Fatal("reload discarded the last valid settings")
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("reload recreated config: %v", err)
	}
}

func TestLoadReportsDefaultConfigCreationFailure(t *testing.T) {
	for _, path := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing", "config.yaml")} {
		store, err := New(zap.NewNop().Sugar(), path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Load(); err == nil {
			t.Fatalf("expected error for config path %s", path)
		}
		if store.Values() != nil {
			t.Fatal("published defaults without a readable config file")
		}
	}
}
