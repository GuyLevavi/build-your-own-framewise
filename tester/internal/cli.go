package internal

import (
	testerutils "github.com/codecrafters-io/tester-utils"
)

func RunCLI(env map[string]string) int {
	defer installHook()()
	return testerutils.RunCLI(env, testerDefinition)
}
