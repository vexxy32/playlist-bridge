package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"playlist-bridge/internal/providerhttp"
)

type PlaylistInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ImageURL string `json:"imageUrl"`
}

func GetUserPlaylists(token string) ([]PlaylistInfo, error) {
	return GetUserPlaylistsContext(context.Background(), token)
}

func GetUserPlaylistsContext(ctx context.Context, token string) ([]PlaylistInfo, error) {
	queryParams := url.Values{
		"part":       {"snippet"},
		"mine":       {"true"},
		"maxResults": {"50"},
	}
	playlists := make([]PlaylistInfo, 0)
	for {
		requestURL := "https://www.googleapis.com/youtube/v3/playlists?" + queryParams.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("создать запрос: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := providerhttp.Do(apiClient, req)
		if err != nil {
			return nil, fmt.Errorf("получить плейлисты YouTube: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("получить плейлисты YouTube: %s", resp.Status)
		}
		var result struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				ID      string `json:"id"`
				Snippet struct {
					Title      string `json:"title"`
					Thumbnails map[string]struct {
						URL string `json:"url"`
					} `json:"thumbnails"`
				} `json:"snippet"`
			} `json:"items"`
		}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("прочитать ответ YouTube: %w", err)
		}
		for _, item := range result.Items {
			if item.Snippet.Title == "" || item.ID == "" {
				continue
			}
			playlists = append(playlists, PlaylistInfo{ID: item.ID, Title: item.Snippet.Title, ImageURL: thumbnailURL(item.Snippet.Thumbnails)})
		}
		if result.NextPageToken == "" {
			break
		}
		if result.NextPageToken == queryParams.Get("pageToken") {
			return nil, fmt.Errorf("YouTube вернул повторный токен страницы")
		}
		queryParams.Set("pageToken", result.NextPageToken)
	}
	return playlists, nil
}

// ProfileInfo содержит название и аватарку канала подключённого пользователя.
type ProfileInfo struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
}

func thumbnailURL(images map[string]struct {
	URL string `json:"url"`
}) string {
	for _, size := range []string{"medium", "high", "default"} {
		if image := images[size]; image.URL != "" {
			return image.URL
		}
	}
	return ""
}

// GetUserProfileContext получает данные канала, связанного с авторизацией Google.
func GetUserProfileContext(ctx context.Context, token string) (ProfileInfo, error) {
	queryParams := url.Values{
		"part": {"snippet"},
		"mine": {"true"},
	}
	requestURL := "https://www.googleapis.com/youtube/v3/channels?" + queryParams.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return ProfileInfo{}, fmt.Errorf("создать запрос канала: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := providerhttp.Do(apiClient, req)
	if err != nil {
		return ProfileInfo{}, fmt.Errorf("получить канал YouTube: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProfileInfo{}, fmt.Errorf("получить канал YouTube: %s", resp.Status)
	}
	var result struct {
		Items []struct {
			Snippet struct {
				Title      string `json:"title"`
				Thumbnails map[string]struct {
					URL string `json:"url"`
				} `json:"thumbnails"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ProfileInfo{}, fmt.Errorf("прочитать канал YouTube: %w", err)
	}
	if len(result.Items) == 0 {
		return ProfileInfo{}, fmt.Errorf("канал YouTube не найден")
	}
	profile := result.Items[0].Snippet
	return ProfileInfo{Name: profile.Title, ImageURL: thumbnailURL(profile.Thumbnails)}, nil
}
