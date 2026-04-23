package iam

// iam.go — Embedded demo OIDC/OAuth2 provider.
//
// This is a deliberately minimal, single-purpose identity provider that
// runs as a goroutine inside oos-demo. It implements just enough of the
// OIDC authorization-code-with-PKCE flow to satisfy the oos desktop
// client's login path:
//
//   GET  /.well-known/openid-configuration   discovery document
//   GET  /auth                                login page + authorization
//   POST /token                               code + verifier -> JWT
//
// Not for production use. Password verification is reduced to picking
// a demo user from a list; there is no refresh token, no JWKS endpoint,
// no user management. In production, oos points at Keycloak, Dex or
// any other real OIDC provider via oos.toml.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// User is a single demo identity served by the embedded IAM.
type User struct {
	Email    string
	Username string
	Groups   []string
}

// Config controls the embedded IAM instance.
type Config struct {
	// Port is the TCP port the IAM listens on. 0 uses the default (5556).
	Port int

	// Users is the list of demo identities selectable on the login page.
	// Groups should match what oos expects (e.g. "oos-admin", "oos-user").
	Users []User
}

// Server is a running embedded IAM instance.
type Server struct {
	cfg    Config
	http   *http.Server
	issuer string
	signer *hs256Signer

	// pendingCodes maps short-lived auth codes to the user and PKCE
	// challenge that were chosen at /auth time. /token consumes them.
	mu           sync.Mutex
	pendingCodes map[string]pendingCode
}

type pendingCode struct {
	user          User
	clientID      string
	redirectURI   string
	codeChallenge string
	issuedAt      time.Time
}

// Start launches the IAM HTTP server in a goroutine and returns once it
// is ready to accept connections. Call Stop to shut it down cleanly.
func Start(cfg Config) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = 5556
	}
	if len(cfg.Users) == 0 {
		return nil, fmt.Errorf("iam: at least one demo user required")
	}

	signer, err := newHS256Signer()
	if err != nil {
		return nil, fmt.Errorf("iam: signer init: %w", err)
	}

	s := &Server{
		cfg:          cfg,
		issuer:       fmt.Sprintf("http://localhost:%d", cfg.Port),
		signer:       signer,
		pendingCodes: make(map[string]pendingCode),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("/auth",  s.handleAuth)
	mux.HandleFunc("/token", s.handleToken)

	s.http = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}

	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[iam] server error: %v", err)
		}
	}()

	log.Printf("[iam] ⚠️  demo IAM — not for production")
	log.Printf("[iam] listening on %s", s.issuer)
	return s, nil
}

// Stop gracefully shuts down the IAM server.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
}

// handleDiscovery serves the OIDC discovery document. Only the two fields
// the oos desktop client reads (authorization_endpoint, token_endpoint)
// are strictly required, but we return the full minimum set for anything
// else that might look.
func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	doc := map[string]any{
		"issuer":                                s.issuer,
		"authorization_endpoint":                s.issuer + "/auth",
		"token_endpoint":                        s.issuer + "/token",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"HS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

// handleAuth handles both the initial GET (render the picker) and the
// POST back from the picker form (issue the auth code and redirect).
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderPicker(w, r.URL.Query())
	case http.MethodPost:
		s.issueCode(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// renderPicker shows a plain HTML page with one radio per demo user.
// The OAuth2 parameters travel through as hidden form fields so the
// POST back to /auth has everything it needs.
func (s *Server) renderPicker(w http.ResponseWriter, q url.Values) {
	clientID      := q.Get("client_id")
	redirectURI   := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	state         := q.Get("state")

	if clientID == "" || redirectURI == "" || codeChallenge == "" {
		http.Error(w, "missing client_id, redirect_uri or code_challenge", http.StatusBadRequest)
		return
	}

	var radios strings.Builder
	for i, u := range s.cfg.Users {
		checked := ""
		if i == 0 {
			checked = "checked"
		}
		radios.WriteString(fmt.Sprintf(
			`<label><input type="radio" name="email" value="%s" %s> %s <small>(%s)</small></label><br>`,
			html.EscapeString(u.Email), checked,
			html.EscapeString(u.Username), html.EscapeString(strings.Join(u.Groups, ", ")),
		))
	}

	page := fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>OOS Demo Login</title>
<style>
 body {font-family: system-ui, sans-serif; max-width: 420px; margin: 4em auto; color: #222;}
 h1   {font-size: 1.2em;}
 .warn {background: #fff3cd; border: 1px solid #e0c97a; padding: .5em .8em; border-radius: 4px; font-size: .9em;}
 button {margin-top: 1em; padding: .5em 1.2em; font-size: 1em;}
 label {display: block; padding: .3em 0;}
</style></head><body>
<h1>OOS Demo Login</h1>
<p class="warn">This is a demo identity provider. Pick a user to continue.</p>
<form method="post" action="/auth">
 <input type="hidden" name="client_id"      value="%s">
 <input type="hidden" name="redirect_uri"   value="%s">
 <input type="hidden" name="code_challenge" value="%s">
 <input type="hidden" name="state"          value="%s">
 %s
 <button type="submit">Continue</button>
</form>
</body></html>`,
		html.EscapeString(clientID),
		html.EscapeString(redirectURI),
		html.EscapeString(codeChallenge),
		html.EscapeString(state),
		radios.String(),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page))
}

// issueCode receives the picker POST, stores the pending auth code,
// and redirects back to the client's redirect_uri with ?code=...
func (s *Server) issueCode(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	email         := r.FormValue("email")
	clientID      := r.FormValue("client_id")
	redirectURI   := r.FormValue("redirect_uri")
	codeChallenge := r.FormValue("code_challenge")
	state         := r.FormValue("state")

	user, ok := s.lookupUser(email)
	if !ok {
		http.Error(w, "unknown user", http.StatusBadRequest)
		return
	}

	code, err := randomToken(24)
	if err != nil {
		http.Error(w, "entropy", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.pendingCodes[code] = pendingCode{
		user:          user,
		clientID:      clientID,
		redirectURI:   redirectURI,
		codeChallenge: codeChallenge,
		issuedAt:      time.Now(),
	}
	s.mu.Unlock()

	target := fmt.Sprintf("%s?code=%s", redirectURI, url.QueryEscape(code))
	if state != "" {
		target += "&state=" + url.QueryEscape(state)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleToken exchanges a pending auth code (+ PKCE verifier) for a
// signed JWT id_token. The access_token is the same JWT — the oos
// client uses the id_token for claims and does not separately validate
// the access_token.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	code         := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	if code == "" || codeVerifier == "" {
		tokenError(w, "invalid_request", "missing code or code_verifier")
		return
	}

	s.mu.Lock()
	pc, ok := s.pendingCodes[code]
	if ok {
		delete(s.pendingCodes, code)
	}
	s.mu.Unlock()

	if !ok {
		tokenError(w, "invalid_grant", "unknown or already-used code")
		return
	}
	if time.Since(pc.issuedAt) > 5*time.Minute {
		tokenError(w, "invalid_grant", "code expired")
		return
	}
	if !verifyPKCE(codeVerifier, pc.codeChallenge) {
		tokenError(w, "invalid_grant", "PKCE verification failed")
		return
	}

	idToken, err := s.signer.sign(jwtClaims{
		Issuer:            s.issuer,
		Subject:           pc.user.Email,
		Audience:          pc.clientID,
		Email:             pc.user.Email,
		PreferredUsername: pc.user.Username,
		Groups:            pc.user.Groups,
		IssuedAt:          time.Now().Unix(),
		ExpiresAt:         time.Now().Add(8 * time.Hour).Unix(),
	})
	if err != nil {
		tokenError(w, "server_error", err.Error())
		return
	}

	resp := map[string]any{
		"access_token": idToken,
		"id_token":     idToken,
		"token_type":   "Bearer",
		"expires_in":   int((8 * time.Hour).Seconds()),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// lookupUser finds a configured demo user by email.
func (s *Server) lookupUser(email string) (User, bool) {
	for _, u := range s.cfg.Users {
		if u.Email == email {
			return u, true
		}
	}
	return User{}, false
}

// tokenError writes an OAuth2-style error response.
func tokenError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}

// randomToken returns a URL-safe random string of n bytes of entropy.
func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
