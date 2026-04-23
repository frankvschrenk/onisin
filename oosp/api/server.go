package api


import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"onisin.com/oos-common/dsl"
)

// Services holds all dependencies required by the HTTP handlers.
type Services struct {
	GetAST          func(groups []string) (*dsl.OOSAst, string, bool)
	ExecuteQuery    func(query string) (any, error)
	ExecuteMutation func(statement string) (any, error)
	ExecuteSave     func(ctxName string, data map[string]any) (any, error)
	GetDSL          func(id string) (string, bool, error)
	GetEnvelope     func(id string, content map[string]any) (map[string]any, error)
	Embed           func(text string) ([]float32, error)
	VectorUpsert    func(collection string, id uint64, vector []float32, payload map[string]string) error
	VectorSearch    func(collection string, vector []float32, filter map[string]string, n uint64) (any, error)
	GetTheme        func(variant string) (string, error)
	SetTheme        func(variant, xml string) error
	SchemaSearch    func(query string, n int) (any, error)
	SchemaAll       func() (any, error)

	// Event System API
	EventSearch   func(mapping, query, streamID string, limit int) (any, error)
	EventMappings func() (any, error)
	EventStreams  func(mapping string, limit int) (any, error)
}

type Server struct {
	addr string
	svc  *Services
}

func New(addr string, svc *Services) *Server {
	return &Server{addr: addr, svc: svc}
}

func (s *Server) Start() error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	e.HTTPErrorHandler = jsonErrorHandler

	h := &handler{svc: s.svc}

	e.GET("/health", h.health)
	e.GET("/ast", h.ast)
	e.POST("/query", h.query)
	e.POST("/save", h.save)
	e.POST("/mutation", h.mutation)
	e.GET("/theme", h.theme)
	e.POST("/theme", h.themeSave)
	e.POST("/dsl", h.dsl)
	e.POST("/embed", h.embed)
	e.POST("/vector/upsert", h.vectorUpsert)
	e.POST("/vector/search", h.vectorSearch)
	e.POST("/schema/search", h.schemaSearch)
	e.GET("/schema/all", h.schemaAll)

	// Event System Routes
	e.POST("/event/search", h.eventSearch)
	e.GET("/event/mappings", h.eventMappings)
	e.GET("/event/streams", h.eventStreams)

	log.Printf("[oosp] ✅ REST → %s", s.addr)

	go func() {
		if err := e.Start(s.addr); err != nil && err != http.ErrServerClosed {
			log.Printf("[oosp] server fehler: %v", err)
		}
	}()

	return nil
}

type handler struct {
	svc *Services
}

// errJSON writes a JSON error body under the "error" key and ends the
// request cycle. Use this when the handler itself decides to bail out
// with a specific status code — the nil return value is only safe here
// because the caller will return errJSON's result directly.
func errJSON(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

// jsonErrorHandler normalises Echo's error responses to the same shape
// the rest of the API uses: {"error": "<message>"}. Echo's default
// handler emits {"message": ...}, which is a subtly different shape and
// caused the chat UI to render the raw body including the wrong key
// name. This handler keeps the wire format consistent whether the
// response comes from errJSON or from echo.NewHTTPError (which the
// permission gate uses to abort the handler chain cleanly).
//
// It mirrors Echo's default behaviour in every other respect: honour
// HEAD requests with an empty body, unwrap nested HTTPError messages,
// and fall back to 500 for anything that is not an HTTPError.
func jsonErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	status := http.StatusInternalServerError
	msg := http.StatusText(status)

	if he, ok := err.(*echo.HTTPError); ok {
		status = he.Code
		switch m := he.Message.(type) {
		case string:
			msg = m
		case error:
			msg = m.Error()
		default:
			msg = http.StatusText(status)
		}
	} else if err != nil {
		msg = err.Error()
	}

	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(status)
		return
	}

	if jerr := c.JSON(status, map[string]string{"error": msg}); jerr != nil {
		c.Logger().Error(jerr)
	}
}

func groupsFromCtx(c echo.Context) []string {
	group := c.Request().Header.Get("X-OOS-Group")
	if group != "" {
		return []string{group}
	}
	return []string{"oos-admin"} // Demo: immer admin
}
