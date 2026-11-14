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

func TestYouTubeAccountAndPlaylistImages(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer google-token" {
			t.Error("неверный токен")
		}
		var body string
		switch r.URL.Path {
		case "/youtube/v3/channels":
			body = `{"items":[{"snippet":{"title":"My Channel","thumbnails":{"default":{"url":"https://example.com/avatar"}}}}]}`
		case "/youtube/v3/playlists":
			if r.URL.Query().Get("pageToken") == "" {
				body = `{"nextPageToken":"next","items":[{"id":"one","snippet":{"title":"First","thumbnails":{"medium":{"url":"https://example.com/cover"}}}}]}`
			} else {
				body = `{"items":[{"id":"two","snippet":{"title":"Second"}}]}`
			}
		default:
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("id", "secret")
	s.sessions["g"] = session{google: true, accessToken: "google-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodGet, "/api/youtube-account", nil)
	req.AddCookie(&http.Cookie{Name: "google_session", Value: "g"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("статус %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Profile struct {
			Name     string `json:"name"`
			ImageURL string `json:"imageUrl"`
		} `json:"profile"`
		Playlists []struct {
			Title    string `json:"title"`
			ImageURL string `json:"imageUrl"`
		} `json:"playlists"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Profile.Name != "My Channel" || result.Profile.ImageURL == "" || len(result.Playlists) != 2 || result.Playlists[0].ImageURL == "" || result.Playlists[1].ImageURL != "" {
		t.Fatalf("неверные данные: %+v", result)
	}
}
