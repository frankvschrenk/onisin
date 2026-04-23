package helper

import (
	"encoding/json"
	"time"
)

type CachedSession struct {
	IDToken string                 `json:"id_token"`
	Claims  map[string]interface{} `json:"claims"`
}

func (s *CachedSession) IsExpired() bool {
	exp, ok := s.Claims["exp"].(float64)
	if !ok {
		return true 
	}
	expTime := time.Unix(int64(exp), 0)
	return time.Now().After(expTime.Add(-5 * time.Minute))
}

func JWTIsValid(token string) bool {
	claims, err := decodeJWTClaims(token)
	if err != nil {
		return false
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return false
	}
	expTime := time.Unix(int64(exp), 0)
	return time.Now().Before(expTime.Add(-5 * time.Minute))
}

func DecodeSession(raw string) (*CachedSession, error) {
	var s CachedSession
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func EncodeSession(idToken string, claims map[string]interface{}) (string, error) {
	s := CachedSession{IDToken: idToken, Claims: claims}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}


