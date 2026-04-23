package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const bufferSize = 1024 * 1024

var stdoutMu sync.Mutex
var sessionID string
var mcpURL string

func Run(url string) {
	if url == "" {
		url = "https://localhost:59124/mcp"
	}
	mcpURL = url
	log.Printf("[bridge] gestartet → %s", url)

	client, err := buildTLSClient()
	if err != nil {
		log.Fatalf("[bridge] TLS Client Fehler: %v", err)
	}

	runStdinLoop(client, url)
}

// ── Stdin Loop — loggt ALLES was Claude Desktop schickt ──────────────────────

func runStdinLoop(client *http.Client, url string) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, bufferSize), bufferSize)

	for scanner.Scan() {
		msg := scanner.Bytes()
		if len(bytes.TrimSpace(msg)) == 0 {
			continue
		}

		var req map[string]any
		if err := json.Unmarshal(msg, &req); err != nil {
			writeResponse(errorResponse(nil, -32700, "Parse error"))
			continue
		}

		method, _ := req["method"].(string)
		_, hasID := req["id"]
		isNotification := !hasID && method != ""

		// Log: was kommt von Claude Desktop
		log.Printf("[bridge] ← Claude Desktop: method=%q id=%v", method, req["id"])

		switch method {
		case "initialize":
			handleInitialize(client, url, msg, req, isNotification)
		case "tools/list":
			log.Printf("[bridge] ← tools/list von Claude Desktop")
			handleToolsList(client, url, req, isNotification)
		default:
			handleGeneric(client, url, msg, req, isNotification)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("[bridge] Stdin Fehler: %v", err)
	}
}

// ── Handler ───────────────────────────────────────────────────────────────────

func handleInitialize(client *http.Client, url string, raw []byte, req map[string]any, isNotification bool) {
	response, err := sendMessage(client, url, raw)
	if err != nil {
		if !isNotification {
			writeResponse(errorResponse(req["id"], -32603, err.Error()))
		}
		return
	}
	writeResponse(bytes.TrimSpace(response))

	notif, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	sendMessage(client, url, notif) //nolint:errcheck

	if sessionID != "" {
		go listenSSE(client, url)
	} else {
		log.Printf("[bridge] WARNUNG: Keine Session-ID nach initialize — SSE deaktiviert")
	}
}

func handleToolsList(client *http.Client, url string, req map[string]any, isNotification bool) {
	response, err := fetchAllTools(client, url, req)
	if err != nil {
		if !isNotification {
			writeResponse(errorResponse(req["id"], -32603, err.Error()))
		}
		return
	}

	// Log: wie viele Tools schicken wir zurück
	var resp map[string]any
	if json.Unmarshal(response, &resp) == nil {
		if result, ok := resp["result"].(map[string]any); ok {
			if tools, ok := result["tools"].([]any); ok {
				log.Printf("[bridge] → tools/list Antwort: %d Tools", len(tools))
			}
		}
	}

	writeResponse(response)
}

func handleGeneric(client *http.Client, url string, raw []byte, req map[string]any, isNotification bool) {
	response, err := sendMessage(client, url, raw)
	if err != nil {
		if !isNotification {
			writeResponse(errorResponse(req["id"], -32603, err.Error()))
		}
		return
	}
	trimmed := bytes.TrimSpace(response)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return
	}
	writeResponse(trimmed)
}

// ── SSE Listener ──────────────────────────────────────────────────────────────

func listenSSE(client *http.Client, url string) {
	for {
		err := connectAndReadSSE(client, url)
		if err != nil {
			log.Printf("[bridge/sse] Verbindung unterbrochen: %v — reconnect in 3s", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func connectAndReadSSE(client *http.Client, baseURL string) error {
	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		return fmt.Errorf("request bauen: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	log.Printf("[bridge/sse] verbunden — warte auf Notifications")
	return readSSEStream(client, resp.Body)
}

func readSSEStream(client *http.Client, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, bufferSize), bufferSize)

	for scanner.Scan() {
		line := scanner.Text()
		data, ok := parseSSEDataLine(line)
		if !ok {
			continue
		}
		if !json.Valid([]byte(data)) {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(data), &msg); err == nil {
			if method, _ := msg["method"].(string); method == "notifications/tools/list_changed" {
				log.Printf("[bridge/sse] list_changed empfangen — hole neue Tool-Liste")
				go refreshToolsAndNotify(client)
				continue
			}
		}

		log.Printf("[bridge/sse] → Claude Desktop: %s", data)
		writeResponse([]byte(data))
	}

	return scanner.Err()
}

func refreshToolsAndNotify(client *http.Client) {
	time.Sleep(500 * time.Millisecond)

	req := map[string]any{"jsonrpc": "2.0", "id": 99999, "method": "tools/list"}
	tools, err := fetchAllTools(client, mcpURL, req)
	if err != nil {
		log.Printf("[bridge/sse] tools/list fehlgeschlagen: %v", err)
		notif, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/tools/list_changed",
			"params":  map[string]any{},
		})
		writeResponse(notif)
		return
	}

	var toolsResp map[string]any
	if err := json.Unmarshal(tools, &toolsResp); err != nil {
		return
	}
	result, _ := toolsResp["result"].(map[string]any)
	toolList := result["tools"]

	if tl, ok := toolList.([]any); ok {
		log.Printf("[bridge/sse] → Claude Desktop: list_changed mit %d Tools", len(tl))
	}

	notif, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/tools/list_changed",
		"params":  map[string]any{"tools": toolList},
	})
	writeResponse(notif)
}

// ── Tools Paginierung ─────────────────────────────────────────────────────────

func fetchAllTools(client *http.Client, baseURL string, originalReq map[string]any) ([]byte, error) {
	var allTools []any
	cursor := ""

	for {
		page, err := fetchToolPage(client, baseURL, originalReq, cursor)
		if err != nil {
			return nil, err
		}
		result, _ := page["result"].(map[string]any)
		if tools, ok := result["tools"].([]any); ok {
			allTools = append(allTools, tools...)
		}
		nextCursor, _ := result["nextCursor"].(string)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	log.Printf("[bridge] tools/list: %d Tools total", len(allTools))

	return json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      originalReq["id"],
		"result":  map[string]any{"tools": allTools},
	})
}

func fetchToolPage(client *http.Client, baseURL string, originalReq map[string]any, cursor string) (map[string]any, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      originalReq["id"],
		"method":  "tools/list",
	}
	params := map[string]any{}
	if p, ok := originalReq["params"].(map[string]any); ok {
		for k, v := range p {
			params[k] = v
		}
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if len(params) > 0 {
		req["params"] = params
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	respBytes, err := sendMessage(client, baseURL, reqBytes)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("JSON parse: %w (raw: %.200s)", err, string(respBytes))
	}
	return resp, nil
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

func sendMessage(client *http.Client, baseURL string, msg []byte) ([]byte, error) {
	var lastErr error
	for i := 0; i < 20; i++ {
		req, err := http.NewRequest("POST", baseURL, bytes.NewReader(msg))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if sessionID == "" {
			if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
				sessionID = sid
				log.Printf("[bridge] Session-ID: %s", sessionID)
			}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return extractJSONFromResponse(body), nil
	}
	return nil, fmt.Errorf("OOS nicht erreichbar: %v", lastErr)
}

func extractJSONFromResponse(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if json.Valid(trimmed) {
		return trimmed
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		data, ok := parseSSEDataLine(scanner.Text())
		if !ok {
			continue
		}
		if json.Valid([]byte(data)) {
			return []byte(data)
		}
	}
	return trimmed
}

func parseSSEDataLine(line string) (string, bool) {
	const prefix = "data: "
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if payload == "" || payload == "[DONE]" {
		return "", false
	}
	return payload, true
}

// ── TLS ───────────────────────────────────────────────────────────────────────

func buildTLSClient() (*http.Client, error) {
	// System Keychain — vertraut allen CA-Zertifikaten die der Mac kennt.
	// Die Vault Root CA muss einmalig installiert werden:
	//   sudo security add-trusted-cert -d -r trustRoot \
	//     -k /Library/Keychains/System.keychain oos-ca.pem
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Printf("[bridge] SystemCertPool nicht verfügbar: %v — InsecureSkipVerify", err)
		return insecureTLSClient(), nil
	}
	log.Printf("[bridge] TLS via System Keychain")
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS13,
			},
		},
	}, nil
}

func insecureTLSClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// ── Stdout ────────────────────────────────────────────────────────────────────

func errorResponse(id any, code int, message string) []byte {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
	return data
}

func writeResponse(data []byte) {
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
	os.Stdout.Sync()
}
