package main

import (
	"fmt"
	"os"

	"github.com/kohofinancial/dependabot-pr-checker/cmd"
)

// Variable for os.Exit to allow overriding in tests
var osExit = os.Exit

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}
