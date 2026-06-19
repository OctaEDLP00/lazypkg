package main

import (
	"embed"
	"os"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localeFS embed.FS

type Localizer struct {
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
}

func NewLocalizer() *Localizer {
	// Se establece Inglés como idioma por defecto
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

	// Lista explícita de archivos locales a parsear e incorporar al bundle
	locales := []string{
		"../../locales/de_de.yaml",
		"../../locales/en_us.yaml",
		"../../locales/es_ar.yaml",
		"../../locales/fr_ch.yaml",
		"../../locales/fr_fr.yaml",
	}

	for _, path := range locales {
		// Leemos el archivo del sistema embebido de forma explícita
		data, err := localeFS.ReadFile(path)
		if err != nil {
			continue
		}

		// Cargamos el contenido directamente usando el parseador registrado
		bundle.MustParseMessageFileBytes(data, path)
	}

	userLang := os.Getenv("LANG")
	if userLang == "" {
		userLang = "en_us"
	} else {
		userLang = strings.ToLower(strings.Split(userLang, ".")[0])
		userLang = strings.ReplaceAll(userLang, "-", "_")
	}

	loc := i18n.NewLocalizer(bundle, userLang)

	return &Localizer{
		bundle:    bundle,
		localizer: loc,
	}
}

func (l *Localizer) T(messageID string) string {
	t, err := l.localizer.Localize(&i18n.LocalizeConfig{
		MessageID: messageID,
	})
	if err != nil {
		return messageID
	}
	return t
}

func (l *Localizer) SetLanguage(lang string) {
	l.localizer = i18n.NewLocalizer(l.bundle, lang)
}
