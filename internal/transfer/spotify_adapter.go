package transfer

import (
	"context"

	"playlist-bridge/internal/spotify"
)

// SearchSpotify подключает поиск Spotify к общему алгоритму переноса.
func SearchSpotify(token string) SearchFunc {
	return func(ctx context.Context, track Track) ([]Candidate, error) {
		found, err := spotify.SearchTrack(ctx, token, track.Title, track.Artist)
		if err != nil {
			return nil, err
		}
		candidates := make([]Candidate, 0, len(found))
		for _, item := range found {
			candidates = append(candidates, Candidate{
				Title: item.Title, Artists: item.Artists, Album: item.Album, URI: item.URI,
			})
		}
		return candidates, nil
	}
}

func AddToSpotify(token string) AddFunc {
	return func(ctx context.Context, playlistID string, uris []string) error {
		return spotify.AddTrack(ctx, token, playlistID, uris)
	}
}

func CreateSpotify(token string) CreateFunc {
	return func(ctx context.Context, name string) (Playlist, error) {
		playlist, err := spotify.CreatePlaylist(ctx, token, name, "", false)
		if err != nil {
			return Playlist{}, err
		}
		return Playlist{
			ID:  playlist.ID,
			URI: playlist.URI,
			URL: playlist.ExternalURLs.Spotify,
		}, nil
	}
}
