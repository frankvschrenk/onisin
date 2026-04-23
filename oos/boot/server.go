package boot

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"onisin.com/oos/helper"
	"onisin.com/oos/secrets"
)

const (
	vaultKeySession = "OOS_SESSION"
	vaultKeyCert    = "OOS_TLS_CERT"
	vaultKeyKey     = "OOS_TLS_KEY"
)

func loadCachedSession() *helper.CachedSession {
	if secrets.Active == nil {
		return nil
	}
	raw, err := secrets.Active.Get(vaultKeySession)
	if err != nil || raw == "" {
		return nil
	}
	session, err := helper.DecodeSession(raw)
	if err != nil {
		log.Printf("[boot] Session aus Vault ungültig: %v", err)
		return nil
	}
	return session
}

func storeSession(idToken string, claims map[string]interface{}) {
	if secrets.Active == nil {
		return
	}
	encoded, err := helper.EncodeSession(idToken, claims)
	if err != nil {
		return
	}
	if err := secrets.Active.Set(vaultKeySession, encoded); err != nil {
		log.Printf("[boot] Session speichern: %v", err)
	} else {
		log.Println("[boot] Session in Vault gespeichert")
	}
}

func loadCert() *tls.Certificate {
	if secrets.Active == nil {
		return nil
	}
	certPEM, err := secrets.Active.Get(vaultKeyCert)
	if err != nil || certPEM == "" {
		return nil
	}
	keyPEM, err := secrets.Active.Get(vaultKeyKey)
	if err != nil || keyPEM == "" {
		return nil
	}
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil
	}
	if len(cert.Certificate) > 0 {
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		if err == nil && time.Now().After(x509Cert.NotAfter.Add(-5*time.Minute)) {
			log.Println("[boot] TLS-Cert läuft bald ab — wird erneuert")
			return nil
		}
	}
	return &cert
}

func storeCert(certPEM, keyPEM string) {
	if secrets.Active == nil {
		return
	}
	secrets.Active.Set(vaultKeyCert, certPEM) //nolint:errcheck
	secrets.Active.Set(vaultKeyKey, keyPEM)   //nolint:errcheck
	log.Println("[boot] TLS-Cert in Vault gespeichert")
}

func writeBridgeCA(vaultURL string) {
	if vaultURL == "" {
		return
	}
	caPEM, err := fetchURL(vaultURL + "/v1/pki/ca/pem")
	if err != nil || len(caPEM) == 0 {
		log.Printf("[boot] Bridge CA nicht erreichbar: %v", err)
		return
	}
	cachePath, err := oosCAPath()
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(cachePath), 0700) //nolint:errcheck
	os.WriteFile(cachePath, caPEM, 0600)       //nolint:errcheck
	log.Printf("[boot] Bridge CA → %s", cachePath)
}

func fetchURL(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func oosCAPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("UserCacheDir: %w", err)
	}
	return filepath.Join(dir, "oos", "oos_ca.pem"), nil
}
