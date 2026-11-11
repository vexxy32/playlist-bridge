package youtube

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetPlaylistVideosReadsAllPages(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	requests := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Query().Get("playlistId") != "playlist" || r.URL.Query().Get("maxResults") != "50" {
			t.Error("неверные параметры запроса")
		}
		body := `{"nextPageToken":"next","items":[{"snippet":{"title":"First","videoOwnerChannelTitle":"Artist - Topic","resourceId":{"videoId":"one"}}}]}`
		if requests == 2 {
			if r.URL.Query().Get("pageToken") != "next" {
				t.Error("не передан токен следующей страницы")
			}
			body = `{"items":[{"snippet":{"title":"Second","videoOwnerChannelTitle":"Other","resourceId":{"videoId":"two"}}}]}`
		} else if requests > 2 {
			t.Fatal("лишний запрос")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	got, err := GetPlaylistVideos("key", "playlist")
	if err != nil || requests != 2 || len(got) != 2 {
		t.Fatalf("получено %v, запросов %d, ошибка %v", got, requests, err)
	}
	if got[0].Title != "First" || got[0].ChannelTitle != "Artist - Topic" || got[1].VideoId != "two" {
		t.Fatalf("неверные данные: %+v", got)
	}
}
