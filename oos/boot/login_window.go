package boot

import (
	"onisin.com/oos/helper"
)

func runPKCELogin() (*helper.AuthResult, error) {
	return helper.IamLogin("", "")
}
