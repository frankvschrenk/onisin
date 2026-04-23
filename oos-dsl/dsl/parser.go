// parser.go — Re-Export von onisin.com/oos-dsl für den Fyne-Builder.
package dsl

import (
	"io"

	base "onisin.com/oos-dsl-base/base"
)

// Parse liest einen XML-Stream und gibt den Node-Baum zurück.
// Delegiert an onsin.com/oos-dsl — kein doppelter Code.
func Parse(r io.Reader) (*Node, error) {
	return base.Parse(r)
}
