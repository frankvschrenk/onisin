package main

import (
	"onisin.com/oos/boot"
	"onisin.com/oos/helper"
)

func main() {
	helper.Meta = helper.InitMeta()
	helper.Meta.Version = VERSION

	if !helper.OOSMode {
		return
	}

	boot.StartFyneApp()
}
