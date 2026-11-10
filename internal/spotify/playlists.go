package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Playlist struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URI          string `json:"uri"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

type createPlaylistRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Public      bool   `json:"public"`
}
type addTracksRequest struct {
	URIs []string `json:"uris"`
}

func CreatePlaylist(ctx context.Context, token, name, description string, public bool) (Playlist, error) {
	requestBody := createPlaylistRequest{
		Name:        name,
		Description: description,
		Public:      public,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return Playlist{}, fmt.Errorf("подготовить тело запроса: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.spotify.com/v1/me/playlists", bytes.NewBuffer(bodyBytes))

	if err != nil {
		return Playlist{}, fmt.Errorf("создать запрос: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	response, err := client.Do(request)
	if err != nil {
		return Playlist{}, fmt.Errorf("отправить запрос: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return Playlist{}, fmt.Errorf("Spotify вернул статус: %s", response.Status)
	}
	var playlist Playlist
	if err := json.NewDecoder(response.Body).Decode(&playlist); err != nil {
		return Playlist{}, fmt.Errorf("прочитать ответ Spotify: %w", err)
	}
	if playlist.ID == "" {
		return Playlist{}, fmt.Errorf("Spotify вернул пустой ID плейлиста")
	}
	return playlist, nil
}

func AddTrack(ctx context.Context, token, playlistID string, uris []string) error {
	if len(uris) == 0 {
		return nil
	}

	address := "https://api.spotify.com/v1/playlists/" + url.PathEscape(playlistID) + "/items"
	client := &http.Client{Timeout: 10 * time.Second}
	for start := 0; start < len(uris); start += 100 {
		end := min(start+100, len(uris))
		bodyBytes, err := json.Marshal(addTracksRequest{URIs: uris[start:end]})
		if err != nil {
			return fmt.Errorf("подготовить тело запроса: %w", err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return fmt.Errorf("создать запрос: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("добавлено %d из %d треков; запрос: %w", start, len(uris), err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
			return fmt.Errorf("добавлено %d из %d треков; Spotify вернул статус: %s", start, len(uris), response.Status)
		}
	}
	return nil
}
