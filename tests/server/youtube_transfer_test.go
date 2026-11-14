//go:build playlist_bridge_tests

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTransferFileToYouTubeMusic(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var calls []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		body := ""
		switch r.URL.Path {
		case "/youtube/v3/search":
			body = `{"items":[{"id":{"videoId":"abc123"},"snippet":{"title":"Numb","channelTitle":"Linkin Park - Topic"}}]}`
		case "/youtube/v3/playlists":
			if r.Header.Get("Authorization") != "Bearer google-token" {
				t.Error("нет токена Google")
			}
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			body = `{"id":"playlist123"}`
		case "/youtube/v3/playlistItems":
			var input struct {
				Snippet struct {
					PlaylistID string `json:"playlistId"`
					ResourceID struct {
						VideoID string `json:"videoId"`
					} `json:"resourceId"`
				} `json:"snippet"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Snippet.PlaylistID != "playlist123" || input.Snippet.ResourceID.VideoID != "abc123" {
				t.Errorf("ошибка добавления: %+v %v", input, err)
			}
			body = `{}`
		default:
			t.Errorf("неожиданный запрос: %s", r.URL.Path)
			body = `{}`
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("id", "secret")
	s.SetYouTubeAPIKey("key")
	s.sessions["g"] = session{google: true, accessToken: "google-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"destination":"youtubeMusic","name":"Test","tracks":[{"title":"Numb","artist":"Linkin Park"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "google_session", Value: "g"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "https://music.youtube.com/playlist?list=playlist123") {
		t.Fatalf("status=%d body=%s calls=%v", w.Code, w.Body.String(), calls)
	}
	if len(calls) != 3 {
		t.Fatalf("запросы: %v", calls)
	}
}

func TestGoogleLoginRequiresMatchingState(t *testing.T) {
	s := New("id", "secret")
	s.SetGoogleCredentials("google-id", "google-secret")
	login := httptest.NewRecorder()
	s.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/auth/google", nil))
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status: %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodGet, "/callback/google?code=test&state=wrong", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	if response.Header().Get("Location") != "/?google=invalid" {
		t.Fatalf("state accepted: %s", response.Header().Get("Location"))
	}
}

func TestTransferSpotifyToYouTube(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Host {
		case "api.spotify.com":
			if r.Header.Get("Authorization") != "Bearer spotify-token" {
				t.Error("неверный токен Spotify")
			}
			body = `{"items":[{"item":{"type":"track","name":"Numb","artists":[{"name":"Linkin Park"}],"album":{"name":"Meteora"}}}],"next":null}`
		case "www.googleapis.com":
			switch r.URL.Path {
			case "/youtube/v3/search":
				body = `{"items":[{"id":{"videoId":"abc123"},"snippet":{"title":"Numb","channelTitle":"Linkin Park - Topic"}}]}`
			case "/youtube/v3/playlists":
				body = `{"id":"playlist123"}`
			case "/youtube/v3/playlistItems":
				body = `{}`
			default:
				t.Errorf("неизвестный путь: %s", r.URL.Path)
			}
		default:
			t.Errorf("неизвестный хост: %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("id", "secret")
	s.SetYouTubeAPIKey("key")
	s.sessions["g"] = session{google: true, accessToken: "google-token", expiresAt: time.Now().Add(time.Hour)}
	s.sessions["s"] = session{accessToken: "spotify-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"destination":"youtube","name":"Test","spotifyUrl":"https://open.spotify.com/playlist/1234567890123456789012"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "google_session", Value: "g"})
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "s"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "playlist123") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}
