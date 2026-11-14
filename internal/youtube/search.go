package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"playlist-bridge/internal/providerhttp"
)

var playlistIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,128}$`)

// PlaylistIDFromURL извлекает ID из ссылки на плейлист YouTube или YouTube Music.
func PlaylistIDFromURL(link string) (string, error) {
	invalidLink := fmt.Errorf("вставь ссылку на плейлист YouTube или YouTube Music")
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", invalidLink
	}
	switch strings.ToLower(u.Hostname()) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path != "/playlist" && u.Path != "/watch" {
			return "", invalidLink
		}
	case "youtu.be":
	default:
		return "", invalidLink
	}
	id := u.Query().Get("list")
	if !playlistIDPattern.MatchString(id) {
		return "", invalidLink
	}
	return id, nil
}

type Video struct {
	Title        string
	VideoId      string
	ChannelTitle string
}

type playlistResponse struct {
	NextPageToken string `json:"nextPageToken"`
	Items         []struct {
		Snippet struct {
			Title                  string `json:"title"`
			VideoOwnerChannelTitle string `json:"videoOwnerChannelTitle"`
			ResourceId             struct {
				VideoId string `json:"videoId"`
			} `json:"resourceId"`
		} `json:"snippet"`
	} `json:"items"`
}

func GetPlaylistVideos(apiKey, playlistID string) ([]Video, error) {
	return GetPlaylistVideosContext(context.Background(), apiKey, playlistID)
}

func GetPlaylistVideosContext(ctx context.Context, apiKey, playlistID string) ([]Video, error) {
	return getPlaylistVideos(ctx, apiKey, "", playlistID)
}

// GetAccountPlaylistVideos читает плейлист с правами подключённого канала.
func GetAccountPlaylistVideos(ctx context.Context, token, playlistID string) ([]Video, error) {
	if token == "" {
		return nil, fmt.Errorf("подключи Google для чтения плейлиста")
	}
	return getPlaylistVideos(ctx, "", token, playlistID)
}

func getPlaylistVideos(ctx context.Context, apiKey, token, playlistID string) ([]Video, error) {
	queryParams := url.Values{}
	queryParams.Set("part", "snippet")
	queryParams.Set("playlistId", playlistID)
	if apiKey != "" {
		queryParams.Set("key", apiKey)
	}
	queryParams.Set("maxResults", "50")

	var videos []Video
	for {
		requestURL := "https://www.googleapis.com/youtube/v3/playlistItems?" + queryParams.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, fmt.Errorf("не удалось создать запрос к YouTube")
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := providerhttp.Do(apiClient, req)
		if err != nil {
			return nil, fmt.Errorf("не удалось отправить запрос к YouTube")
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("YouTube вернул статус: %s", resp.Status)
		}

		var result playlistResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("прочитать ответ YouTube: %w", err)
		}

		for _, item := range result.Items {
			videos = append(videos, Video{
				Title:        item.Snippet.Title,
				VideoId:      item.Snippet.ResourceId.VideoId,
				ChannelTitle: item.Snippet.VideoOwnerChannelTitle,
			})
		}
		if result.NextPageToken == "" {
			break
		}
		queryParams.Set("pageToken", result.NextPageToken)
	}
	return videos, nil
}
