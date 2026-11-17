// Command certgraft serves upstream container images with the workstation's extra CA roots added.
package main

import (
	"github.com/sikhlana/pkg/cli"

	"github.com/sikhlana/certgraft/cmd"
)

func main() {
	cli.Main(cmd.Root)
}
