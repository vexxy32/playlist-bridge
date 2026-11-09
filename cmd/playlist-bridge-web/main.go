package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"playlist-bridge/internal/server"
)

func main() {
	_ = godotenv.Load()
	clientID := strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		log.Fatal("Укажи SPOTIFY_CLIENT_ID и SPOTIFY_CLIENT_SECRET в .env")
	}
	if _, err := os.Stat("web/dist/index.html"); err != nil {
		log.Fatal("Не найдена страница web/dist. Выполни npm.cmd run build в папке web")
	}
	app := server.New(clientID, clientSecret)
	httpServer := &http.Server{Addr: "127.0.0.1:8080", Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Println("Открой http://127.0.0.1:8080/")
	log.Fatal(httpServer.ListenAndServe())
}
