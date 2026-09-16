package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"ticket-system/internal/auth"
	"ticket-system/internal/handlers"
	"ticket-system/internal/middleware"
	"ticket-system/internal/store"
)

func main() {
	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "tickets.db")
	jwtSecret := getEnv("JWT_SECRET", "dev-secret-change-me")

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	tokens := auth.NewTokenIssuer(jwtSecret, 24*time.Hour)
	api := handlers.NewAPI(db, tokens)
	requireAuth := middleware.RequireAuth(tokens)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.Health)
	mux.HandleFunc("POST /auth/register", api.Register)
	mux.HandleFunc("POST /auth/login", api.Login)

	mux.Handle("POST /tickets", requireAuth(http.HandlerFunc(api.CreateTicket)))
	mux.Handle("GET /tickets", requireAuth(http.HandlerFunc(api.ListTickets)))
	mux.Handle("GET /tickets/{id}", requireAuth(http.HandlerFunc(api.GetTicket)))
	mux.Handle("PATCH /tickets/{id}/status", requireAuth(http.HandlerFunc(api.UpdateTicketStatus)))

	addr := ":" + port
	log.Printf("ticket-system listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
