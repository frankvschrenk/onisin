package livekit

import (
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"
	"onisin.com/oos/secrets"
)

const (
	vaultKeyAPIKey    = "LIVEKIT_API_KEY"
	vaultKeyAPISecret = "LIVEKIT_API_SECRET"
	vaultKeyURL       = "LIVEKIT_URL"
)

type Config struct {
	APIKey    string
	APISecret string
	URL       string // WebSocket URL, z.B. "ws://localhost:7880"
}

func LoadConfig(defaults Config) (Config, error) {
	cfg := defaults

	if secrets.Active != nil {
		if key, err := secrets.Active.Get(vaultKeyAPIKey); err == nil && key != "" {
			cfg.APIKey = key
		}
		if secret, err := secrets.Active.Get(vaultKeyAPISecret); err == nil && secret != "" {
			cfg.APISecret = secret
		}
		if url, err := secrets.Active.Get(vaultKeyURL); err == nil && url != "" {
			cfg.URL = url
		}
	}

	if cfg.APIKey == "" || cfg.APISecret == "" {
		return cfg, fmt.Errorf("livekit: API Key oder Secret fehlt (Vault oder oos.toml)")
	}
	if cfg.URL == "" {
		cfg.URL = "ws://localhost:7880"
	}

	return cfg, nil
}

type TokenRequest struct {
	Identity   string // Username aus dem Authentik-JWT
	RoomName   string // Name des Rooms (für 1:1: "call_<userA>_<userB>")
	CanPublish bool   // Audio/Video senden dürfen
	TTL        time.Duration
}

func GenerateToken(cfg Config, req TokenRequest) (string, error) {
	if req.TTL == 0 {
		req.TTL = time.Hour
	}

	at := auth.NewAccessToken(cfg.APIKey, cfg.APISecret)
	grant := &auth.VideoGrant{
		RoomJoin:   true,
		Room:       req.RoomName,
		CanPublish: &req.CanPublish,
	}

	at.SetVideoGrant(grant).
		SetIdentity(req.Identity).
		SetValidFor(req.TTL)

	token, err := at.ToJWT()
	if err != nil {
		return "", fmt.Errorf("livekit: token generieren: %w", err)
	}
	return token, nil
}

func RoomName(userA, userB string) string {
	if userA > userB {
		userA, userB = userB, userA
	}
	return "call_" + userA + "_" + userB
}
