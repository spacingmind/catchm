package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: catchm serve")
		os.Exit(2)
	}
	fmt.Println("CatchM — catch the meeting, keep the mind. (daemon not yet implemented)")
}
