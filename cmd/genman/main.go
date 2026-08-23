package main

import (
	"log"
	"os"

	"github.com/dedomorozoff/dmsh/internal/cli"
	"github.com/spf13/cobra/doc"
)

func main() {
	cmd := cli.NewRootCmd()
	header := &doc.GenManHeader{
		Title:   "DMSH",
		Section: "1",
		Source:  "Direct Model Shell",
		Manual:  "dmsh Manual",
	}

	err := os.MkdirAll("man", 0755)
	if err != nil {
		log.Fatalf("failed to create directory: %v", err)
	}

	err = doc.GenManTree(cmd, header, "man")
	if err != nil {
		log.Fatalf("failed to generate man pages: %v", err)
	}
}
