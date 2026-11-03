// Package i18n owns embedded Go translations and language resolution.
package i18n

import (
	"embed"
	"fmt"
	"github.com/jeandeaual/go-locale"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/pelletier/go-toml/v2"
	"go.uber.org/zap"
	"golang.org/x/text/language"
)

//go:embed lang/active.*.toml
var langFS embed.FS

// Catalog resolves system and configured languages against embedded translations.
type Catalog struct {
	bundle *i18n.Bundle
	logger *zap.SugaredLogger
}

// New loads the bundled translations.
func New(logger *zap.SugaredLogger) (*Catalog, error) {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	if _, err := bundle.LoadMessageFileFS(langFS, "lang/active.ru.toml"); err != nil {
		return nil, fmt.Errorf("load message file: %w", err)
	}
	return &Catalog{bundle: bundle, logger: logger}, nil
}

// SystemLocalizer provides initial feedback before configuration has loaded.
func (c *Catalog) SystemLocalizer() (*i18n.Localizer, error) {
	localizer, _, err := c.Resolve("auto")
	return localizer, err
}

// Resolve selects the configured language or detects the system language.
// English messages supplied by callers provide the fallback.
func (c *Catalog) Resolve(lang string) (*i18n.Localizer, string, error) {
	if lang == "auto" {
		var err error
		lang, err = locale.GetLanguage()
		if err != nil {
			return nil, "", fmt.Errorf("get system locale: %w", err)
		}
	}
	c.logger.Infof("Selected language: %s", lang)
	return i18n.NewLocalizer(c.bundle, lang, "en"), lang, nil
}
