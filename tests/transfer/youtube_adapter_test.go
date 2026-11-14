//go:build playlist_bridge_tests

package transfer

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"playlist-bridge/internal/youtube"
)

func TestTracksFromYouTube(t *testing.T) {
	videos := []youtube.Video{{Title: "Song", ChannelTitle: " Artist - Topic "}, {Title: "Other", ChannelTitle: "Topic Records"}}
	want := []Track{{Title: "Song", Artist: "Artist", fromYouTube: true}, {Title: "Other", Artist: "Topic Records", fromYouTube: true}}
	if got := TracksFromYouTube(videos); !reflect.DeepEqual(got, want) {
		t.Fatalf("получено %+v, ожидалось %+v", got, want)
	}
}

func TestVideoTrackArtistAndTitle(t *testing.T) {
	for _, tc := range []struct {
		title, channel, artist, want string
	}{
		{"Artist — Song (Official Audio)", "Artist - Topic", "Artist", "Song"},
		{"Artist-Side - Song", "Artist-Side", "Artist-Side", "Song"},
		{"Artist—Song", "artist", "Artist", "Song"},
		{"Artist-Song", "Artist", "Artist", "Song"},
		{"Artist|Song | Artist", "Artist", "Artist", "Song"},
		{"Song | Artist", "Artist", "Artist", "Song"},
		{"Artist &amp; Band | Don&#39;t Stop", "Artist &amp; Band", "Artist & Band", "Don't Stop"},
		{"Artist - Song - Live", "ArtistVEVO", "Artist", "Song - Live"},
		{"The Weeknd - Can't Feel My Face", "TheWeekndVEVO", "The Weeknd", "Can't Feel My Face"},
		{"Sia - Chandelier (Official Music Video 1080p 60fps)", "Sia", "Sia", "Chandelier"},
		{"The Chainsmokers - Closer (Lyric) ft. Halsey", "ChainsmokersVEVO", "The Chainsmokers", "Closer ft. Halsey"},
		{"Justin Bieber - Love Yourself (PURPOSE : The Movement)", "JustinBieberVEVO", "Justin Bieber", "Love Yourself"},
		{"Artist - Song (Live) (Official Video 4K)", "ArtistVEVO", "Artist", "Song (Live)"},
		{"Artist - Song (Unlisted Edition)", "ArtistVEVO", "Artist", "Song (Unlisted Edition)"},
		{"Trap Nation: Marshmello - Alone", "Trap Nation", "Marshmello", "Alone"},
		{"Silk Sonic (Bruno Mars & Anderson .Paak) - Leave The Door Open", "Bruno Mars", "Silk Sonic, Bruno Mars, Anderson .Paak", "Leave The Door Open"},
		{"Artist - Song", "Example Records", "Artist", "Song"},
		{"Darin feat. Eloise - Falling In love (Music Video)", "Darin Music", "Darin feat. Eloise", "Falling In love"},
		{"Bella Kay - iloveitiloveitiloveit (Lyrics)", "VibesOnly", "Bella Kay", "iloveitiloveitiloveit"},
		{"Alex Warren - Ordinary (Lyrics)", "7clouds", "Alex Warren", "Ordinary"},
		{"Lady Gaga, Bruno Mars - Die With A Smile (Lyrics)", "SyrebralVibes", "Lady Gaga, Bruno Mars", "Die With A Smile"},
		{"Song - Live (Lyrics)", "Lyrics Channel", "Lyrics Channel", "Song - Live"},
		{"Part One - Part Two (Lyrics)", "Artist - Topic", "Artist", "Part One - Part Two"},
		{"Song - Live", "Example Records", "Example Records", "Song - Live"},
		{"Part One - Part Two", "Artist", "Artist", "Part One - Part Two"},
		{"Artist - Song (Official Video - Clean Version)", "Artist", "Artist", "Song (Clean Version)"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			got := videoTrack(youtube.Video{Title: tc.title, ChannelTitle: tc.channel})
			if got.Title != tc.want || got.Artist != tc.artist {
				t.Fatalf("track = %+v, want %q by %q", got, tc.want, tc.artist)
			}
		})
	}
}

func TestYouTubeMusicCreditsAndVideoLabels(t *testing.T) {
	for _, tc := range []struct {
		title, channel string
		candidate      Candidate
	}{
		{"Artist - Song ft. Guest Film 7 Soundtrack", "Artist", Candidate{Title: "Song", Artists: []string{"Artist", "Guest"}, URI: "match"}},
		{"Artist - Song (Vertical Video) ft. Guest", "Artist", Candidate{Title: "Song", Artists: []string{"Artist", "Guest"}, URI: "match"}},
		{"Artist - Song [Official Dance Video]", "Label Records", Candidate{Title: "Song", Artists: []string{"Artist"}, URI: "match"}},
	} {
		t.Run(tc.title, func(t *testing.T) {
			track := videoTrack(youtube.Video{Title: tc.title, ChannelTitle: tc.channel})
			got, ok := selectCandidate(track, []Candidate{tc.candidate})
			if !ok || got.URI != tc.candidate.URI {
				t.Fatalf("track=%+v candidate=%+v got=%+v", track, tc.candidate, got)
			}
		})
	}
}
func TestSearchYouTubeUsesExpectedArtistInVideoTitles(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	const body = `{"items":[
		{"id":{"kind":"youtube#video","videoId":"live"},"snippet":{"title":"Artist — Song (Live)","channelTitle":"Label"}},
		{"id":{"kind":"youtube#video","videoId":"match"},"snippet":{"title":"Artist|Song [From the Film Soundtrack]","channelTitle":"Label"}},
		{"id":{"kind":"youtube#video","videoId":"cover"},"snippet":{"title":"Other Artist - Song","channelTitle":"Other Artist"}}
	]}`
	calls := 0
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if query := r.URL.Query().Get("q"); query != "Artist Song" {
			t.Errorf("query = %q", query)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	tracks := []Track{{Title: "Song", Artist: "Artist", DurationMs: 180000, ISRC: "USABC2300001", Album: "Album"}}
	got, err := TransferTracks(context.Background(), tracks, SearchYouTube("key"))
	if err != nil || len(got) != 1 || got[0].URI != "match" || calls != 1 {
		t.Fatalf("results = %+v, calls = %d, err = %v", got, calls, err)
	}
}

func TestVideoTrackMatchesArtistCreditsAndPreservesVersions(t *testing.T) {
	tests := []struct {
		name, title, channel string
		candidates           []Candidate
		want                 string
	}{
		{
			name: "VEVO alias requires primary artist", title: "Someone Like You", channel: "AdeleVEVO",
			candidates: []Candidate{
				{Title: "Someone Like You", Artists: []string{"Boyce Avenue", "Adele"}, URI: "cover"},
				{Title: "Someone Like You", Artists: []string{"Adele"}, URI: "official"},
			}, want: "official",
		},
		{
			name: "compact article in channel alias", title: "Blinding Lights", channel: "TheWeekndVEVO",
			candidates: []Candidate{
				{Title: "Blinding Lights", Artists: []string{"The Weeknd"}, URI: "official"},
			}, want: "official",
		},
		{
			name: "expected collaborators must all be present", title: "Lil Baby x 42 Dugg - We Paid (Official Video)", channel: "LilBabyVEVO",
			candidates: []Candidate{
				{Title: "We Paid", Artists: []string{"Lil Baby"}, URI: "solo"},
				{Title: "We Paid", Artists: []string{"Lil Baby", "42 Dugg"}, URI: "official"},
			}, want: "official",
		},
		{
			name: "video suffix does not remove live edition", title: "Artist - Song (Live) (Official Video 4K)", channel: "ArtistVEVO",
			candidates: []Candidate{
				{Title: "Song", Artists: []string{"Artist"}, URI: "studio"},
				{Title: "Song (Live)", Artists: []string{"Artist"}, URI: "official"},
			}, want: "official",
		},
		{
			name: "generic label is not artist evidence", title: "Song", channel: "Independent Records",
			candidates: []Candidate{
				{Title: "Song", Artists: []string{"Artist"}, URI: "unknown"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			track := videoTrack(youtube.Video{Title: tc.title, ChannelTitle: tc.channel})
			got, _ := selectCandidate(track, tc.candidates)
			if got.URI != tc.want {
				t.Fatalf("track=%+v, got %q, want %q", track, got.URI, tc.want)
			}
		})
	}
}
