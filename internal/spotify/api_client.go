package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"playlist-bridge/internal/providerhttp"
)

type Credentials struct {
	ClientID     string
	ClientSecret string
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// RateLimitError сообщает, на сколько секунд Spotify просит отложить запросы.
type RateLimitError struct {
	Reason            string
	RetryAfterSeconds int
}

func (e *RateLimitError) Error() string {
	if e.Reason == "QUOTA_EXCEEDED" {
		if e.RetryAfterSeconds > 0 {
			return fmt.Sprintf("Квота Spotify исчерпана. Повтори через %d сек.", e.RetryAfterSeconds)
		}
		return "Квота Spotify исчерпана."
	}
	if e.RetryAfterSeconds > 0 {
		return fmt.Sprintf("Spotify ограничил частоту запросов: повтори через %d сек.", e.RetryAfterSeconds)
	}
	return "Spotify ограничил частоту запросов. Попробуй позже."
}

// Candidate — структура кандидата с данными, нужными для сравнения и добавления в плейлист.
type Candidate struct {
	Title      string
	Artists    []string
	Album      string
	URI        string
	Explicit   *bool
	DurationMs int    `json:"-"`
	ISRC       string `json:"-"`
}

type trackMetadata struct {
	DurationMs  int `json:"duration_ms"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

// Внутренняя структура под формат JSON самого Spotify.
type searchResponse struct {
	Tracks struct {
		Items []struct {
			Name     string `json:"name"`
			URI      string `json:"uri"`
			Explicit *bool  `json:"explicit"`
			trackMetadata
			Album struct {
				Name string `json:"name"`
			} `json:"album"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
		} `json:"items"`
	} `json:"tracks"`
}

func GetToken(credentials Credentials) (string, error) {
	formValues := url.Values{}
	formValues.Set("grant_type", "client_credentials")
	formValues.Set("client_id", credentials.ClientID)
	formValues.Set("client_secret", credentials.ClientSecret)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.PostForm(
		"https://accounts.spotify.com/api/token",
		formValues,
	)
	if err != nil {
		return "", fmt.Errorf("отправить запрос токена: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Spotify вернул статус: %s", resp.Status)
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("прочитать ответ Spotify: %w", err)
	}

	if token.AccessToken == "" {
		return "", fmt.Errorf("Spotify вернул пустой токен")
	}

	return token.AccessToken, nil
}

func SearchTrack(ctx context.Context, token, title, artist string) ([]Candidate, error) {
	return SearchTrackQuery(ctx, token, TrackSearchQuery(title, artist))
}

// TrackSearchQuery составляет поисковый запрос по названию и исполнителю.
func TrackSearchQuery(title, artist string) string {
	return "track:" + strings.TrimSpace(title) + " artist:" + strings.TrimSpace(artist)
}

// SearchTrackQuery выполняет поиск треков по готовому запросу Spotify.
func SearchTrackQuery(ctx context.Context, token, query string) ([]Candidate, error) {
	queryParams := url.Values{}
	queryParams.Set("q", query)
	queryParams.Set("type", "track")
	queryParams.Set("limit", "10")
	requestURL := "https://api.spotify.com/v1/search?" + queryParams.Encode()

	var parsed searchResponse
	err := getJSON(ctx, token, requestURL, &parsed)
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(parsed.Tracks.Items))
	for _, item := range parsed.Tracks.Items {
		artists := make([]string, 0, len(item.Artists))
		for _, a := range item.Artists {
			artists = append(artists, a.Name)
		}

		candidates = append(candidates, Candidate{
			Title:      item.Name,
			Artists:    artists,
			Album:      item.Album.Name,
			URI:        item.URI,
			Explicit:   item.Explicit,
			DurationMs: item.DurationMs,
			ISRC:       item.ExternalIDs.ISRC,
		})
	}

	return candidates, nil
}

type HTTPStatusError struct {
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("Spotify вернул статус: %s", e.Status)
}

var apiClient = &http.Client{Timeout: 10 * time.Second}

func getJSON(ctx context.Context, token, requestURL string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("создать запрос: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := providerhttp.Do(apiClient, req)
	if err != nil {
		return fmt.Errorf("отправить запрос к Spotify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		var body struct {
			Error struct {
				Reason string
			}
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
		return &RateLimitError{RetryAfterSeconds: max(0, seconds), Reason: body.Error.Reason}
	}
	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{StatusCode: resp.StatusCode, Status: resp.Status}
	}

	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		return fmt.Errorf("прочитать ответ Spotify: %w", err)
	}
	return nil
}
