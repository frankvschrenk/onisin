// node.go — Re-Export von onisin.com/oos-dsl für den Fyne-Builder.
//
// Der Node-Typ und alle Konstanten leben jetzt in oos-dsl.
// Dieses File stellt Typ-Aliase bereit damit bestehender Code im
// dsl-Package ohne Änderungen weiterläuft.
package dsl

import base "onisin.com/oos-dsl-base/base"

// Typ-Aliase — der Rest des Packages nutzt diese Namen direkt.
type Node = base.Node
type NodeType = base.NodeType

const (
	NodeScreen   = base.NodeScreen
	NodeBox      = base.NodeBox
	NodeGrid     = base.NodeGrid
	NodeGridWrap = base.NodeGridWrap
	NodeBorder   = base.NodeBorder
	NodeCenter   = base.NodeCenter
	NodeStack    = base.NodeStack
	NodeTabs     = base.NodeTabs
	NodeTab      = base.NodeTab
	NodeSection  = base.NodeSection
	NodeField    = base.NodeField

	NodeLabel    = base.NodeLabel
	NodeButton   = base.NodeButton
	NodeEntry    = base.NodeEntry
	NodeTextArea = base.NodeTextArea
	NodeChoices  = base.NodeChoices
	NodeCheck    = base.NodeCheck
	NodeRadio    = base.NodeRadio
	NodeOption   = base.NodeOption
	NodeProgress = base.NodeProgress
	NodeToolbar  = base.NodeToolbar
	NodeSep      = base.NodeSep
	NodeCard     = base.NodeCard
	NodeForm     = base.NodeForm

	NodeAccordion     = base.NodeAccordion
	NodeAccordionItem = base.NodeAccordionItem
	NodeSlider        = base.NodeSlider
	NodeHyperlink     = base.NodeHyperlink
	NodeIcon          = base.NodeIcon
	NodeRichText      = base.NodeRichText
	NodeSpan          = base.NodeSpan

	NodeTable   = base.NodeTable
	NodeColumn  = base.NodeColumn
	NodeList    = base.NodeList
	NodeTree    = base.NodeTree
	NodeNode    = base.NodeNode
	NodeUnknown = base.NodeUnknown
)
