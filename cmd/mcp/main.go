package main

import (
	"context"
	"flag"
	"log"

	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/startup"
)

func main() {
	cfgPath := flag.String("config", "pali.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	runtime, err := startup.NewMCPRuntime(cfg)
	if err != nil {
		log.Fatalf("build mcp runtime: %v", err)
	}
	defer runtime.Cleanup()

	log.Printf("starting pali mcp server over stdio")
	if err := runtime.Server.RunStdio(context.Background()); err != nil {
		log.Fatalf("mcp server exited: %v", err)
	}
}
