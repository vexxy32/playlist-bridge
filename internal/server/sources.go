package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"playlist-bridge/internal/spotify"
	"playlist-bridge/internal/transfer"
	"playlist-bridge/internal/youtube"
)

func (input transferRequest) hasMultipleSources() bool {
	hasYouTubeURL := strings.TrimSpace(input.YouTubeURL) != ""
	hasSpotifyURL := strings.TrimSpace(input.SpotifyURL) != ""
	hasJSON := len(input.Tracks) > 0 && string(input.Tracks) != "null"
	return (hasYouTubeURL && hasSpotifyURL) || (hasJSON && (hasYouTubeURL || hasSpotifyURL))
}

func (s *Server) readSource(r *http.Request, input transferRequest) ([]transfer.Track, int, error) {
	youtubeURL := strings.TrimSpace(input.YouTubeURL)
	spotifyURL := strings.TrimSpace(input.SpotifyURL)
	switch {
	case youtubeURL != "":
		return s.readYouTubeSource(r, youtubeURL, input.YouTubeFromAccount)
	case spotifyURL != "":
		return s.readSpotifySource(r, spotifyURL)
	default:
		tracks, err := transfer.ParseTracks(input.Tracks)
		return tracks, http.StatusBadRequest, err
	}
}

func (s *Server) readYouTubeSource(r *http.Request, link string, fromAccount bool) ([]transfer.Track, int, error) {
	videos, status, err := s.readYouTubeVideos(r, link, fromAccount)
	if err != nil {
		return nil, status, err
	}
	return transfer.TracksFromYouTube(videos), http.StatusOK, nil
}

func (s *Server) readYouTubeVideos(r *http.Request, link string, fromAccount bool) ([]youtube.Video, int, error) {
	id, err := youtube.PlaylistIDFromURL(link)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	var videos []youtube.Video
	if fromAccount {
		token, ok := s.requestToken(r, "google_session")
		if !ok {
			return nil, http.StatusUnauthorized, fmt.Errorf("подключи Google для чтения плейлиста")
		}
		videos, err = youtube.GetAccountPlaylistVideos(r.Context(), token, id)
	} else {
		if s.youtubeAPIKey == "" {
			return nil, http.StatusServiceUnavailable, fmt.Errorf("чтение публичных плейлистов YouTube ещё не настроено")
		}
		videos, err = youtube.GetPlaylistVideosContext(r.Context(), s.youtubeAPIKey, id)
	}
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("не удалось прочитать плейлист YouTube. Проверь доступ и ссылку")
	}

	return videos, http.StatusOK, nil
}

func (s *Server) readSpotifySource(r *http.Request, link string) ([]transfer.Track, int, error) {
	token, ok := s.requestToken(r, "playlist_session")
	if !ok {
		return nil, http.StatusUnauthorized, fmt.Errorf("подключи Spotify для чтения треков")
	}

	songs, err := spotify.SongsFromURL(r.Context(), token, link)
	if err != nil {
		if errors.Is(err, spotify.ErrInvalidSourceURL) {
			return nil, http.StatusBadRequest, err
		}
		var rateLimit *spotify.RateLimitError
		if errors.As(err, &rateLimit) {
			return nil, http.StatusTooManyRequests, err
		}
		return nil, http.StatusBadGateway, fmt.Errorf("не удалось прочитать треки Spotify. Для плейлиста проверь, что ты владелец или соавтор")
	}

	return transfer.TracksFromSpotify(songs), http.StatusOK, nil
}

func (s *Server) requestToken(r *http.Request, cookieName string) (string, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	return s.sessionToken(cookie.Value, cookie.Name)
}

func writeRetryAfter(w http.ResponseWriter, err error) bool {
	var rateLimit *spotify.RateLimitError
	if !errors.As(err, &rateLimit) {
		return false
	}
	if rateLimit.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(rateLimit.RetryAfterSeconds))
	}
	return true
}
