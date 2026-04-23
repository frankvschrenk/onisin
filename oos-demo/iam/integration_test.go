package iam

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestFullAuthFlow exercises discovery -> /auth GET (picker) -> /auth POST
// (code issuance) -> /token (JWT redemption) end to end against a running
// Server instance. This is the scenario the oos desktop client will run
// against at demo time.
func TestFullAuthFlow(t *testing.T) {
	srv, err := Start(Config{
		Port: 15557, // non-standard to avoid clashing with a real dex/iam on 5556
		Users: []User{
			{Email: "admin@oos.local", Username: "admin", Groups: []string{"oos-admin"}},
			{Email: "user@oos.local",  Username: "user",  Groups: []string{"oos-user"}},
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	// Give the listener a moment to bind.
	time.Sleep(100 * time.Millisecond)

	base := "http://localhost:15557"

	// 1. Discovery
	var disco struct {
		AuthEndpoint  string `json:"authorization_endpoint"`
		TokenEndpoint string `json:"token_endpoint"`
		Issuer        string `json:"issuer"`
	}
	resp, err := http.Get(base + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&disco); err != nil {
		t.Fatalf("discovery decode: %v", err)
	}
	resp.Body.Close()
	if disco.AuthEndpoint != base+"/auth" {
		t.Errorf("authorization_endpoint: got %q", disco.AuthEndpoint)
	}
	if disco.TokenEndpoint != base+"/token" {
		t.Errorf("token_endpoint: got %q", disco.TokenEndpoint)
	}

	// 2. /auth GET — picker page must render and mention both demo users.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	authGET := disco.AuthEndpoint + "?" + url.Values{
		"client_id":             {"oos-desktop"},
		"redirect_uri":           {"http://127.0.0.1:15556"},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	resp, err = http.Get(authGET)
	if err != nil {
		t.Fatalf("auth GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "admin@oos.local") ||
		!strings.Contains(string(body), "user@oos.local") {
		t.Fatalf("picker missing users: %s", body)
	}

	// 3. /auth POST — simulate the picker form submission. The server
	//    answers with a 302 to the redirect_uri carrying ?code=...
	//    Disable redirects so we can read the Location header.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	postForm := url.Values{
		"email":          {"admin@oos.local"},
		"client_id":      {"oos-desktop"},
		"redirect_uri":   {"http://127.0.0.1:15556"},
		"code_challenge": {challenge},
	}
	resp, err = client.PostForm(disco.AuthEndpoint, postForm)
	if err != nil {
		t.Fatalf("auth POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("auth POST status: got %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("location parse: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", resp.Header.Get("Location"))
	}

	// 4. /token POST — redeem the code + PKCE verifier and confirm the
	//    JWT payload contains the expected claims for admin.
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://127.0.0.1:15556"},
		"client_id":     {"oos-desktop"},
		"code_verifier": {verifier},
	}
	resp, err = http.PostForm(disco.TokenEndpoint, tokenForm)
	if err != nil {
		t.Fatalf("token POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("token status: got %d, body: %s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("token decode: %v", err)
	}
	resp.Body.Close()

	// 5. Inspect the id_token payload (no signature verification — see
	//    jwt.go comment for why).
	parts := strings.Split(tok.IDToken, ".")
	if len(parts) != 3 {
		t.Fatalf("id_token malformed: %q", tok.IDToken)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if claims.Email != "admin@oos.local" {
		t.Errorf("email: got %q", claims.Email)
	}
	if claims.PreferredUsername != "admin" {
		t.Errorf("preferred_username: got %q", claims.PreferredUsername)
	}
	if len(claims.Groups) != 1 || claims.Groups[0] != "oos-admin" {
		t.Errorf("groups: got %v", claims.Groups)
	}

	// 6. Code reuse must fail.
	resp, err = http.PostForm(disco.TokenEndpoint, tokenForm)
	if err != nil {
		t.Fatalf("token reuse POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("code reuse succeeded — should have failed")
	}
}
