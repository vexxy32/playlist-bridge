package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"playlist-bridge/internal/spotify"
	"playlist-bridge/internal/transfer"
)

func main() {
	_ = godotenv.Load()
	creds := spotify.Credentials{
		ClientID:     strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_SECRET")),
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		log.Fatal("Укажи SPOTIFY_CLIENT_ID и SPOTIFY_CLIENT_SECRET в .env")
	}

	jsonData, err := os.ReadFile("tracks.json")
	if err != nil {
		log.Fatalf("Ошибка чтения файла с треками: %v", err)
	}

	tracks, err := transfer.ParseTracks(jsonData)
	if err != nil {
		log.Fatalf("Ошибка в формате входных данных: %v", err)
	}

	token, err := spotify.GetToken(creds)
	if err != nil {
		log.Fatalf("Ошибка получения токена: %v", err)
	}

	results, err := transfer.TransferTracks(context.Background(), tracks, transfer.SearchSpotify(token))
	if err != nil {
		log.Fatalf("Поиск прерван из-за ошибки запроса: %v", err)
	}

	for _, r := range results {
		if r.Found {
			fmt.Printf("[НАЙДЕН]    %s — %s -> %s\n", r.Track.Artist, r.Track.Title, r.URI)
		} else {
			fmt.Printf("[НЕ НАЙДЕН] %s — %s\n", r.Track.Artist, r.Track.Title)
		}
	}
}
