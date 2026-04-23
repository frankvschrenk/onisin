package helper

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type oidcEndpoints struct {
	AuthURL  string
	TokenURL string
}

func discoverOIDC(issuer string) (*oidcEndpoints, error) {
	discoveryURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"

	tlsCfg := &tls.Config{}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}

	resp, err := client.Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC Discovery nicht erreichbar (%s): %w", discoveryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC Discovery HTTP %d: %s", resp.StatusCode, discoveryURL)
	}

	var doc struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("OIDC Discovery parsen: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("OIDC Discovery: authorization_endpoint oder token_endpoint fehlt")
	}

	log.Printf("[login] OIDC Discovery: auth=%s  token=%s", doc.AuthorizationEndpoint, doc.TokenEndpoint)
	return &oidcEndpoints{
		AuthURL:  doc.AuthorizationEndpoint,
		TokenURL: doc.TokenEndpoint,
	}, nil
}

func IamLogin(_, _ string) (*AuthResult, error) {
	issuer   := strings.TrimRight(Meta.IAM.IssuerURL, "/")
	clientID := Meta.IAM.ClientID

	if issuer == "" {
		return nil, fmt.Errorf("IAM IssuerURL nicht konfiguriert")
	}
	if clientID == "" {
		return nil, fmt.Errorf("IAM ClientID nicht konfiguriert")
	}

	scope := Meta.IAM.Scope
	if scope == "" {
		scope = "openid profile email"
	}

	endpoints, err := discoverOIDC(issuer)
	if err != nil {
		return nil, err
	}

	return pkceLogin(clientID, scope, endpoints)
}

func pkceLogin(clientID, scope string, ep *oidcEndpoints) (*AuthResult, error) {
	codeVerifier  := oauth2.GenerateVerifier()
	codeChallenge := oauth2.S256ChallengeFromVerifier(codeVerifier)

	codeCh := make(chan string, 1)
	errCh  := make(chan error, 1)

	const callbackPort = 15556
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return nil, fmt.Errorf("lokaler Callback-Server (Port %d): %w", callbackPort, err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d", callbackPort)

	srv := &http.Server{}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			errCh <- fmt.Errorf("auth error: %s", r.URL.Query().Get("error_description"))
			fmt.Fprintf(w, "<html><body><h2>Login fehlgeschlagen</h2><p>%s</p></body></html>", errParam)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("kein code im Callback")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<html><head><meta charset="utf-8"></head><body>
<h2>&#x2705; Login erfolgreich</h2>
<p>Du kannst dieses Fenster schlie&szlig;en.</p>
</body></html>`)
		codeCh <- code
		go func() {
			time.Sleep(500 * time.Millisecond)
			srv.Close()
		}()
	})

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[login] callback server: %v", err)
		}
	}()

	authURL := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&code_challenge=%s&code_challenge_method=S256",
		ep.AuthURL,
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scope),
		url.QueryEscape(codeChallenge),
	)

	log.Printf("[login] Öffne Browser: %s", authURL)
	// OOS_NO_BROWSER skips the system-default-browser call. The URL is
	// still printed above; an external automation tool (Cypress-style)
	// can pick it up from stdout and drive a browser it already owns.
	if os.Getenv("OOS_NO_BROWSER") == "" {
		if err := openBrowser(authURL); err != nil {
			return nil, fmt.Errorf("browser öffnen: %w", err)
		}
	} else {
		log.Printf("[login] OOS_NO_BROWSER set — waiting for external login")
	}

	select {
	case code := <-codeCh:
		return redeemCode(ep.TokenURL, clientID, code, codeVerifier, redirectURI)
	case err := <-errCh:
		return nil, err
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("login timeout — Browser-Fenster geschlossen?")
	}
}

func openBrowser(rawURL string) error {
	return exec.Command("open", rawURL).Start()
}

func redeemCode(tokenURL, clientID, code, codeVerifier, redirectURI string) (*AuthResult, error) {
	form := url.Values{}
	form.Set("grant_type",    "authorization_code")
	form.Set("client_id",     clientID)
	form.Set("code",          code)
	form.Set("redirect_uri",  redirectURI)
	form.Set("code_verifier", codeVerifier)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{},
		},
	}

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errResp struct{ ErrorDescription string `json:"error_description"` }
		if json.Unmarshal(body, &errResp) == nil && errResp.ErrorDescription != "" {
			return nil, fmt.Errorf("%s", errResp.ErrorDescription)
		}
		return nil, fmt.Errorf("token HTTP %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("token parsen: %w", err)
	}
	if tok.IDToken == "" {
		return nil, fmt.Errorf("kein id_token — scope 'openid' fehlt?")
	}

	claims, err := decodeJWTClaims(tok.IDToken)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		AccessToken: tok.AccessToken,
		IDToken:     tok.IDToken,
		Claims:      claims,
	}, nil
}
