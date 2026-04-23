package secrets

import (
	"os"
	"strings"
)

type EnvSource struct {
	Prefix string
}

func NewEnvSource(prefix string) *EnvSource {
	return &EnvSource{Prefix: prefix}
}

func (s *EnvSource) envKey(key string) string {
	k := strings.ReplaceAll(key, "/", "_")
	k = strings.ReplaceAll(k, ".", "_")
	k = strings.ToUpper(k)
	if s.Prefix != "" {
		k = strings.ToUpper(s.Prefix) + "_" + k
	}
	return k
}

func (s *EnvSource) Get(key string) (string, error) {
	val := os.Getenv(s.envKey(key))
	if val == "" {
		return "", &ErrNotFound{Key: key}
	}
	return val, nil
}

func (s *EnvSource) Set(key string, value string) error {
	return os.Setenv(s.envKey(key), value)
}

func (s *EnvSource) Delete(key string) error {
	return os.Unsetenv(s.envKey(key))
}

func (s *EnvSource) Exists(key string) (bool, error) {
	_, ok := os.LookupEnv(s.envKey(key))
	return ok, nil
}
