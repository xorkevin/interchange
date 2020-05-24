package main

import (
	"fmt"
	"github.com/spf13/pflag"
)

var (
	argPort    int
	argTarget  string
	argVerbose bool
	argHelp    bool
)

func main() {
	pflag.IntVarP(&argPort, "port", "p", 0, "listen port for traffic")
	pflag.StringVarP(&argTarget, "target", "t", "", "destination port for traffic (HOST:PORT)")
	pflag.BoolVarP(&argVerbose, "verbose", "v", false, "enable verbose logging")
	pflag.BoolVarP(&argHelp, "help", "h", false, "print this help")
	pflag.Parse()

	if argHelp {
		pflag.Usage()
		return
	}

	fmt.Println(argPort, argTarget)
}
