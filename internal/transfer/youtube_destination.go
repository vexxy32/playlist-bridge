package transfer

import (
	"context"
	"fmt"
	"html"
	"net/url"

	"playlist-bridge/internal/youtube"
)

// SearchYouTube преобразует найденные ролики в кандидатов для общего алгоритма.
func SearchYouTube(apiKey string) SearchFunc {
	return func(ctx context.Context, track Track) ([]Candidate, error) {
		videos, err := youtube.SearchVideos(ctx, apiKey, track.Artist+" "+track.Title)
		if err != nil {
			return nil, err
		}
		candidates := make([]Candidate, 0, len(videos))
		for _, v := range videos {
			v.Title = html.UnescapeString(v.Title)
			v.ChannelTitle = html.UnescapeString(v.ChannelTitle)
			parsed := videoTrack(v)
			candidates = append(candidates, Candidate{
				Title:   parsed.Title,
				Artists: []string{parsed.Artist},
				URI:     v.VideoId,
			})
		}
		return candidates, nil
	}
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
		results = append(results, TransferResult{
			Track: videoTrack(video),
			Found: video.VideoId != "",
			URI:   video.VideoId,
		})
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
