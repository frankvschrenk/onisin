package helper

// meta.go — Global runtime configuration model.
//
// Meta is populated from oos.toml on startup via LoadAppDirsConfig.
// JWT claims from the OIDC login flow fill the Active* variables.

import (
	"log"
)

// MetaModel holds the runtime configuration for the oos application.
// Values are populated from oos.toml on startup.
type MetaModel struct {
	Version     string
	BuildNumber string
	LocalMode   bool

	OOSPUrl         string
	OOSPFingerprint string

	Vault VaultConfig

	IAM IAMConfig

	LiveKitAPIKey    string
	LiveKitAPISecret string
	LiveKitURL       string

	ClusterEndpoints []string

	SecureSrvPort int
	BridgeURL     string
}

// VaultConfig holds the OpenBao / Vault connection settings.
type VaultConfig struct {
	URL string
}

// IAMConfig holds the OIDC identity provider settings.
type IAMConfig struct {
	IssuerURL   string
	ClientID    string
	Scope       string
	LoginPAT    string
	RedirectURI string
}

// Meta is the global runtime configuration instance.
var Meta MetaModel

// ActiveGroups holds the OIDC group memberships of the logged-in user.
var ActiveGroups []string

// ActiveRole is the resolved OOS role derived from ActiveGroups.
var ActiveRole string

// ActiveIDToken is the raw OIDC ID token of the current session.
var ActiveIDToken string

var activeEmail    string
var activeUsername string

// SetActiveIdentity stores the email and username from JWT claims.
func SetActiveIdentity(email, username string) {
	activeEmail    = email
	activeUsername = username
}

// ActiveUsername returns the username of the currently logged-in user.
func ActiveUsername() string { return activeUsername }

// ActiveEmail returns the email address of the currently logged-in user.
func ActiveEmail() string { return activeEmail }

// Log writes the current Meta values to the standard logger.
func (m *MetaModel) Log() {
	log.Printf("[meta] version=%s oosp=%s vault=%s",
		m.Version, m.OOSPUrl, m.Vault.URL)
}
