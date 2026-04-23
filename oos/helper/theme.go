package helper

// theme.go — User preference for the UI theme variant (light/dark).
//
// The variant is stored in oos.toml under [ui] variant. It is kept in
// a dedicated file rather than bolted onto SetupConfig because the
// setup dialog and the theme switcher are independent affordances —
// the user changes the variant often, the setup values rarely.

import (
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/viper"

	oostheme "onisin.com/oos-common/theme"
)

// activeVariant is the in-memory copy of [ui] variant from oos.toml.
// Updated by LoadAppDirsConfig at startup and by SaveThemeVariant at
// runtime. Default is "light".
var activeVariant = "light"

// ActiveTheme is the fully-resolved theme currently in use. It may
// come from oos.ctx[theme] or from oostheme.DefaultTheme — callers
// should treat it as read-only.
var ActiveTheme *oostheme.OOSTheme

// PreferredThemeVariant returns the user's preferred variant
// ("light" or "dark"). Always returns one of those two values.
func PreferredThemeVariant() string {
	if activeVariant == "dark" {
		return "dark"
	}
	return "light"
}

// applyThemeVariantFromViper is called by LoadAppDirsConfig after
// viper has read oos.toml. Kept here so the variant handling stays
// in one file.
func applyThemeVariantFromViper() {
	if v := viper.GetString("ui.variant"); v != "" {
		activeVariant = v
	}
}

// SaveThemeVariant persists variant ("light" or "dark") to oos.toml
// and updates the in-memory value. Other keys in the file are
// preserved.
func SaveThemeVariant(variant string) error {
	if variant != "light" && variant != "dark" {
		variant = "light"
	}

	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	_ = v.ReadInConfig()

	v.Set("ui.variant", variant)

	if err := v.WriteConfig(); err != nil {
		if err2 := v.SafeWriteConfig(); err2 != nil {
			return err
		}
	}

	activeVariant = variant
	log.Printf("[config] theme variant: %s", variant)
	return nil
}
