package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"playlist-bridge/internal/providerhttp"
)

// SearchVideos ищет кандидатов в YouTube, не предполагая что каждый ролик — музыка.
func SearchVideos(ctx context.Context, apiKey, query string) ([]Video, error) {
	queryParams := url.Values{
		"part":            {"snippet"},
		"type":            {"video"},
		"videoCategoryId": {"10"},
		"maxResults":      {"10"},
		"q":               {query},
		"key":             {apiKey},
	}
	requestURL := "https://www.googleapis.com/youtube/v3/search?" + queryParams.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := providerhttp.Do(apiClient, req)
	if err != nil {
		return nil, fmt.Errorf("не удалось отправить запрос поиска к YouTube")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("поиск YouTube: %s", resp.Status)
	}
	var result struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	videos := make([]Video, 0, len(result.Items))
	for _, item := range result.Items {
		if item.ID.VideoID != "" {
			videos = append(videos, Video{
				Title:        item.Snippet.Title,
				ChannelTitle: item.Snippet.ChannelTitle,
				VideoId:      item.ID.VideoID,
			})
		}
	}
	return videos, nil
}
