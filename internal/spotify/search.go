package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Song struct {
	Type  string `json:"type"`
	Title string `json:"name"`

	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
}

func GetSong(ctx context.Context, token, trackID string) (Song, error) {
	requestURL := "https://api.spotify.com/v1/tracks/" + url.PathEscape(trackID)
	var song Song
	err := getJSON(ctx, token, requestURL, &song)
	if err != nil {
		return Song{}, fmt.Errorf("получить трек Spotify: %w", err)
	}
	return song, nil
}

// SongsFromURL получает трек или содержимое плейлиста по ссылке Spotify.
func SongsFromURL(ctx context.Context, token, link string) ([]Song, error) {
	kind, id := resourceFromURL(link)
	switch kind {
	case "track":
		song, err := GetSong(ctx, token, id)
		if err != nil {
			return nil, err
		}
		return []Song{song}, nil
	case "playlist":
		return GetPlaylistSongsContext(ctx, token, id)
	default:
		return nil, ErrInvalidSourceURL
	}
}

// GetPlaylistSongs получает все страницы доступного пользователю плейлиста.
func GetPlaylistSongs(token, playlistID string) ([]Song, error) {
	return GetPlaylistSongsContext(context.Background(), token, playlistID)
}

func GetPlaylistSongsContext(ctx context.Context, token, playlistID string) ([]Song, error) {
	queryParams := url.Values{}
	queryParams.Set("limit", "50")

	songs := make([]Song, 0)
	offset := 0
	for {
		queryParams.Set("offset", strconv.Itoa(offset))
		requestURL := "https://api.spotify.com/v1/playlists/" + url.PathEscape(playlistID) + "/items?" + queryParams.Encode()
		var result struct {
			Items []struct {
				Item *Song `json:"item"`
			} `json:"items"`
			Next *string `json:"next"`
		}
		err := getJSON(ctx, token, requestURL, &result)
		if err != nil {
			return nil, fmt.Errorf("получить плейлист Spotify: %w", err)
		}
		for _, entry := range result.Items {
			if entry.Item != nil && entry.Item.Title != "" && len(entry.Item.Artists) > 0 && (entry.Item.Type == "" || entry.Item.Type == "track") {
				songs = append(songs, *entry.Item)
			}
		}
		if result.Next == nil || *result.Next == "" {
			break
		}
		if len(result.Items) == 0 {
			return nil, fmt.Errorf("Spotify вернул пустую страницу со следующей страницей")
		}
		offset += len(result.Items)
	}
	return songs, nil
}

var ErrInvalidSourceURL = errors.New("вставь ссылку на трек или плейлист Spotify")

// PlaylistIDFromURL извлекает ID из ссылки на плейлист Spotify.
func PlaylistIDFromURL(link string) (string, error) {
	kind, id := resourceFromURL(link)
	if kind != "playlist" {
		return "", fmt.Errorf("вставь ссылку на плейлист Spotify")
	}
	return id, nil
}

func resourceFromURL(link string) (string, string) {
	parsedURL, err := url.Parse(strings.TrimSpace(link))
	if err != nil || parsedURL.Scheme != "https" || !strings.EqualFold(parsedURL.Host, "open.spotify.com") || parsedURL.User != nil {
		return "", ""
	}

	path := strings.TrimPrefix(parsedURL.Path, "/")
	if strings.HasPrefix(path, "intl-") {
		_, path, _ = strings.Cut(path, "/")
	}
	kind, id, _ := strings.Cut(path, "/")
	if (kind != "track" && kind != "playlist") || len(id) != 22 {
		return "", ""
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
			return "", ""
		}
	}
	return kind, id
}
