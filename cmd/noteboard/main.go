package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kayushkin/noteboard/internal/api"
	"github.com/kayushkin/noteboard/internal/db"
)

func main() {
	port := os.Getenv("NOTEBOARD_PORT")
	if port == "" {
		port = "8191"
	}

	dbPath := os.Getenv("NOTEBOARD_DB")
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, ".noteboard", "noteboard.db")
	}

	store, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()

	a := api.New(store)
	addr := fmt.Sprintf(":%s", port)
	log.Printf("noteboard listening on %s (db: %s)", addr, dbPath)
	log.Fatal(http.ListenAndServe(addr, a.Handler()))
}
