package main

import (
	"fmt"
	"os"

	"xorkevin.dev/interchange/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
