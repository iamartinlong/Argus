package main

import (
	"fmt"
	"os"

	"github.com/iamartinlong/Argus/cmd/argus/cmd"
)

func main() {
	if maybeRunAsService() {
		return
	}
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
