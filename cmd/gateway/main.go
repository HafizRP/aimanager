package main

import (
	"os"

	"9router-gateway/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		os.Exit(1)
	}
}