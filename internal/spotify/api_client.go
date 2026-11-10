package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Credentials struct {
	ClientID     string
	ClientSecret string
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Candidate — структура кандидата с данными, нужными для сравнения и добавления в плейлист.
type Candidate struct {
	Title   string
	Artists []string
	Album   string
	URI     string
}

// Внутренняя структура под формат JSON самого Spotify.
type searchResponse struct {
	Tracks struct {
		Items []struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
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
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", credentials.ClientID)
	data.Set("client_secret", credentials.ClientSecret)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	response, err := client.PostForm(
		"https://accounts.spotify.com/api/token",
		data,
	)
	if err != nil {
		return "", fmt.Errorf("отправить запрос токена: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Spotify вернул статус: %s", response.Status)
	}

	var token tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("прочитать ответ Spotify: %w", err)
	}

	if token.AccessToken == "" {
		return "", fmt.Errorf("Spotify вернул пустой токен")
	}

	return token.AccessToken, nil
}

func SearchTrack(ctx context.Context, token, title, artist string) ([]Candidate, error) {
	params := url.Values{}
	params.Set("q", "track:"+strings.TrimSpace(title)+" artist:"+strings.TrimSpace(artist))
	params.Set("type", "track")
	params.Set("limit", "10")

	address := "https://api.spotify.com/v1/search?" + params.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("создать запрос: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	var parsed searchResponse
	for attempt := 0; ; attempt++ {
		response, err := client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("отправить запрос: %w", err)
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			response.Body.Close()
			if err != nil || seconds < 1 {
				seconds = 2
			}
			if seconds > 30 {
				return nil, fmt.Errorf("Spotify ограничил частоту запросов: повтори через %d сек.", seconds)
			}
			timer := time.NewTimer(time.Duration(seconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("Spotify вернул статус: %s", response.Status)
		}
		err = json.NewDecoder(response.Body).Decode(&parsed)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("прочитать ответ Spotify: %w", err)
		}
		break
	}

	candidates := make([]Candidate, 0, len(parsed.Tracks.Items))
	for _, item := range parsed.Tracks.Items {
		artists := make([]string, 0, len(item.Artists))
		for _, a := range item.Artists {
			artists = append(artists, a.Name)
		}

		candidates = append(candidates, Candidate{
			Title:   item.Name,
			Artists: artists,
			Album:   item.Album.Name,
			URI:     item.URI,
		})
	}

	return candidates, nil
}
