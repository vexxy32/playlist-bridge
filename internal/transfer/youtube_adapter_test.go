package transfer

import (
	"playlist-bridge/internal/youtube"
	"reflect"
	"testing"
)

func TestTracksFromYouTube(t *testing.T) {
	videos := []youtube.Video{{Title: "Song", ChannelTitle: " Artist - Topic "}, {Title: "Other", ChannelTitle: "Topic Records"}}
	want := []Track{{Title: "Song", Artist: "Artist"}, {Title: "Other", Artist: "Topic Records"}}
	if got := TracksFromYouTube(videos); !reflect.DeepEqual(got, want) {
		t.Fatalf("получено %+v, ожидалось %+v", got, want)
	}
}
