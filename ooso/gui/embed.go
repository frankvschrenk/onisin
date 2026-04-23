package gui

// embed.go — eingebettete Testdaten für DSL-Preview und Theme-Preview.

import _ "embed"

//go:embed testdata/person-detail.xml
var personDetailDSL []byte

//go:embed testdata/person-detail.json
var personDetailJSON []byte

//go:embed testdata/user-list.xml
var userListDSL []byte

//go:embed testdata/user-list.json
var userListJSON []byte

//go:embed testdata/user-profile.xml
var userProfileDSL []byte

//go:embed testdata/user-profile.json
var userProfileJSON []byte

// testScreens ist die geordnete Liste der eingebetteten Testscreens.
type testScreen struct {
	name string
	dsl  []byte
	data []byte
}

var testScreens = []testScreen{
	{"person-detail", personDetailDSL, personDetailJSON},
	{"user-list", userListDSL, userListJSON},
	{"user-profile", userProfileDSL, userProfileJSON},
}
