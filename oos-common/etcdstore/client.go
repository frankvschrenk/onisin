package etcdstore

// client.go — etcd Client fuer OOS Service Discovery.
//
// OOS liest beim Start folgende Konfiguration aus etcd:
//   /oos/config/vault_url        → Vault URL
//   /oos/config/html_type        → s3 | filesystem
//   /oos/config/html_dir         → oos-html
//   /oos/config/iam_issuer_url   → IAM Realm URL
//   /oos/config/iam_client_id    → OAuth Client ID
//   /oos/config/iam_scope        → OAuth Scope
//   /oos/services/oosp/url       → OOSP URL
//   /oos/services/oosp/fp        → OOSP Fingerprint
//   /oos/services/oosp/dsn_<n>   → Datasource als JSON
//
// Ollama-Konfiguration kommt NICHT aus etcd — sie liegt user-spezifisch
// in der oos.toml (appdirs) und wird über das Einstellungs-Menü verwaltet.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"onisin.com/oos-common/db"
)

const (
	KeyVaultURL     = "/oos/config/vault_url"
	KeyHTMLType     = "/oos/config/html_type"
	KeyHTMLDir      = "/oos/config/html_dir"
	KeyIAMIssuerURL = "/oos/config/iam_issuer_url"
	KeyIAMClientID  = "/oos/config/iam_client_id"
	KeyIAMScope     = "/oos/config/iam_scope"
	KeyIAMLoginPAT  = "/oos/config/iam_login_pat"
	KeyOOSPURL      = "/oos/services/oosp/url"
	KeyOOSPFP       = "/oos/services/oosp/fp"
	KeyOOSPBackend  = "/oos/services/oosp/backend"
	KeyOOSPDSN      = "/oos/services/oosp/dsn"
	PrefixOOSPDSN   = "/oos/services/oosp/dsn_"
)

type Client struct {
	etcd *clientv3.Client
}

func New(endpoints []string) (*Client, error) {
	if len(endpoints) == 0 {
		endpoints = []string{"localhost:2379"}
	}
	etcd, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("etcd verbinden (%v): %w", endpoints, err)
	}
	log.Printf("[etcd] Verbunden: %v", endpoints)
	return &Client{etcd: etcd}, nil
}

func (c *Client) Close() {
	c.etcd.Close()
}

func (c *Client) Get(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.etcd.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("etcd get %q: %w", key, err)
	}
	if len(resp.Kvs) == 0 {
		return "", nil
	}
	return string(resp.Kvs[0].Value), nil
}

func (c *Client) GetWithPrefix(prefix string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.etcd.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd get prefix %q: %w", prefix, err)
	}
	result := map[string]string{}
	for _, kv := range resp.Kvs {
		result[string(kv.Key)] = string(kv.Value)
	}
	return result, nil
}

func (c *Client) Put(key, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.etcd.Put(ctx, key, value)
	if err != nil {
		return fmt.Errorf("etcd put %q: %w", key, err)
	}
	return nil
}

func (c *Client) Watch(key string, onChange func(value string)) {
	go func() {
		ch := c.etcd.Watch(context.Background(), key)
		for resp := range ch {
			for _, ev := range resp.Events {
				log.Printf("[etcd] watch %q → geaendert", key)
				onChange(string(ev.Kv.Value))
			}
		}
	}()
}

// OOSConfig enthaelt die komplette OOS Konfiguration aus etcd.
type OOSConfig struct {
	IAMIssuerURL    string
	IAMClientID     string
	IAMScope        string
	IAMLoginPAT     string
	VaultURL        string
	HTMLType        string
	HTMLDir         string
	OOSPURL         string
	OOSPFingerprint string
}

// OOSPConfig enthaelt die OOSP Server Konfiguration aus etcd.
type OOSPConfig struct {
	Backend     string
	DSN         string
	Datasources map[string]db.DatasourceConfig
}

// LoadOOSConfig laedt die OOS Client Konfiguration aus etcd.
func (c *Client) LoadOOSConfig() (*OOSConfig, error) {
	cfg := &OOSConfig{}
	mapping := []struct {
		key  string
		dest *string
	}{
		{KeyIAMIssuerURL, &cfg.IAMIssuerURL},
		{KeyIAMClientID, &cfg.IAMClientID},
		{KeyIAMScope, &cfg.IAMScope},
		{KeyIAMLoginPAT, &cfg.IAMLoginPAT},
		{KeyVaultURL, &cfg.VaultURL},
		{KeyHTMLType, &cfg.HTMLType},
		{KeyHTMLDir, &cfg.HTMLDir},
		{KeyOOSPURL, &cfg.OOSPURL},
		{KeyOOSPFP, &cfg.OOSPFingerprint},
	}
	for _, m := range mapping {
		val, err := c.Get(m.key)
		if err != nil {
			return nil, fmt.Errorf("etcd lesen fehlgeschlagen: %w", err)
		}
		*m.dest = val
	}
	missing := []string{}
	if cfg.IAMIssuerURL == "" {
		missing = append(missing, KeyIAMIssuerURL)
	}
	if cfg.IAMClientID == "" {
		missing = append(missing, KeyIAMClientID)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("erforderliche etcd Keys fehlen: %v", missing)
	}
	if cfg.IAMScope == "" {
		cfg.IAMScope = "openid profile email"
	}
	log.Printf("[etcd] IAM: %s  OOSP: %s", cfg.IAMIssuerURL, cfg.OOSPURL)
	return cfg, nil
}

// LoadOOSPConfig laedt die OOSP Server Konfiguration aus etcd.
func (c *Client) LoadOOSPConfig() (*OOSPConfig, error) {
	cfg := &OOSPConfig{
		Datasources: map[string]db.DatasourceConfig{},
	}

	backend, err := c.Get(KeyOOSPBackend)
	if err != nil {
		return nil, err
	}
	cfg.Backend = backend

	dsn, err := c.Get(KeyOOSPDSN)
	if err != nil {
		return nil, err
	}
	cfg.DSN = dsn

	entries, err := c.GetWithPrefix(PrefixOOSPDSN)
	if err != nil {
		return nil, err
	}
	for key, raw := range entries {
		name := strings.ToLower(strings.TrimPrefix(key, PrefixOOSPDSN))
		if name == "" || raw == "" {
			continue
		}
		dsCfg, err := db.ParseDatasourceConfig(raw)
		if err != nil {
			log.Printf("[etcd] datasource %q: JSON parsen fehlgeschlagen: %v — übersprungen", name, err)
			continue
		}
		cfg.Datasources[name] = *dsCfg
	}

	log.Printf("[etcd] OOSP backend=%s datasources=%d", cfg.Backend, len(cfg.Datasources))
	return cfg, nil
}
