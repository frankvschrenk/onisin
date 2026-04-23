package oosp

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// tokenRoundTripper setzt bei jedem Request einen frischen Bearer Token.
type tokenRoundTripper struct {
	base    http.RoundTripper
	tokenFn func() string
}

func (t *tokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.tokenFn != nil {
		if token := t.tokenFn(); token != "" {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return t.base.RoundTrip(req)
}

// pinnedRoundTripper prueft den Server-Cert Fingerprint statt einer CA.
// Verbindung wird abgelehnt wenn der Fingerprint nicht stimmt.
type pinnedRoundTripper struct {
	base        http.RoundTripper
	fingerprint string // SHA256 hex des Server Public Key
	tokenFn     func() string
}

func (t *pinnedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.tokenFn != nil {
		if token := t.tokenFn(); token != "" {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return t.base.RoundTrip(req)
}

// Client kommuniziert mit einem OOSP Plugin-MCP-Server.
type Client struct {
	mcp *mcpclient.Client
	ctx context.Context
	mu  sync.Mutex
}

// NewHTTP verbindet sich zu einem plain HTTP MCP Server.
func NewHTTP(url string) (*Client, error) {
	base := mcpURL(url)
	c, err := mcpclient.NewStreamableHttpClient(base)
	if err != nil {
		return nil, fmt.Errorf("oosp http: %w", err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("oosp http start: %w", err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return nil, fmt.Errorf("oosp http init: %w", err)
	}
	log.Printf("[oosp] ✅ HTTP → %s", url)
	return &Client{mcp: c, ctx: ctx}, nil
}

// NewPinned verbindet sich zu OOSP ueber HTTPS mit Fingerprint-Pinning.
//
// Statt einer CA wird der SHA256-Fingerprint des Server Public Key geprueft.
// Der Fingerprint kommt aus dem JWT Claim oos_oosp_fingerprint.
//
// Kein Vault noetig auf OOS-Seite. Kein Zertifikat-Ablauf. Wie Iroh NodeIDs.
func NewPinned(oospURL, fingerprint string, tokenFn func() string) (*Client, error) {
	if fingerprint == "" {
		return nil, fmt.Errorf("oos_oosp_fingerprint fehlt im JWT")
	}

	// TLS ohne CA-Validierung -- wir pruefen selbst den Fingerprint
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec -- Fingerprint-Pruefung unten
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifyFingerprint(cs, fingerprint)
		},
	}

	tlsTransport := &http.Transport{TLSClientConfig: tlsCfg}
	httpClient := &http.Client{
		Transport: &tokenRoundTripper{base: tlsTransport, tokenFn: tokenFn},
	}

	c, err := mcpclient.NewStreamableHttpClient(mcpURL(oospURL),
		transport.WithHTTPBasicClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("oosp pinned: mcp client: %w", err)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("oosp pinned: start: %w", err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return nil, fmt.Errorf("oosp pinned: init: %w", err)
	}

	log.Printf("[oosp] ✅ Pinned TLS → %s (fp: %s...)", oospURL, fingerprint[:16])
	return &Client{mcp: c, ctx: ctx}, nil
}

// NewTLS verbindet sich zu OOSP via HTTPS mit den System Root CAs.
// Das CA-Zertifikat muss einmalig im OS-Zertifikatspeicher installiert sein
// (macOS Keychain, Windows Cert Store, Linux /etc/ssl).
// Danach vertraut OOS der Vault-CA automatisch -- genau wie jeder Browser.
func NewTLS(oospURL string, tokenFn func() string) (*Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		// Fallback: leerer Pool -- TLS schlägt fehl wenn CA nicht installiert
		log.Printf("[oosp] ⚠️  System CertPool nicht verfügbar: %v", err)
		pool = x509.NewCertPool()
	}

	tlsTransport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}
	httpClient := &http.Client{
		Transport: &tokenRoundTripper{base: tlsTransport, tokenFn: tokenFn},
	}

	c, err := mcpclient.NewStreamableHttpClient(mcpURL(oospURL),
		transport.WithHTTPBasicClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("oosp tls: mcp client: %w", err)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("oosp tls: start: %w", err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		return nil, fmt.Errorf("oosp tls: init: %w", err)
	}

	log.Printf("[oosp] ✅ TLS (System CA) → %s", oospURL)
	return &Client{mcp: c, ctx: ctx}, nil
}

// verifyFingerprint prueft ob der Server Public Key mit dem erwarteten Fingerprint uebereinstimmt.
// SHA256 des DER-kodierten Public Key (SPKI) -- Standard wie in Chrome/Firefox "Certificate Pinning".
func verifyFingerprint(cs tls.ConnectionState, expected string) error {
	if len(cs.PeerCertificates) == 0 {
		return fmt.Errorf("keine Server-Zertifikate")
	}
	cert := cs.PeerCertificates[0]
	spki, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("public key marshal: %w", err)
	}
	sum := sha256.Sum256(spki)
	got := hex.EncodeToString(sum[:])

	if got != strings.ToLower(expected) {
		return fmt.Errorf("fingerprint mismatch: erwartet %s, got %s", expected, got)
	}
	return nil
}

// fetchVaultCARootPEM holt den CA Root von Vault PKI (public endpoint).
func fetchVaultCARootPEM(vaultURL string) ([]byte, error) {
	caURL := strings.TrimRight(vaultURL, "/") + "/v1/pki/ca/pem"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(caURL)
	if err != nil {
		return nil, fmt.Errorf("vault nicht erreichbar (%s): %w", caURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault ca root %d: %s", resp.StatusCode, body)
	}
	pem, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vault ca root lesen: %w", err)
	}
	if len(pem) == 0 {
		return nil, fmt.Errorf("vault ca root leer")
	}
	return pem, nil
}

// GetSources fragt den Plugin-Server nach seinen verfuegbaren DSN-Namen.
func (c *Client) GetSources() ([]string, error) {
	raw, err := c.Call("oosp_sources", nil)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, fmt.Errorf("oosp_sources parse: %w", err)
	}
	return names, nil
}

// Call ruft ein Plugin-Tool auf und gibt das Ergebnis als JSON-String zurueck.
func (c *Client) Call(tool string, args map[string]string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	argsMap := make(map[string]interface{})
	for k, v := range args {
		argsMap[k] = v
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = argsMap

	resp, err := c.mcp.CallTool(c.ctx, req)
	if err != nil {
		return "", fmt.Errorf("oosp: %w", err)
	}

	for _, content := range resp.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return "", fmt.Errorf("oosp: leere Antwort")
}

// mcpURL stellt sicher dass die URL auf /mcp endet.
func mcpURL(url string) string {
	base := strings.TrimRight(url, "/")
	if !strings.HasSuffix(base, "/mcp") {
		base += "/mcp"
	}
	return base
}
