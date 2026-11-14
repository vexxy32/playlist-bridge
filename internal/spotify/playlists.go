package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"playlist-bridge/internal/providerhttp"
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
	payload := createPlaylistRequest{
		Name:        name,
		Description: description,
		Public:      public,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Playlist{}, fmt.Errorf("подготовить тело запроса: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.spotify.com/v1/me/playlists", bytes.NewReader(body))

	if err != nil {
		return Playlist{}, fmt.Errorf("создать запрос: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := providerhttp.Do(apiClient, req)
	if err != nil {
		return Playlist{}, fmt.Errorf("отправить запрос: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return Playlist{}, fmt.Errorf("Spotify вернул статус: %s", resp.Status)
	}
	var playlist Playlist
	if err := json.NewDecoder(resp.Body).Decode(&playlist); err != nil {
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

	requestURL := "https://api.spotify.com/v1/playlists/" + url.PathEscape(playlistID) + "/items"
	for start := 0; start < len(uris); start += 100 {
		end := min(start+100, len(uris))
		body, err := json.Marshal(addTracksRequest{URIs: uris[start:end]})
		if err != nil {
			return fmt.Errorf("подготовить тело запроса: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("создать запрос: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := providerhttp.Do(apiClient, req)
		if err != nil {
			return fmt.Errorf("добавлено %d из %d треков; запрос: %w", start, len(uris), err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("добавлено %d из %d треков; Spotify вернул статус: %s", start, len(uris), resp.Status)
		}
	}
	return nil
}
