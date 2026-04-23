package boot

// init.go — Config initialisation from oos.toml.

import (
	"log"

	"onisin.com/oos/helper"
)

// initFromConfig loads oos.toml and applies all values to the global
// configuration. Returns an error when the file is missing or invalid.
func initFromConfig() error {
	if err := helper.LoadAppDirsConfig(); err != nil {
		log.Printf("[config] oos.toml not loaded: %v", err)
		log.Printf("[config] please open Settings and configure the application")
		return err
	}

	log.Printf("[config] ✅ OOSP: %s", helper.Meta.OOSPUrl)
	log.Printf("[config] ✅ Dex:  %s", helper.Meta.IAM.IssuerURL)

	return nil
}
