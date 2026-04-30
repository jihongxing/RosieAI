package main

import (
	"context"
	"log"
	"net/http"

	"rosie-api/internal/config"
	"rosie-api/internal/httpapi"
	"rosie-api/internal/store"
)

func main() {
	cfg := config.FromEnv()
	repo, cleanup, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	if _, err := repo.UpsertMerchant(cfg.DefaultMerchant()); err != nil {
		log.Fatal(err)
	}

	server := httpapi.NewServer(repo, cfg)
	log.Printf("rosie api-go listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}
