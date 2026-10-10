package main

import (
	"aide/internal/server"
	"log"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--knowledge-go-types" {
		if err := server.RunGoTypeHelper(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
