package spotify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type ProfileInfo struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl,omitempty"`
	URL      string `json:"url,omitempty"`
}

type PlaylistInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ImageURL string `json:"imageUrl,omitempty"`
}

type image struct {
	URL string `json:"url"`
}

func GetUserProfileContext(ctx context.Context, token string) (ProfileInfo, error) {
	var result struct {
		ID           string  `json:"id"`
		DisplayName  string  `json:"display_name"`
		Images       []image `json:"images"`
		ExternalURLs struct {
			Spotify string `json:"spotify"`
		} `json:"external_urls"`
	}

	err := getJSON(ctx, token, "https://api.spotify.com/v1/me", &result)
	if err != nil {
		return ProfileInfo{}, fmt.Errorf("получить профиль Spotify: %w", err)
	}

	name := result.DisplayName
	if name == "" {
		name = result.ID
	}
	return ProfileInfo{
		Name:     name,
		ImageURL: firstImageURL(result.Images),
		URL:      result.ExternalURLs.Spotify,
	}, nil
}

func GetUserPlaylistsContext(ctx context.Context, token string) ([]PlaylistInfo, error) {
	queryParams := url.Values{}
	queryParams.Set("limit", "50")

	playlists := make([]PlaylistInfo, 0)
	offset := 0
	for {
		queryParams.Set("offset", strconv.Itoa(offset))
		requestURL := "https://api.spotify.com/v1/me/playlists?" + queryParams.Encode()

		var result struct {
			Items []struct {
				ID     string  `json:"id"`
				Name   string  `json:"name"`
				Images []image `json:"images"`
			} `json:"items"`
			Next string `json:"next"`
		}
		err := getJSON(ctx, token, requestURL, &result)
		if err != nil {
			return nil, fmt.Errorf("получить плейлисты Spotify: %w", err)
		}

		for _, item := range result.Items {
			if item.ID == "" || item.Name == "" {
				continue
			}
			playlists = append(playlists, PlaylistInfo{
				ID:       item.ID,
				Title:    item.Name,
				ImageURL: firstImageURL(item.Images),
			})
		}

		if result.Next == "" {
			return playlists, nil
		}
		if len(result.Items) == 0 {
			return nil, fmt.Errorf("Spotify вернул пустую страницу со следующей страницей")
		}
		offset += len(result.Items)
	}
}

func firstImageURL(images []image) string {
	for _, item := range images {
		if item.URL != "" {
			return item.URL
		}
	}
	return ""
}
