package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"playlist-bridge/internal/transfer"
)

func TestExportSpotifyPagesAndJSON(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("токен не передан")
		}
		if r.URL.Path != "/v1/playlists/1234567890123456789012/items" {
			t.Errorf("неверный путь: %s", r.URL.Path)
		}
		var body string
		switch r.URL.Query().Get("offset") {
		case "0":
			entries := make([]string, 50)
			for i := range entries {
				entries[i] = fmt.Sprintf(`{"item":{"type":"track","name":"Song %d","artists":[{"name":"Artist"}],"album":{"name":"Album"}}}`, i)
			}
			body = `{"items":[` + strings.Join(entries, ",") + `],"next":"more"}`
		case "50":
			body = `{"items":[{"item":{"type":"track","name":"Last","artists":[{"name":"One"},{"name":"Two"}],"album":{"name":"Record"}}},{"item":null}],"next":null}`
		default:
			t.Errorf("неверная страница: %s", r.URL.RawQuery)
			body = `{}`
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("id", "secret")
	s.sessions["session"] = session{accessToken: "user-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/export/spotify", strings.NewReader(`{"url":"https://open.spotify.com/playlist/1234567890123456789012"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var tracks []transfer.Track
	if err := json.Unmarshal(w.Body.Bytes(), &tracks); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(tracks) != 51 || tracks[50].Artist != "One, Two" || tracks[50].Album != "Record" {
		t.Fatalf("calls=%d tracks=%d last=%+v", calls, len(tracks), tracks[len(tracks)-1])
	}
	parsed, err := transfer.ParseTracks(w.Body.Bytes())
	if err != nil || len(parsed) != 51 {
		t.Fatalf("импорт: %v", err)
	}
}
