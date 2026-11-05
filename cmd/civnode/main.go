// civnode is a local development replay CLI, not a consensus node.
package main

import (
	"civora/internal/citizencli"
	"civora/internal/core"
	"civora/internal/devcli"
	"fmt"
	"os"
)

func main() {
	var e error
	if len(os.Args) > 1 && os.Args[1] == "replay" {
		e = devcli.Run(os.Args[1:], core.New)
	} else {
		e = citizencli.Run(os.Args[1:])
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
