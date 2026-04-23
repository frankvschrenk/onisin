package boot

import (
	"log"
	"strings"

	"onisin.com/oos-common/dsl"
	"onisin.com/oos/helper"
)

// connectOOSP establishes a connection to the OOSP backend.
// On success it wires up the AST fetch function so the rest of the app
// can load the schema without knowing the transport details.
func connectOOSP(oospURL string) {
	if helper.UnsecureMode {
		oospURL = toHTTP(oospURL)
	}

	if !helper.ConnectOOSP(oospURL) {
		log.Printf("[oosp] connection failed — starting without OOSP")
		return
	}

	helper.OOSPFetchASTFn = func() (*dsl.OOSAst, error) {
		ast, _, err := helper.OOSP.FetchAST()
		return ast, err
	}
}

func toHTTP(url string) string {
	return strings.Replace(url, "https://", "http://", 1)
}
