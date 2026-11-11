package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func postJSON(ctx context.Context, token, endpoint string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("запрос к YouTube: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("YouTube вернул статус: %s", resp.Status)
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("прочитать ответ YouTube: %w", err)
		}
	}
	return nil
}

// CreatePlaylist создаёт закрытый плейлист в аккаунте пользователя.
func CreatePlaylist(ctx context.Context, token, name string) (string, error) {
	payload := map[string]any{
		"snippet": map[string]string{"title": name},
		"status":  map[string]string{"privacyStatus": "private"},
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := postJSON(ctx, token, "https://www.googleapis.com/youtube/v3/playlists?part=snippet,status", payload, &result); err != nil {
		return "", err
	}
	if result.ID == "" {
		return "", fmt.Errorf("YouTube не вернул ID плейлиста")
	}
	return result.ID, nil
}

// AddVideos добавляет видео по одному, сохраняя исходный порядок.
func AddVideos(ctx context.Context, token, playlistID string, videoIDs []string) error {
	for i, id := range videoIDs {
		payload := map[string]any{
			"snippet": map[string]any{
				"playlistId": playlistID,
				"resourceId": map[string]string{
					"kind":    "youtube#video",
					"videoId": id,
				},
			},
		}
		if err := postJSON(ctx, token, "https://www.googleapis.com/youtube/v3/playlistItems?part=snippet", payload, nil); err != nil {
			return fmt.Errorf("добавлено %d из %d: %w", i, len(videoIDs), err)
		}
	}
	return nil
}
