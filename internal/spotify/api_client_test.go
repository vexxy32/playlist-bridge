package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAddTrackBatchesInOrder(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var got []string
	var sizes []int
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.spotify.com/v1/playlists/playlist123/items" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("неверный запрос: %s %s", r.Method, r.URL)
		}
		var body addTracksRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(body.URIs))
		got = append(got, body.URIs...)
		return &http.Response{StatusCode: http.StatusCreated, Status: "201 Created", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	var uris []string
	for i := 0; i < 205; i++ {
		uris = append(uris, fmt.Sprintf("spotify:track:%d", i))
	}
	if err := AddTrack(context.Background(), "token", "playlist123", uris); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{100, 100, 5}) || !reflect.DeepEqual(got, uris) {
		t.Fatalf("части: %v, порядок сохранён: %v", sizes, reflect.DeepEqual(got, uris))
	}
}

func TestSearchTrackReportsRateLimitWithoutRetrying(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("неверный размер поиска: %s", r.URL.RawQuery)
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests", Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	_, err := SearchTrack(context.Background(), "token", "Song", "Artist")
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfterSeconds != 1 {
		t.Fatalf("ожидалось сообщение об ограничении: %v", err)
	}
	if calls != 1 {
		t.Fatalf("ожидался один запрос, получено %d", calls)
	}
}
