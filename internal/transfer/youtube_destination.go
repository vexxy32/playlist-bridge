package transfer

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"

	"playlist-bridge/internal/youtube"
)

// SearchYouTube преобразует найденные ролики в кандидатов для общего алгоритма.
func SearchYouTube(apiKey string) SearchFunc {
	search, _ := NewYouTubeSearch(apiKey)
	return search
}

// NewYouTubeSearch reports search attempts for a single transfer.
func NewYouTubeSearch(apiKey string) (SearchFunc, func() int) {
	var requests atomic.Int64
	var mu sync.Mutex
	queries := make(map[string]func() ([]youtube.Video, error))
	search := func(ctx context.Context, track Track) ([]Candidate, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query := track.Artist + " " + track.Title
		mu.Lock()
		lookup, ok := queries[query]
		if !ok {
			lookup = sync.OnceValues(func() ([]youtube.Video, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				requests.Add(1)
				return youtube.SearchVideos(ctx, apiKey, query)
			})
			queries[query] = lookup
		}
		mu.Unlock()
		videos, err := lookup()
		if err != nil {
			return nil, err
		}
		candidates := make([]Candidate, 0, len(videos))
		for _, v := range videos {
			parsed := videoTrackForArtist(v, track.Artist)
			candidates = append(candidates, Candidate{
				Title:   parsed.Title,
				Artists: []string{parsed.Artist},
				URI:     v.VideoId,
			})
		}
		return candidates, nil
	}
	return search, func() int { return int(requests.Load()) }
}

func CreateYouTube(token string, music bool) CreateFunc {
	return func(ctx context.Context, name string) (Playlist, error) {
		id, err := youtube.CreatePlaylist(ctx, token, name)
		if err != nil {
			return Playlist{}, err
		}
		host := "www.youtube.com"
		if music {
			host = "music.youtube.com"
		}
		return Playlist{ID: id, URL: "https://" + host + "/playlist?list=" + url.QueryEscape(id)}, nil
	}
}

func AddToYouTube(token string) AddFunc {
	return func(ctx context.Context, playlistID string, ids []string) error {
		return youtube.AddVideos(ctx, token, playlistID, ids)
	}
}

// CopyYouTubePlaylist сохраняет исходные видео и их порядок при переносе между YouTube и YouTube Music.
func CopyYouTubePlaylist(ctx context.Context, token, name string, videos []youtube.Video, music bool) (Playlist, []TransferResult, error) {
	results := make([]TransferResult, 0, len(videos))
	for _, video := range videos {
		track := videoTrack(video)
		unavailable := track.unavailable || video.VideoId == ""
		result := TransferResult{Track: track, Found: !unavailable, Unavailable: unavailable}
		if !unavailable {
			result.URI = video.VideoId
		}
		results = append(results, result)
	}

	videoIDs := collectFoundURIs(results)
	if len(videoIDs) == 0 {
		return Playlist{}, results, fmt.Errorf("в исходном плейлисте нет доступных видео")
	}

	playlist, err := CreateYouTube(token, music)(ctx, name)
	if err != nil {
		return Playlist{}, results, fmt.Errorf("создать плейлист: %w", err)
	}
	if err := youtube.AddVideos(ctx, token, playlist.ID, videoIDs); err != nil {
		return playlist, results, fmt.Errorf("плейлист создан, но добавление видео остановилось: %w", err)
	}
	return playlist, results, nil
}
