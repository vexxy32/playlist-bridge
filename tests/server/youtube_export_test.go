//go:build playlist_bridge_tests

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"playlist-bridge/internal/transfer"
)

func TestExportYouTubeMusicWithoutLogin(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("playlistId") != "PLtest" {
			t.Error("неверный ID")
		}
		data := `{"items":[{"snippet":{"title":"Numb","videoOwnerChannelTitle":"Linkin Park - Topic","resourceId":{"videoId":"123"}}}]}`
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data))}, nil
	})
	s := New("id", "secret")
	s.SetYouTubeAPIKey("key")
	req := httptest.NewRequest(http.MethodPost, "/api/export/youtube", strings.NewReader(`{"url":"https://music.youtube.com/playlist?list=PLtest"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("статус %d: %s", w.Code, w.Body.String())
	}
	tracks, err := transfer.ParseTracks(w.Body.Bytes())
	if err != nil || len(tracks) != 1 || tracks[0].Title != "Numb" || tracks[0].Artist != "Linkin Park" {
		t.Fatalf("tracks=%+v err=%v", tracks, err)
	}
}
