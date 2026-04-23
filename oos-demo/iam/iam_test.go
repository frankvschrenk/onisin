package iam

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestVerifyPKCE checks that a known verifier/challenge pair validates
// and that a tampered verifier does not. The golden values come from
// RFC 7636 appendix B.
func TestVerifyPKCE(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !verifyPKCE(verifier, challenge) {
		t.Fatal("valid pair rejected")
	}
	if verifyPKCE(verifier+"x", challenge) {
		t.Fatal("tampered verifier accepted")
	}
}

// TestSignProducesParseableJWT round-trips the payload: sign, split on
// '.', decode the middle segment, and confirm the claims come back.
// The signature itself is not verified here — only structural validity
// and payload fidelity matter for the oos client path.
func TestSignProducesParseableJWT(t *testing.T) {
	signer, err := newHS256Signer()
	if err != nil {
		t.Fatalf("signer init: %v", err)
	}

	token, err := signer.sign(jwtClaims{
		Issuer:            "http://localhost:5556",
		Subject:           "admin@oos.local",
		Audience:          "oos-desktop",
		Email:             "admin@oos.local",
		PreferredUsername: "admin",
		Groups:            []string{"oos-admin"},
		IssuedAt:          1_700_000_000,
		ExpiresAt:         1_700_028_800,
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload decode: %v", err)
	}

	var got jwtClaims
	if err := json.Unmarshal(payloadJSON, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}

	if got.Email != "admin@oos.local" {
		t.Errorf("email: got %q", got.Email)
	}
	if got.PreferredUsername != "admin" {
		t.Errorf("preferred_username: got %q", got.PreferredUsername)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "oos-admin" {
		t.Errorf("groups: got %v", got.Groups)
	}
}
