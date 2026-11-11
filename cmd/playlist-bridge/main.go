package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"playlist-bridge/internal/spotify"
	"playlist-bridge/internal/transfer"
	"playlist-bridge/internal/youtube"
)

func main() {
	playlistID := flag.String("youtube-playlist", "", "ID публичного плейлиста YouTube")
	searchMode := flag.String("search-mode", string(transfer.SpotifySearchStandard), "Режим поиска: standard, broad-first, reuse-candidates")
	flag.Parse()
	mode := transfer.SpotifySearchMode(*searchMode)
	switch mode {
	case transfer.SpotifySearchStandard, transfer.SpotifySearchBroadFirst, transfer.SpotifySearchReuseCandidates:
	default:
		log.Fatalf("Неизвестный режим поиска %q. Доступные варианты: standard, broad-first, reuse-candidates", mode)
	}
	_ = godotenv.Load()
	creds := spotify.Credentials{
		ClientID:     strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_SECRET")),
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		log.Fatal("Укажи SPOTIFY_CLIENT_ID и SPOTIFY_CLIENT_SECRET в .env")
	}

	var tracks []transfer.Track
	if *playlistID != "" {
		key := strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY"))
		if key == "" {
			log.Fatal("Укажи YOUTUBE_API_KEY в .env")
		}
		videos, err := youtube.GetPlaylistVideos(key, *playlistID)
		if err != nil {
			log.Fatalf("Ошибка получения плейлиста YouTube: %v", err)
		}
		tracks = transfer.TracksFromYouTube(videos)
	} else {
		jsonData, err := os.ReadFile("tracks.json")
		if err != nil {
			log.Fatalf("Ошибка чтения файла с треками: %v", err)
		}
		tracks, err = transfer.ParseTracks(jsonData)
		if err != nil {
			log.Fatalf("Ошибка в формате входных данных: %v", err)
		}
	}

	token, err := spotify.GetToken(creds)
	if err != nil {
		log.Fatalf("Ошибка получения токена: %v", err)
	}

	search, searchStats := transfer.NewSpotifySearch(token, mode)
	started := time.Now()
	results, err := transfer.TransferTracks(context.Background(), tracks, search)
	elapsed := time.Since(started).Round(time.Millisecond)
	stats := searchStats()
	fmt.Printf("Поиск Spotify (%s), %d записей, %s. Запросов: %d (первых %d, дополнительных %d). Повторно использовано ответов: %d.\n",
		mode, len(tracks), elapsed, stats.FirstRequests+stats.FallbackRequests, stats.FirstRequests, stats.FallbackRequests, stats.SharedQueries)
	fmt.Printf("Среди уникальных записей найдено с первого запроса: %d, с дополнительного: %d, из ответа на другой трек: %d.\n",
		stats.FirstMatches, stats.FallbackMatches, stats.ReusedMatches)
	if err != nil {
		log.Fatalf("Поиск прерван из-за ошибки запроса: %v", err)
	}

	matched := 0
	for _, r := range results {
		if r.Found {
			matched++
			fmt.Printf("[НАЙДЕН]    %s — %s -> %s\n", r.Track.Artist, r.Track.Title, r.URI)
		} else {
			fmt.Printf("[НЕ НАЙДЕН] %s — %s\n", r.Track.Artist, r.Track.Title)
		}
	}
	fmt.Printf("Всего найдено %d из %d, не найдено %d.\n", matched, len(results), len(results)-matched)
}
