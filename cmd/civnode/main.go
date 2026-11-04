// civnode is a local development replay CLI, not a consensus node.
package main

import (
	"civora/internal/core"
	"civora/internal/devcli"
	"fmt"
	"os"
)

func main() {
	if e := devcli.Run(os.Args[1:], core.New); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
