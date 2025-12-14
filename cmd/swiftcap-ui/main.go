package main

import (
	"log"

	"swiftcap/internal/uiapp"
)

func main() {
	if err := uiapp.Run(); err != nil {
		log.Fatalf("swiftcap-ui failed: %v", err)
	}
}
