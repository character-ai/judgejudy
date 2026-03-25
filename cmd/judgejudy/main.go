package main

import (
	"fmt"
	"os"

	"github.com/character-ai/judgejudy/internal/cli"
	"github.com/joho/godotenv"
)

var version = "0.1.0"

func main() {
	// Load .env file if present (does not override existing env vars).
	_ = godotenv.Load()

	rootCmd := cli.NewRootCmd()
	rootCmd.Version = version

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
