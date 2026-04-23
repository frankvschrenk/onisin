package secrets

import "fmt"

type Source interface {
	Get(key string) (string, error)
	Set(key string, value string) error
	Delete(key string) error
	Exists(key string) (bool, error)
}

type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("secrets: key %q not found", e.Key)
}

func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}

type ErrReadOnly struct {
	Backend string
}

func (e *ErrReadOnly) Error() string {
	return fmt.Sprintf("secrets: backend %q is read-only", e.Backend)
}
