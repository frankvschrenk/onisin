package iam

// jwt.go — HS256 JWT signing and PKCE verification.
//
// The demo IAM signs id_tokens with HS256 using a random secret that
// lives only in memory for the lifetime of the server. Since no
// downstream component actually verifies the signature (oosp only
// reads the X-OOS-Group header, not the token, and the oos client
// parses the JWT without verifying it), HS256 is sufficient: we want
// structurally valid JWTs, not cryptographic guarantees.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// jwtClaims are the fields the oos desktop client reads from the id_token.
// Struct tags match the JSON keys expected by oos/helper/auth.go.
type jwtClaims struct {
	Issuer            string   `json:"iss"`
	Subject           string   `json:"sub"`
	Audience          string   `json:"aud"`
	Email             string   `json:"email"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
	IssuedAt          int64    `json:"iat"`
	ExpiresAt         int64    `json:"exp"`
}

// hs256Signer signs JWTs with a single in-memory secret.
type hs256Signer struct {
	secret []byte
}

// newHS256Signer generates a fresh 32-byte signing secret.
func newHS256Signer() (*hs256Signer, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	return &hs256Signer{secret: secret}, nil
}

// sign encodes claims as a compact JWS in the form header.payload.signature.
func (s *hs256Signer) sign(claims jwtClaims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	h := base64.RawURLEncoding.EncodeToString(headerJSON)
	p := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := h + "." + p

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

// verifyPKCE checks that sha256(verifier) base64url-encoded (no padding)
// matches the challenge the client sent at /auth time. S256 is the only
// supported method — see the discovery document.
func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	return hmac.Equal([]byte(expected), []byte(challenge))
}
