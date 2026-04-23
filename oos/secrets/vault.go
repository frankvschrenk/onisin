package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type VaultSource struct {
	URL       string
	Token     string
	MountPath string
	Path      string
	client    *http.Client
}

func NewVaultSource(url, token, mountPath, path string) *VaultSource {
	return &VaultSource{
		URL:       url,
		Token:     token,
		MountPath: mountPath,
		Path:      path,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *VaultSource) kvURL() string {
	return fmt.Sprintf("%s/v1/%s/data/%s", s.URL, s.MountPath, s.Path)
}

func (s *VaultSource) readAll() (map[string]string, error) {
	req, err := http.NewRequest("GET", s.kvURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", s.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return map[string]string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault read: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("vault read: decode: %w", err)
	}
	return result.Data.Data, nil
}

func (s *VaultSource) writeAll(data map[string]string) error {
	body, err := json.Marshal(map[string]interface{}{
		"data": data,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", s.kvURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", s.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault write: HTTP %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (s *VaultSource) Get(key string) (string, error) {
	data, err := s.readAll()
	if err != nil {
		return "", fmt.Errorf("vault get %q: %w", key, err)
	}
	val, ok := data[key]
	if !ok || val == "" {
		return "", &ErrNotFound{Key: key}
	}
	return val, nil
}

func (s *VaultSource) Set(key string, value string) error {
	data, err := s.readAll()
	if err != nil {
		return fmt.Errorf("vault set %q: %w", key, err)
	}
	data[key] = value
	if err := s.writeAll(data); err != nil {
		return fmt.Errorf("vault set %q: %w", key, err)
	}
	return nil
}

func (s *VaultSource) Delete(key string) error {
	data, err := s.readAll()
	if err != nil {
		return fmt.Errorf("vault delete %q: %w", key, err)
	}
	delete(data, key)
	if err := s.writeAll(data); err != nil {
		return fmt.Errorf("vault delete %q: %w", key, err)
	}
	return nil
}

func (s *VaultSource) Exists(key string) (bool, error) {
	_, err := s.Get(key)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}
