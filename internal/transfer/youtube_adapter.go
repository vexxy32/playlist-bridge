package transfer

import (
	"regexp"
	"strings"

	"playlist-bridge/internal/youtube"
)

var videoLabels = regexp.MustCompile(`(?i)[(\[]\s*(?:official(?:\s+(?:music\s+video|audio|lyric\s+video|(?:hd|4k)\s+video|video))?|(?:hd|4k)\s+official\s+(?:music\s+)?video|(?:official\s+)?(?:music|lyric|lyrics)\s+video|(?:hd|4k)(?:\s+remaster(?:ed)?)?)\s*[)\]]`)
var videoOnlyLabels = regexp.MustCompile(`(?i)[(\[]\s*(?:video version|director['’]s cut|as featured in [^\])]+|(?:us|uk) version\s*[-–—]\s*official(?: music)? video)\s*[)\]]`)
var cleanVideoLabel = regexp.MustCompile(`(?i)[(\[]\s*official(?: music)? video\s*[-–—]\s*(clean(?: version)?)\s*[)\]]`)
var videoArtistSeparator = regexp.MustCompile(`\s+[-–—|]\s+`)

func videoTrack(video youtube.Video) Track {
	artist := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(video.ChannelTitle), " - Topic"))
	title := strings.TrimSpace(video.Title)
	if split := videoArtistSeparator.FindStringIndex(title); split != nil {
		prefix, remainder := title[:split[0]], title[split[1]:]
		channel := strings.ToLower(artist)
		primary, _, _ := strings.Cut(prefix, ", ")
		if !strings.HasSuffix(strings.TrimSpace(video.ChannelTitle), " - Topic") &&
			(strings.HasSuffix(channel, "vevo") || strings.HasSuffix(channel, "records") ||
				sameArtist(primary, channel)) {
			artist, title = prefix, remainder
		}
	}
	if label := strings.LastIndex(title, " | "); label >= 0 && sameArtist(title[label+3:], video.ChannelTitle) {
		title = title[:label]
	}
	title = cleanVideoLabel.ReplaceAllString(title, " ($1)")
	title = videoLabels.ReplaceAllString(title, " ")
	title = videoOnlyLabels.ReplaceAllString(title, " ")
	return Track{Title: cleanSpacing(title), Artist: cleanSpacing(artist)}
}

// TracksFromYouTube подготавливает сведения о видео для поиска треков.
func TracksFromYouTube(videos []youtube.Video) []Track {
	tracks := make([]Track, 0, len(videos))
	for _, video := range videos {
		tracks = append(tracks, videoTrack(video))
	}
	return tracks
}
