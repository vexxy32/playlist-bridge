package transfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpotifySearchAdapter(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.spotify.com" || r.Header.Get("Authorization") != "Bearer token" {
			t.Error("неверный запрос Spotify")
		}
		body := `{"tracks":{"items":[{"name":"Numb","uri":"spotify:track:1","album":{"name":"Meteora"},"artists":[{"name":"Linkin Park"}]}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	got, err := TransferTracks(context.Background(), []Track{{Title: "Numb", Artist: "Linkin Park", Album: "Meteora"}}, SearchSpotify("token"))
	if err != nil || len(got) != 1 || !got[0].Found || got[0].URI != "spotify:track:1" {
		t.Fatalf("результат: %+v, ошибка: %v", got, err)
	}
}

func TestTransferTracksUsesSelectedSearch(t *testing.T) {
	tracks := []Track{{Title: "Numb", Artist: "Linkin Park", Album: "Meteora"}, {Title: "Missing", Artist: "Someone"}}
	var calls []Track
	var mu sync.Mutex
	search := func(_ context.Context, track Track) ([]Candidate, error) {
		mu.Lock()
		calls = append(calls, track)
		mu.Unlock()
		if track.Title == "Missing" {
			return nil, nil
		}
		return []Candidate{
			{Title: "Numb", Artists: []string{"Linkin Park"}, Album: "Other", URI: "service:other"},
			{Title: "numb", Artists: []string{"linkin park"}, Album: "Meteora", URI: "service:correct"},
		}, nil
	}
	results, err := TransferTracks(context.Background(), tracks, search)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || (calls[0] != tracks[0] && calls[1] != tracks[0]) || (calls[0] != tracks[1] && calls[1] != tracks[1]) {
		t.Fatalf("поиск получил %v", calls)
	}
	if len(results) != 2 || !results[0].Found || results[0].URI != "service:correct" || results[1].Found {
		t.Fatalf("неверный результат: %+v", results)
	}
	if got := collectFoundURIs(results); !reflect.DeepEqual(got, []string{"service:correct"}) {
		t.Fatalf("неверные URI: %v", got)
	}
}

func TestTransferTracksReportsSearchErrors(t *testing.T) {
	want := errors.New("сервис недоступен")
	_, err := TransferTracks(context.Background(), []Track{{Title: "Song", Artist: "Artist"}}, func(context.Context, Track) ([]Candidate, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("ожидалась исходная ошибка, получено %v", err)
	}
	if _, err := TransferTracks(context.Background(), nil, nil); err == nil {
		t.Fatal("ожидалась ошибка отсутствующего поиска")
	}
}

func TestSelectCandidateWithSeveralEditions(t *testing.T) {
	track := Track{Title: "Breaking the Habit", Artist: "Linkin Park"}
	candidates := []Candidate{
		{Title: "Breaking the Habit - Live", Artists: []string{"Linkin Park"}, URI: "live"},
		{Title: "Breaking the Habit", Artists: []string{"Linkin Park"}, Album: "Meteora", URI: "original"},
		{Title: "Breaking the Habit", Artists: []string{"Linkin Park"}, Album: "Compilation", URI: "other-edition"},
	}
	got, ok := selectCandidate(track, candidates)
	if !ok || got.URI != "original" {
		t.Fatalf("не выбрано первое подходящее издание: %+v", got)
	}
	track.Album = "Compilation"
	got, ok = selectCandidate(track, candidates)
	if !ok || got.URI != "other-edition" {
		t.Fatalf("не учтён альбом: %+v", got)
	}
}

func TestSelectCandidateArtistAndVersion(t *testing.T) {
	cases := []struct {
		name       string
		track      Track
		candidates []Candidate
		want       string
		found      bool
	}{
		{"accent", Track{Title: "Numb / Encore", Artist: "JAY-Z"}, []Candidate{{Title: "Numb / Encore", Artists: []string{"JAŸ-Z", "Linkin Park"}, URI: "different"}}, "", false},
		{"transliteration", Track{Title: "Группа крови", Artist: "Кино"}, []Candidate{{Title: "Группа крови", Artists: []string{"Kino"}, URI: "different"}}, "", false},
		{"spaces and case", Track{Title: " Song ", Artist: " ARTIST "}, []Candidate{{Title: "song", Artists: []string{"artist"}, URI: "correct"}}, "correct", true},
		{"different punctuation", Track{Title: "A B", Artist: "Artist"}, []Candidate{{Title: "AB", Artists: []string{"Artist"}, URI: "different"}}, "", false},
		{"wrong artist", Track{Title: "Numb", Artist: "Eminem"}, []Candidate{{Title: "Numb", Artists: []string{"Linkin Park"}, URI: "wrong"}}, "", false},
		{"remix not original", Track{Title: "Numb", Artist: "Linkin Park"}, []Candidate{{Title: "Numb - Remix", Artists: []string{"Linkin Park"}, URI: "wrong"}}, "", false},
		{"remaster", Track{Title: "Numb", Artist: "Linkin Park"}, []Candidate{{Title: "Numb - 2011 Remastered", Artists: []string{"Linkin Park"}, URI: "correct"}}, "correct", true},
		{"different remaster", Track{Title: "Song - 2011 Remaster", Artist: "Artist"}, []Candidate{{Title: "Song - 2021 Remaster", Artists: []string{"Artist"}, URI: "wrong"}}, "", false},
		{"solo before duet", Track{Title: "Song", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist", "Other"}, URI: "duet"}, {Title: "Song", Artists: []string{"Artist"}, URI: "solo"}}, "solo", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectCandidate(tc.track, tc.candidates)
			if ok != tc.found || got.URI != tc.want {
				t.Fatalf("получено: %+v, found=%v", got, ok)
			}
		})
	}
}

func TestSelectCandidatePrefersExactTitleOverRemaster(t *testing.T) {
	track := Track{Title: "Song", Artist: "Artist", Album: "Example"}
	candidates := []Candidate{
		{Title: "Song - 2011 Remaster", Artists: []string{"Artist"}, Album: "Example", URI: "remaster"},
		{Title: "Song", Artists: []string{"Artist"}, URI: "original"},
	}
	got, ok := selectCandidate(track, candidates)
	if !ok || got.URI != "original" {
		t.Fatalf("ожидалось точное название, получено %+v", got)
	}
}

func TestTransferTracksReusesDuplicateSearchAndKeepsDuplicates(t *testing.T) {
	tracks := []Track{{Title: "Song", Artist: "Artist"}, {Title: "Other", Artist: "Artist"}, {Title: "song", Artist: "artist"}}
	var mu sync.Mutex
	calls := make(map[string]int)
	search := func(_ context.Context, track Track) ([]Candidate, error) {
		mu.Lock()
		calls[strings.ToLower(track.Title)]++
		mu.Unlock()
		return []Candidate{{Title: track.Title, Artists: []string{track.Artist}, URI: "uri:" + strings.ToLower(track.Title)}}, nil
	}
	got, err := TransferTracks(context.Background(), tracks, search)
	if err != nil {
		t.Fatal(err)
	}
	if calls["song"] != 1 || calls["other"] != 1 || len(got) != 3 || got[0].URI != "uri:song" || got[1].URI != "uri:other" || got[2].URI != "uri:song" {
		t.Fatalf("calls=%v results=%+v", calls, got)
	}
}

func TestTransferTracksRunsSearchConcurrently(t *testing.T) {
	var mu sync.Mutex
	active, maximum := 0, 0
	search := func(_ context.Context, track Track) ([]Candidate, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return []Candidate{{Title: track.Title, Artists: []string{track.Artist}, URI: track.Title}}, nil
	}
	tracks := []Track{{Title: "one", Artist: "A"}, {Title: "two", Artist: "A"}, {Title: "three", Artist: "A"}, {Title: "four", Artist: "A"}}
	results, err := TransferTracks(context.Background(), tracks, search)
	if err != nil || len(results) != 4 || results[0].URI != "one" || results[3].URI != "four" {
		t.Fatalf("порядок: %+v %v", results, err)
	}
	if maximum < 2 || maximum > 3 {
		t.Fatalf("одновременных запросов: %d", maximum)
	}
}

func TestCreateTransferPreservesCreatedPlaylistOnAddError(t *testing.T) {
	want := errors.New("ошибка добавления")
	search := func(context.Context, Track) ([]Candidate, error) {
		return []Candidate{{Title: "Song", Artists: []string{"Artist"}, URI: "track:1"}}, nil
	}
	create := func(_ context.Context, name string) (Playlist, error) {
		return Playlist{ID: "created", URL: "https://example.com/playlist"}, nil
	}
	add := func(_ context.Context, id string, uris []string) error {
		if id != "created" || !reflect.DeepEqual(uris, []string{"track:1"}) {
			t.Errorf("неверные данные: %s %v", id, uris)
		}
		return want
	}
	playlist, results, err := CreateTransfer(context.Background(), []Track{{Title: "Song", Artist: "Artist"}}, "Test", search, create, add)
	if !errors.Is(err, want) || playlist.ID != "created" || len(results) != 1 {
		t.Fatalf("плейлист и результаты потеряны: %+v %+v %v", playlist, results, err)
	}
}

func TestCreateTransferDoesNotCreateEmptyPlaylist(t *testing.T) {
	created := false
	search := func(context.Context, Track) ([]Candidate, error) { return nil, nil }
	create := func(context.Context, string) (Playlist, error) { created = true; return Playlist{}, nil }
	add := func(context.Context, string, []string) error { return nil }
	_, results, err := CreateTransfer(context.Background(), []Track{{Title: "Missing", Artist: "Artist"}}, "Test", search, create, add)
	if err == nil || created || len(results) != 1 || results[0].Found {
		t.Fatalf("неверный результат: %v %+v %v", created, results, err)
	}
}
