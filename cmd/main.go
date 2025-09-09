package main

import (
	"log"

	"github.com/BIwashi/candecode/app/convert"
	"github.com/BIwashi/candecode/pkg/cli"
)

func main() {
	c := cli.NewCLI(
		"candecode",
		"Convert CAN data captured with pcapng to MCAP using a DBC file.",
	)

	c.AddCommands(
		convert.NewCommand(),
		// gen.NewCommand(),
	)

	if err := c.Run(); err != nil {
		log.Fatal(err)
	}
}
