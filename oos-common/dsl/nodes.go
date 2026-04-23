package dsl

// DSLFile ist das Ergebnis einer geparsten XML-Datei.
type DSLFile struct {
	Filename string
	CTX      *CTXFile
}
