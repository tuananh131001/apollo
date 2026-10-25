// Command cli is the apollo command-line tool.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tuananh131001/apollo/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	fmt.Fprintln(os.Stderr, "usage: cli [-version]")
	os.Exit(2)
}
