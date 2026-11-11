package server

import (
	"encoding/json"
	"net/http"

	"playlist-bridge/internal/spotify"
)

func (s *Server) spotifyAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := s.requestToken(r, "playlist_session")
	if !ok {
		http.Error(w, "Подключи Spotify заново", http.StatusUnauthorized)
		return
	}

	profile, err := spotify.GetUserProfileContext(r.Context(), token)
	if err != nil {
		writeSpotifyAccountError(w, err, "Не удалось получить профиль Spotify")
		return
	}

	playlists, err := spotify.GetUserPlaylistsContext(r.Context(), token)
	if err != nil {
		writeSpotifyAccountError(w, err, "Не удалось получить плейлисты Spotify")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Profile   spotify.ProfileInfo    `json:"profile"`
		Playlists []spotify.PlaylistInfo `json:"playlists"`
	}{
		Profile:   profile,
		Playlists: playlists,
	})
}

func writeSpotifyAccountError(w http.ResponseWriter, err error, message string) {
	if writeRetryAfter(w, err) {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	http.Error(w, message, http.StatusBadGateway)
}
