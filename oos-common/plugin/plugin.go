package plugin

// Caller ist das Interface das ein externer Plugin-MCP-Client implementieren muss.
type Caller interface {
	Call(tool string, args map[string]string) (string, error)
}

// HTTPClientFactory wird von außen gesetzt um Circular Imports zu vermeiden.
// helper/dsn.go registriert hier oosp.NewHTTP beim Start.
var HTTPClientFactory func(url string) (Caller, error)

// NewHTTPClient erstellt einen Plugin-Client über die registrierte Factory.
func NewHTTPClient(url string) (Caller, error) {
	if HTTPClientFactory == nil {
		return nil, nil
	}
	return HTTPClientFactory(url)
}
