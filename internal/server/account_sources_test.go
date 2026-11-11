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

func TestSpotifyAccountAndTrackExport(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := make([]string, 0, 4)
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer spotify-token" {
			t.Error("неверный токен Spotify")
		}

		calls = append(calls, req.URL.RequestURI())
		var body string
		switch req.URL.Path {
		case "/v1/me":
			body = `{"id":"profile","display_name":"My Profile","images":[{"url":"https://i.scdn.co/avatar"}]}`
		case "/v1/me/playlists":
			switch req.URL.Query().Get("offset") {
			case "0":
				body = `{"items":[{"id":"first","name":"First","images":[{"url":"https://i.scdn.co/cover"}]}],"next":"more"}`
			case "1":
				body = `{"items":[{"id":"second","name":"Second","images":null}],"next":null}`
			default:
				t.Errorf("неверная страница: %s", req.URL.RawQuery)
				body = "{}"
			}
		case "/v1/tracks/1234567890123456789012":
			body = `{"name":"Numb","artists":[{"name":"Linkin Park"}],"album":{"name":"Meteora"}}`
		default:
			t.Errorf("неожиданный запрос: %s", req.URL.Path)
			body = "{}"
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	app := New("id", "secret")
	app.sessions["spotify-id"] = session{accessToken: "spotify-token", expiresAt: time.Now().Add(time.Hour)}
	app.sessions["google-id"] = session{google: true, accessToken: "google-token", expiresAt: time.Now().Add(time.Hour)}
	handler := app.Handler()

	for _, id := range []string{"", "google-id"} {
		req := httptest.NewRequest(http.MethodGet, "/api/spotify-account", nil)
		if id != "" {
			req.AddCookie(&http.Cookie{Name: "playlist_session", Value: id})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("ожидался 401, получен %d", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/spotify-account", nil)
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "spotify-id"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("профиль Spotify: %d, %s", rec.Code, rec.Body)
	}
	var account struct {
		Profile struct {
			Name     string
			ImageURL string
		}
		Playlists []struct {
			ID       string
			ImageURL string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	if account.Profile.Name != "My Profile" || account.Profile.ImageURL == "" ||
		len(account.Playlists) != 2 || account.Playlists[0].ImageURL == "" || account.Playlists[1].ID != "second" {
		t.Fatalf("неверный профиль: %+v", account)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/export/spotify", strings.NewReader(`{"url":"https://open.spotify.com/intl-en/track/1234567890123456789012?si=example"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "spotify-id"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(calls) != 4 || !strings.Contains(rec.Body.String(), "Meteora") {
		t.Fatalf("экспорт трека: %d, %s, запросы %v", rec.Code, rec.Body, calls)
	}
}

func TestCopyAccountYouTubePlaylistWithoutSearch(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var addedIDs []string
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer google-token" {
			t.Error("неверный токен Google")
		}

		var body string
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/youtube/v3/playlistItems":
			if req.URL.Query().Get("playlistId") != "PLprivate" || req.URL.Query().Has("key") {
				t.Errorf("неверные параметры чтения плейлиста: %s", req.URL.RawQuery)
			}
			body = `{"items":[{"snippet":{"title":"One","resourceId":{"videoId":"first"}}},{"snippet":{"title":"Two","resourceId":{"videoId":"second"}}}]}`
		case req.Method == http.MethodPost && req.URL.Path == "/youtube/v3/playlists":
			body = `{"id":"PLnew"}`
		case req.Method == http.MethodPost && req.URL.Path == "/youtube/v3/playlistItems":
			var item struct {
				Snippet struct {
					ResourceID struct{ VideoID string }
				}
			}
			if err := json.NewDecoder(req.Body).Decode(&item); err != nil {
				t.Fatal(err)
			}
			addedIDs = append(addedIDs, item.Snippet.ResourceID.VideoID)
			body = "{}"
		default:
			t.Errorf("неожиданный запрос: %s %s", req.Method, req.URL.Path)
			body = "{}"
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	app := New("id", "secret")
	app.sessions["google-id"] = session{google: true, accessToken: "google-token", expiresAt: time.Now().Add(time.Hour)}
	input := `{"name":"Copy","destination":"youtubeMusic","youtubeFromAccount":true,"youtubeUrl":"https://www.youtube.com/playlist?list=PLprivate"}`
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(input))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "google_session", Value: "google-id"})
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || strings.Join(addedIDs, ",") != "first,second" ||
		!strings.Contains(rec.Body.String(), "https://music.youtube.com/playlist?list=PLnew") {
		t.Fatalf("копирование: %d, %s, видео %v", rec.Code, rec.Body, addedIDs)
	}
}
