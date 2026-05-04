package main

import (
	"fmt"
	"os"

	"github.com/AxT-Team/uapi-cli/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if err.Error() == "" {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
