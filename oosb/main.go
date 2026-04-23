package main

import (
	"flag"
	"fmt"
	"os"
)

var VERSION = ""

func main() {
	url := flag.String("url", "", "OOS MCP URL (default: https://localhost:59124/mcp)")
	flag.Parse()

	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(VERSION)
		return
	}

	Run(*url)
}
