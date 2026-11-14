//go:build playlist_bridge_tests

package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"playlist-bridge/internal/spotify"
	"playlist-bridge/internal/youtube"
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

func TestSpotifySearchSharesQueriesAndMatchesEachTrack(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	const filtered = "track:Song artist:Artist"
	const broad = "Song Artist"
	var mu sync.Mutex
	calls := make(map[string]int)
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		mu.Lock()
		calls[query]++
		mu.Unlock()
		body := `{"tracks":{"items":[
			{"name":"Song","uri":"solo","album":{"name":"Album"},"artists":[{"name":"Artist"}]},
			{"name":"Song","uri":"compilation","album":{"name":"Collection"},"artists":[{"name":"Artist"}]}
		]}}`
		if query == broad {
			body = `{"tracks":{"items":[
				{"name":"Song","uri":"duet","album":{"name":"Album"},"artists":[{"name":"Artist"},{"name":"Guest"}]},
				{"name":"Song","uri":"duet-compilation","album":{"name":"Collection"},"artists":[{"name":"Artist"},{"name":"Guest"}]}
			]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})

	tracks := []Track{
		{Title: "Song", Artist: "Artist", Album: "Album"},
		{Title: "Song", Artist: "Artist", Album: "Collection"},
		{Title: "Song ft. Guest", Artist: "Artist", Album: "Album"},
		{Title: "Song ft. Guest", Artist: "Artist", Album: "Collection"},
	}
	results, err := TransferTracks(context.Background(), tracks, SearchSpotify("token"))
	if err != nil {
		t.Fatal(err)
	}
	wantURIs := []string{"solo", "compilation", "duet", "duet-compilation"}
	if got := collectFoundURIs(results); !reflect.DeepEqual(got, wantURIs) {
		t.Fatalf("получены %v, ожидались %v", got, wantURIs)
	}
	if want := map[string]int{filtered: 1, broad: 1}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("запросы: %v, ожидались %v", calls, want)
	}
}

func TestSpotifyBroadFirstCountsFallbackRequests(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var queries []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		body := `{"tracks":{"items":[]}}`
		if query == "track:Song artist:Artist" {
			body = `{"tracks":{"items":[{"name":"Song","uri":"uri:song","artists":[{"name":"Artist"}]}]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	search, stats := NewSpotifySearch("token", SpotifySearchBroadFirst)
	results, err := TransferTracks(context.Background(), []Track{{Title: "Song", Artist: "Artist"}}, search)
	if err != nil || len(results) != 1 || results[0].URI != "uri:song" {
		t.Fatalf("результат: %+v, ошибка: %v", results, err)
	}
	if want := []string{"Song Artist", "track:Song artist:Artist"}; !reflect.DeepEqual(queries, want) {
		t.Fatalf("запросы: %v, ожидались %v", queries, want)
	}
	if got := stats(); got != (SpotifySearchStats{FirstRequests: 1, FallbackRequests: 1, FallbackMatches: 1}) {
		t.Fatalf("счётчики: %+v", got)
	}
}

func TestSpotifyReusesOnlyUnambiguousAlbumCandidate(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var queries []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		queries = append(queries, query)
		body := `{"tracks":{"items":[
			{"name":"First","uri":"uri:first","album":{"name":"Album"},"artists":[{"name":"Artist"}]},
			{"name":"Second","uri":"uri:second","album":{"name":"Album"},"artists":[{"name":"Artist"}]},
			{"name":"Third","uri":"uri:third","album":{"name":"Album"},"artists":[{"name":"Artist"}]},
			{"name":"Third","uri":"uri:other-edition","album":{"name":"Album"},"artists":[{"name":"Artist"}]}
		]}}`
		if query == "track:Third artist:Artist" {
			body = `{"tracks":{"items":[{"name":"Third","uri":"uri:third","album":{"name":"Album"},"artists":[{"name":"Artist"}]}]}}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	search, stats := NewSpotifySearch("token", SpotifySearchReuseCandidates)
	for _, tc := range []struct{ title, uri string }{
		{"First", "uri:first"}, {"Second", "uri:second"}, {"Third", "uri:third"},
	} {
		track := Track{Title: tc.title, Artist: "Artist", Album: "Album"}
		candidates, err := search(context.Background(), track)
		got, found := selectCandidate(track, candidates)
		if err != nil || !found || got.URI != tc.uri {
			t.Fatalf("результат для %q: %+v, ошибка: %v", tc.title, got, err)
		}
	}
	if want := []string{"track:First artist:Artist", "track:Third artist:Artist"}; !reflect.DeepEqual(queries, want) {
		t.Fatalf("запросы: %v, ожидались %v", queries, want)
	}
	if got := stats(); got != (SpotifySearchStats{FirstRequests: 2, FirstMatches: 2, ReusedMatches: 1}) {
		t.Fatalf("счётчики: %+v", got)
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

func TestTransferTracksStopsWorkersOnSearchError(t *testing.T) {
	want := errors.New("сервис недоступен")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	calls := 0
	workersStarted := make(chan struct{})
	search := func(ctx context.Context, track Track) ([]Candidate, error) {
		mu.Lock()
		calls++
		if calls == 3 {
			close(workersStarted)
		}
		mu.Unlock()
		select {
		case <-workersStarted:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if track.Title == "one" {
			return nil, want
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	tracks := []Track{{Title: "one"}, {Title: "two"}, {Title: "three"}, {Title: "four"}}
	if _, err := TransferTracks(ctx, tracks, search); !errors.Is(err, want) {
		t.Fatalf("ожидалась исходная ошибка, получено %v", err)
	}
	if calls != 3 {
		t.Fatalf("должны были запуститься только первые три поиска, запустилось %d", calls)
	}
}

func TestCreateTransferPreservesCompletedSearchesOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	limit := &spotify.RateLimitError{RetryAfterSeconds: 83104}
	lastStarted := make(chan struct{})
	search := func(ctx context.Context, track Track) ([]Candidate, error) {
		switch track.Title {
		case "completed":
			return []Candidate{{Title: track.Title, Artists: []string{track.Artist}, URI: "found"}}, nil
		case "failed":
			select {
			case <-lastStarted:
				return nil, limit
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "last":
			// This job cannot start until the completed result has been recorded.
			close(lastStarted)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	tracks := []Track{
		{Title: "completed", Artist: "Band"}, {Title: "blocked"}, {Title: "failed"},
		{Title: "last"}, {Title: "Deleted video", unavailable: true}, {Title: "completed", Artist: "Band"},
	}
	create := func(context.Context, string) (Playlist, error) {
		t.Fatal("плейлист создан после ошибки поиска")
		return Playlist{}, nil
	}
	add := func(context.Context, string, []string) error {
		t.Fatal("началась запись после ошибки поиска")
		return nil
	}
	playlist, results, err := CreateTransfer(ctx, tracks, "test", search, create, add)
	if !errors.Is(err, limit) || playlist.ID != "" || len(results) != len(tracks) {
		t.Fatalf("playlist=%+v, results=%+v, err=%v", playlist, results, err)
	}
	if results[0].URI != "found" || results[5].URI != "found" || !results[4].Unavailable ||
		!results[1].Pending || !results[2].Pending || !results[3].Pending {
		t.Fatalf("results=%+v", results)
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
		{"accent", Track{Title: "Numb / Encore", Artist: "JAY-Z"}, []Candidate{{Title: "Numb / Encore", Artists: []string{"JAŸ-Z", "Linkin Park"}, URI: "correct"}}, "correct", true},
		{"transliteration", Track{Title: "Группа крови", Artist: "Кино"}, []Candidate{{Title: "Группа крови", Artists: []string{"Kino"}, URI: "different"}}, "", false},
		{"spaces and case", Track{Title: " Song ", Artist: " ARTIST "}, []Candidate{{Title: "song", Artists: []string{"artist"}, URI: "correct"}}, "correct", true},
		{"different punctuation", Track{Title: "A B", Artist: "Artist"}, []Candidate{{Title: "AB", Artists: []string{"Artist"}, URI: "different"}}, "", false},
		{"wrong artist", Track{Title: "Numb", Artist: "Eminem"}, []Candidate{{Title: "Numb", Artists: []string{"Linkin Park"}, URI: "wrong"}}, "", false},
		{"remix not original", Track{Title: "Numb", Artist: "Linkin Park"}, []Candidate{{Title: "Numb - Remix", Artists: []string{"Linkin Park"}, URI: "wrong"}}, "", false},
		{"remaster", Track{Title: "Numb", Artist: "Linkin Park"}, []Candidate{{Title: "Numb - 2011 Remastered", Artists: []string{"Linkin Park"}, URI: "correct"}}, "correct", true},
		{"different remaster", Track{Title: "Song - 2011 Remaster", Artist: "Artist"}, []Candidate{{Title: "Song - 2021 Remaster", Artists: []string{"Artist"}, URI: "wrong"}}, "", false},
		{"solo before duet", Track{Title: "Song", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist", "Other"}, URI: "duet"}, {Title: "Song", Artists: []string{"Artist"}, URI: "solo"}}, "solo", true},
		{"guest must match", Track{Title: "Song ft. Guest", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist", "Other"}, URI: "wrong"}, {Title: "Song", Artists: []string{"Artist", "Guest"}, URI: "correct"}}, "correct", true},
		{"version after guest is preserved", Track{Title: "Song ft. Guest", Artist: "Artist"}, []Candidate{{Title: "Song ft. Guest - Live", Artists: []string{"Artist", "Guest"}, URI: "live"}}, "", false},
		{"original before cover", Track{Title: "Song", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Cover", "Artist"}, URI: "cover"}, {Title: "Song (feat. Guest)", Artists: []string{"Artist", "Guest"}, URI: "original"}}, "original", true},
		{"secondary credit alone is not enough", Track{Title: "Song", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Cover", "Artist"}, URI: "cover"}}, "", false},
		{"version spelling", Track{Title: "Song (Dance Mix) ft. Guest", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist", "Guest"}, URI: "original"}, {Title: "Song (feat. Guest) - Dance Mix", Artists: []string{"Artist", "Guest"}, URI: "mix"}}, "mix", true},
		{"clean version requires confirmation", Track{Title: "Song (Clean Version)", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, URI: "unknown"}}, "", false},
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

func TestSelectCandidateWeightedSignals(t *testing.T) {
	for _, tc := range []struct {
		name       string
		track      Track
		candidates []Candidate
		want       string
	}{
		{"punctuation and labels", Track{Title: "Don't Stop: The Music", Artist: "Artist"}, []Candidate{{Title: "Dont Stop — The Music [Single Version] (Radio Edit) (Original Mix) [Official Audio] (2007) [OST: The Film]", Artists: []string{"Artist"}, URI: "match"}}, "match"},
		{"unknown suffix does not replace short title", Track{Title: "Home", Artist: "Artist"}, []Candidate{{Title: "Home Again", Artists: []string{"Artist"}, URI: "different"}}, ""},
		{"matching duration supports token overlap", Track{Title: "The Long Road Home", Artist: "Artist", DurationMs: 180000}, []Candidate{{Title: "Long Road Home", Artists: []string{"Artist"}, DurationMs: 182500, URI: "match"}}, "match"},
		{"matching duration outweighs album", Track{Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 180000}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, Album: "Album", URI: "unknown"}, {Title: "Song", Artists: []string{"Artist"}, DurationMs: 183000, URI: "match"}}, "match"},
		{"long duration mismatch lowers rank", Track{Title: "Song", Artist: "Artist", DurationMs: 180000}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, DurationMs: 195001, URI: "long"}, {Title: "Song", Artists: []string{"Artist"}, URI: "match"}}, "match"},
		{"missing metadata", Track{Title: "Song", Artist: "Artist", DurationMs: 180000, ISRC: "USABC2300001", Album: "Album"}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, URI: "match"}}, "match"},
		{"matching ISRC outweighs album", Track{Title: "Song", Artist: "Artist", ISRC: "US-ABC-23-00001", Album: "Album"}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, ISRC: "USABC2300002", Album: "Album", URI: "different"}, {Title: "Song", Artists: []string{"Artist"}, ISRC: "usabc2300001", URI: "match"}}, "match"},
		{"same requested edition", Track{Title: "Song (Sped Up)", Artist: "Artist"}, []Candidate{{Title: "Song", Artists: []string{"Artist"}, URI: "original"}, {Title: "Song — Speed Up", Artists: []string{"Artist"}, URI: "match"}}, "match"},
		{"track named Live", Track{Title: "Live", Artist: "Artist"}, []Candidate{{Title: "Live", Artists: []string{"Artist"}, URI: "match"}}, "match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectCandidate(tc.track, tc.candidates)
			if got.URI != tc.want || ok != (tc.want != "") {
				t.Fatalf("URI = %q, found = %v, want %q", got.URI, ok, tc.want)
			}
		})
	}
}

func TestSelectCandidateRejectsUnexpectedVersions(t *testing.T) {
	track := Track{Title: "The Long Road Home", Artist: "Artist", DurationMs: 180000, ISRC: "USABC2300001"}
	for _, title := range []string{
		"The Long Road Home [Live at Wembley] [Official Audio]",
		"The Long Road Home - Club Remix",
		"The Long Road Home Club Remix",
		"The Long Road Home (Cover) [Bonus Track]",
		"The Long Road Home - Live [Bonus Track]",
		"The Long Road Home - Live - Wembley",
		"The Long Road Home Remix by DJ",
		"The Long Road Home: Instrumental",
		"The Long Road Home | Karaoke",
		"The Long Road Home—Speed Up",
		"The Long Road Home Slowed & Reverb",
		"The Long Road Home (Live from the Original Soundtrack)",
		"The Long Road Home (Acoustic Version)",
		"The Long Road Home (Twin Ver.)",
		"The Long Road Home - En Vivo",
	} {
		t.Run(title, func(t *testing.T) {
			candidates := []Candidate{{Title: title, Artists: []string{"Artist"}, DurationMs: track.DurationMs, ISRC: track.ISRC, URI: "wrong"}}
			if got, ok := selectCandidate(track, candidates); ok {
				t.Fatalf("unexpected match %q", got.URI)
			}
		})
	}
}

func TestSelectCandidateCorroboratesTitleQualifiers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		track      Track
		candidates []Candidate
		want       string
	}{
		{"joint credit in brackets", Track{Title: "Song", Artist: "Artist, Guest"}, []Candidate{
			{Title: "Song", Artists: []string{"Artist"}, URI: "solo"},
			{Title: "Song (with Guest)", Artists: []string{"Artist", "Guest"}, URI: "joint"},
		}, "joint"},
		{"named edition", Track{Title: "Song (Twin Ver.)", Artist: "Artist"}, []Candidate{
			{Title: "Song", Artists: []string{"Artist"}, URI: "studio"},
			{Title: "Song - Twin Version", Artists: []string{"Artist"}, URI: "twin"},
		}, "twin"},
		{"alternate script in the source", Track{Title: "Детство (Childhood)", Artist: "Artist", fromYouTube: true}, []Candidate{
			{Title: "Детство (Remix)", Artists: []string{"Artist"}, URI: "remix"},
			{Title: "Детство", Artists: []string{"Artist"}, URI: "studio"},
		}, "studio"},
		{"director credit is not a recording qualifier", Track{Title: "Song (Directed by Someone)", Artist: "Artist", fromYouTube: true}, []Candidate{
			{Title: "Song", Artists: []string{"Other Artist"}, URI: "homonym"},
			{Title: "Song (Live)", Artists: []string{"Artist"}, URI: "live"},
			{Title: "Song", Artists: []string{"Artist"}, URI: "studio"},
		}, "studio"},
		{"archive description with full-width artist separator", Track{Title: "Night Market ｜ Glass Orchard (Full song from a reel)", Artist: "Archive Desk", fromYouTube: true}, []Candidate{
			{Title: "Glass Orchard", Artists: []string{"Other Band"}, URI: "homonym"},
			{Title: "Glass Orchard (Acoustic)", Artists: []string{"Night Market"}, URI: "acoustic"},
			{Title: "Glass Orchard", Artists: []string{"Night Market"}, URI: "studio"},
		}, "studio"},
		{"unverified curator prefix stays part of the credit", Track{Title: "Unknown Desk: Night Market ｜ Glass Orchard", Artist: "Archive Desk", fromYouTube: true}, []Candidate{
			{Title: "Glass Orchard", Artists: []string{"Night Market"}, URI: "studio"},
		}, ""},
		{"label upload with artist last and corroborated guest", Track{Title: "Glass Orchard (with River Band) — Night Market [Клип]", Artist: "Archive Records", fromYouTube: true}, []Candidate{
			{Title: "Glass Orchard", Artists: []string{"Night Market"}, URI: "solo"},
			{Title: "Glass Orchard", Artists: []string{"Night Market", "River Band"}, URI: "joint"},
		}, "joint"},
		{"recording and rerecording stay distinct", Track{Title: "Glass Orchard / Night Market {HQ}", Artist: "Archive Records", fromYouTube: true}, []Candidate{
			{Title: "Glass Orchard (Anniversary Re-recording)", Artists: []string{"Night Market"}, URI: "rerecording"},
			{Title: "Glass Orchard", Artists: []string{"Night Market"}, URI: "studio"},
		}, "studio"},
		{"same-script subtitle stays part of the title", Track{Title: "Song (A Longer Subtitle)", Artist: "Artist", fromYouTube: true}, []Candidate{
			{Title: "Song", Artists: []string{"Artist"}, URI: "other"},
			{Title: "Song - A Longer Subtitle", Artists: []string{"Artist"}, URI: "full-title"},
		}, "full-title"},
		{"candidate album corroborates the bracket", Track{Title: "Song (The Album)", Artist: "Artist", fromYouTube: true}, []Candidate{
			{Title: "Song", Album: "Other Album", Artists: []string{"Artist"}, URI: "other"},
			{Title: "Song", Album: "The Album", Artists: []string{"Artist"}, URI: "studio"},
		}, "studio"},
		{"every written artist is required", Track{Title: "Artist x Guest - Song", Artist: "Channel", fromYouTube: true}, []Candidate{
			{Title: "Song", Artists: []string{"Artist"}, URI: "solo"},
		}, ""},
		{"compound credit names can match", Track{Title: "Artist x Guest - Song", Artist: "Channel", fromYouTube: true}, []Candidate{
			{Title: "Song", Artists: []string{"Artist", "Guest"}, URI: "joint"},
		}, "joint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectCandidate(tc.track, tc.candidates)
			if got.URI != tc.want || ok != (tc.want != "") {
				t.Fatalf("URI = %q, found = %v, want %q", got.URI, ok, tc.want)
			}
		})
	}
}

func TestSpotifySearchQueryBudget(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	const isrc = "isrc:USABC2300001"
	const filtered = "track:Song artist:Artist"
	const broad = "Song Artist"
	const matched = `{"tracks":{"items":[{"name":"Song","uri":"matched","duration_ms":180000,"external_ids":{"isrc":"USABC2300001"},"artists":[{"name":"Artist"},{"name":"Guest"}]}]}}`
	const empty = `{"tracks":{"items":[]}}`
	for _, tc := range []struct {
		name      string
		mode      SpotifySearchMode
		code      string
		queries   []string
		response  map[string]string
		wantURI   string
		wantStats SpotifySearchStats
	}{
		{"clean first query", SpotifySearchStandard, "", []string{filtered}, map[string]string{filtered: matched}, "matched", SpotifySearchStats{FirstRequests: 1, FirstMatches: 1}},
		{"ISRC hit", SpotifySearchStandard, "us-abc-23-00001", []string{isrc}, map[string]string{isrc: matched}, "matched", SpotifySearchStats{FirstRequests: 1, FirstMatches: 1}},
		{"ISRC miss then match", SpotifySearchStandard, "USABC2300001", []string{isrc, filtered}, map[string]string{filtered: matched}, "matched", SpotifySearchStats{FirstRequests: 1, FallbackRequests: 1, FallbackMatches: 1}},
		{"ISRC and text miss", SpotifySearchStandard, "USABC2300001", []string{isrc, filtered}, nil, "", SpotifySearchStats{FirstRequests: 1, FallbackRequests: 1}},
		{"ISRC miss in broad mode", SpotifySearchBroadFirst, "USABC2300001", []string{isrc, broad}, nil, "", SpotifySearchStats{FirstRequests: 1, FallbackRequests: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				query := r.URL.Query().Get("q")
				queries = append(queries, query)
				body := tc.response[query]
				if body == "" {
					body = empty
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			track := Track{Title: "Song [Radio Edit] (feat. Guest)", Artist: "Artist (Official Audio), Guest", ISRC: tc.code}
			search, stats := NewSpotifySearch("token", tc.mode)
			candidates, err := search(context.Background(), track)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := selectCandidate(track, candidates)
			if got.URI != tc.wantURI || !reflect.DeepEqual(queries, tc.queries) || stats() != tc.wantStats {
				t.Fatalf("match=%q queries=%v stats=%+v", got.URI, queries, stats())
			}
			if got.URI != "" && (got.DurationMs != 180000 || got.ISRC != "USABC2300001") {
				t.Fatalf("metadata = %+v", got)
			}
		})
	}
}

func TestSpotifySongMetadataAndTransferJSON(t *testing.T) {
	var song spotify.Song
	wire := `{"type":"track","name":"Song","artists":[{"name":"Artist"}],"album":{"name":"Album"},"duration_ms":180000,"external_ids":{"isrc":"USABC2300001"}}`
	if err := json.Unmarshal([]byte(wire), &song); err != nil {
		t.Fatal(err)
	}
	tracks := TracksFromSpotify([]spotify.Song{song})
	want := Track{Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 180000, ISRC: "USABC2300001"}
	if !reflect.DeepEqual(tracks, []Track{want}) {
		t.Fatalf("tracks = %+v", tracks)
	}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{song, `{"type":"track","name":"Song","artists":[{"name":"Artist"}],"album":{"name":"Album"}}`},
		{tracks[0], `{"title":"Song","artist":"Artist","album":"Album"}`},
	} {
		data, err := json.Marshal(tc.value)
		if err != nil || string(data) != tc.want {
			t.Fatalf("JSON = %s, err = %v, want %s", data, err, tc.want)
		}
	}
}

func TestSpotifySearchContinuesAfterShortRateLimit(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var blockedAt time.Time
	var calls []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("q")
		calls = append(calls, query)
		if len(calls) == 2 {
			blockedAt = time.Now()
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"1"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
		if len(calls) == 3 && time.Since(blockedAt) < time.Second {
			t.Error("Spotify search retried before Retry-After")
		}
		body := `{"tracks":{"items":[{"name":"Song","uri":"song","artists":[{"name":"Artist"}]},{"name":"Other","uri":"other","artists":[{"name":"Artist"}]}]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	search, stats := NewSpotifySearch("token", SpotifySearchStandard)
	track := Track{Title: "Song", Artist: "Artist"}
	if _, err := search(context.Background(), track); err != nil {
		t.Fatal(err)
	}
	results, err := TransferTracks(context.Background(), []Track{track, {Title: "Other", Artist: "Artist"}}, search)
	wantCalls := []string{"track:Song artist:Artist", "track:Other artist:Artist", "track:Other artist:Artist"}
	if err != nil || !reflect.DeepEqual(calls, wantCalls) || !reflect.DeepEqual(collectFoundURIs(results), []string{"song", "other"}) {
		t.Fatalf("calls=%v, results=%+v, err=%v", calls, results, err)
	}
	if got := stats(); got.FirstRequests != 3 || got.FallbackRequests != 0 || got.SharedQueries != 1 {
		t.Fatalf("stats=%+v", got)
	}
}

func TestSpotifySearchStopsSendingQueuedRequestsOnLongRateLimit(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"8932"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	search, stats := NewSpotifySearch("token", SpotifySearchStandard)
	tracks := []Track{{Title: "One", Artist: "Artist"}, {Title: "Two", Artist: "Artist"}, {Title: "Three", Artist: "Artist"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := TransferTracks(ctx, tracks, search)
	var limited *spotify.RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfterSeconds != 8932 || calls != 1 || stats().FirstRequests != 1 {
		t.Fatalf("err=%v, calls=%d, stats=%+v", err, calls, stats())
	}
}

func TestSpotifyFallbackUsesArtistInVideoTitle(t *testing.T) {
	for _, videoTitle := range []string{
		"Lynyrd Skynyrd - Sweet Home Alabama",
		"1. Sweet Home Alabama - Lynyrd Skynyrd (with Lyrics)",
		"Lynyrd Skynyrd Sweet Home Alabama Lyrics!",
	} {
		t.Run(videoTitle, func(t *testing.T) {
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			var queries []string
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				query := r.URL.Query().Get("q")
				queries = append(queries, query)
				body := `{"tracks":{"items":[]}}`
				if !strings.HasPrefix(query, "track:") {
					body = `{"tracks":{"items":[
						{"name":"Sweet Home Alabama","uri":"wrong","artists":[{"name":"Cover Artist"}]},
						{"name":"Sweet Home Alabama","uri":"matched","artists":[{"name":"Lynyrd Skynyrd"}]}
					]}}`
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			tracks := TracksFromYouTube([]youtube.Video{
				{Title: videoTitle, ChannelTitle: "Uploader"},
				{Title: "Deleted video", VideoId: "deleted"},
				{Title: "Private video", VideoId: "private"},
			})
			search, stats := NewSpotifySearch("token", SpotifySearchStandard)
			results, err := TransferTracks(context.Background(), tracks, search)
			if err != nil {
				t.Fatal(err)
			}
			if len(queries) != 1 || strings.Contains(queries[0], "Uploader") ||
				stats().FirstRequests != 1 || stats().FallbackRequests != 0 {
				t.Fatalf("queries=%v stats=%+v", queries, stats())
			}
			if !results[0].Found || results[0].URI != "matched" || results[0].Match.Artist != "Lynyrd Skynyrd" ||
				!results[1].Unavailable || !results[2].Unavailable {
				t.Fatalf("results=%+v", results)
			}
		})
	}
}

func TestSpotifyRetriesBadGatewayForYouTubeTitle(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if query := r.URL.Query().Get("q"); query != "Here I Go Again - Whitesnake" {
			t.Errorf("неверный запрос: %q", query)
		}
		status := http.StatusOK
		body := `{"tracks":{"items":[{"name":"Here I Go Again","uri":"match","artists":[{"name":"Whitesnake"}]}]}}`
		if calls == 1 {
			status = http.StatusBadGateway
			body = "{}"
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	search, stats := NewSpotifySearch("token", SpotifySearchStandard)
	tracks := TracksFromYouTube([]youtube.Video{{Title: "Here I Go Again-Lyrics-Whitesnake", ChannelTitle: "eggs712"}})
	results, err := TransferTracks(context.Background(), tracks, search)
	if err != nil || len(results) != 1 || results[0].URI != "match" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if calls != 2 || stats().FirstRequests != 2 || stats().FallbackRequests != 0 {
		t.Fatalf("calls=%d stats=%+v", calls, stats())
	}
}
