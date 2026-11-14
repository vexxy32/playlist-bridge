package transfer

import (
	"html"
	"regexp"
	"strings"

	"playlist-bridge/internal/youtube"
)

var videoLabels = regexp.MustCompile(`(?i)[(\[]\s*(?:official(?:\s+(?:(?:(?:hd|4k)\s+)?(?:music\s+video|video)|audio|lyric\s+video))?|(?:hd|4k)\s+official\s+(?:music\s+)?video|(?:official\s+)?(?:music|lyric|lyrics)\s+video|(?:hd|4k)(?:\s+remaster(?:ed)?)?)(?:\s+(?:hd|4k|8k|\d{3,4}p|\d{2,3}fps))*\s*[)\]]`)
var videoOnlyLabels = regexp.MustCompile(`(?i)[(\[]\s*(?:video version|purpose\s*:\s*the\s+movement|director['’]s cut|as featured in [^\])]+|(?:us|uk) version\s*[-–—]\s*official(?: music)? video)\s*[)\]]`)
var cleanVideoLabel = regexp.MustCompile(`(?i)[(\[]\s*official(?: music)? video\s*[-–—]\s*(clean(?: version)?)\s*[)\]]`)
var videoQualityLabel = regexp.MustCompile(`(?i)^\s*(?:(?:\d{3,4}p|\d{2,3}fps|[248]k|uhd|hdr)(?:\s*[/,|]\s*|\s+))*\s*(?:\d{3,4}p|\d{2,3}fps|[248]k|uhd|hdr)\s*$`)
var videoArtistSeparator = regexp.MustCompile(`\s*[-–—|]\s*`)
var vevoSuffix = regexp.MustCompile(`(?i)vevo$`)
var topicSuffix = regexp.MustCompile(`(?i)\s+-\s+topic$`)

var videoTrackNumber = regexp.MustCompile(`^\d{1,3}[.)]\s+`)
var byCredit = regexp.MustCompile(`(?i)\s+by\s+`)
var inlineLyrics = regexp.MustCompile(`(?i)\s*[-–—|~:]\s*(?:with\s+)?lyrics\s*[-–—|~:]\s*`)
var soundtrackCredit = regexp.MustCompile(`(?i)\s+(?:from\s+)?(?:the\s+)?(?:original\s+)?(?:motion picture\s+)?[a-z]+\s+\d+\s+soundtrack\s*$`)
var lyricsSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—|~:+]\s*)?(?:\bwith\s+|\bw/\s*)?\blyrics(?:\s*(?:on screen|[+&]\s*hq sound))?[!.]*\s*$`)
var uploadChannelSuffixes = []string{"music", "records", "lyrics", "vibes"}
var videoCreditLabels = regexp.MustCompile("(?i)[(\\[]\\s*(?:(?:official\\s+)?(?:music video|lyric video|video|audio|(?:(?:color coded|with)\\s+|w/\\s*)?lyrics?(?:\\s*[+&]\\s*hq sound)?)|visuali[sz]er)\\s*[)\\]]")

func videoTrack(video youtube.Video) Track {
	track := videoTrackForArtist(video, "")
	track.fromYouTube = true
	if strings.TrimSpace(video.ChannelTitle) == "" && (matchText(track.Title) == "deleted video" || matchText(track.Title) == "private video") {
		track.unavailable = true
	} else if cleanSpacing(html.UnescapeString(video.ChannelTitle)) == track.Artist {
		switch {
		case videoVEVOArtist(track.Artist) != "":
			track.videoArtistAlias = videoVEVOArtist(track.Artist)
		case len(titleWords(track.Title)) > 1 && !topicSuffix.MatchString(track.Artist):
			track.videoSearchTitle = track.Title
		}
	}
	return track
}

func videoTrackForArtist(video youtube.Video, expectedArtist string) Track {
	channel := cleanSpacing(html.UnescapeString(video.ChannelTitle))
	artist := topicSuffix.ReplaceAllString(channel, "")
	title := cleanSpacing(html.UnescapeString(video.Title))
	hasVideoLabel := videoCreditLabels.MatchString(title)
	title = cleanVideoLabel.ReplaceAllString(title, " ($1)")
	title = videoCreditLabels.ReplaceAllString(title, " ")
	title = videoLabels.ReplaceAllString(title, " ")
	title = titleBrackets.ReplaceAllStringFunc(title, func(bracket string) string {
		if bracket != title && videoQualityLabel.MatchString(bracket[1:len(bracket)-1]) {
			return " "
		}
		return bracket
	})
	title = cleanSpacing(videoOnlyLabels.ReplaceAllString(title, " "))
	title = videoTrackNumber.ReplaceAllString(title, "")
	title = inlineLyrics.ReplaceAllString(title, " - ")
	title = soundtrackCredit.ReplaceAllString(title, "")
	if stripped := lyricsSuffix.ReplaceAllString(title, ""); stripped != "" {
		title = stripped
	}

	// Некоторые загрузчики указывают название канала перед исполнителем и треком.
	channelPrefix, remainder, hasColon := strings.Cut(title, ":")
	hasUploaderPrefix := hasColon && sameArtist(channelPrefix, channel) &&
		videoArtistSeparator.MatchString(strings.TrimSpace(remainder))
	if hasUploaderPrefix {
		title = strings.TrimSpace(remainder)
	}

	vevoArtist := videoVEVOArtist(artist)
	creditedTo := func(credit, name string) bool {
		return sameArtist(credit, name) || sameArtist(spotifyPrimaryArtist(credit), name) ||
			sameArtist(credit, spotifyPrimaryArtist(name))
	}
	knownArtist := func(credit string) bool {
		return creditedTo(credit, artist) || creditedTo(credit, expectedArtist) ||
			videoArtistAliasMatches(vevoArtist, spotifyPrimaryArtist(credit))
	}
	if videoArtistAliasMatches(vevoArtist, expectedArtist) {
		artist = expectedArtist
	}

	separators := videoArtistSeparator.FindAllStringIndex(title, -1)
	if len(separators) > 0 {
		last := separators[len(separators)-1]
		// Название может заканчиваться исполнителем или каналом, даже если исполнитель указан в начале.
		if suffix := title[last[1]:]; last[0] > 0 && !isVideoEdition(suffix) && (knownArtist(suffix) || sameArtist(suffix, channel)) {
			if knownArtist(suffix) {
				artist = suffix
			}
			title = title[:last[0]]
			separators = separators[:len(separators)-1]
		}
	}
	for _, split := range separators {
		prefix, remainder := title[:split[0]], title[split[1]:]
		if prefix == "" || remainder == "" {
			continue
		}
		if knownArtist(prefix) {
			return Track{Title: cleanSpacing(remainder), Artist: cleanSpacing(prefix)}
		}
		if group, members, ok := videoGroupCredit(prefix); ok &&
			(knownArtist(group) || knownArtist(members[0])) {
			return Track{Title: cleanSpacing(remainder), Artist: group + ", " + strings.Join(members, ", ")}
		}
	}

	// Для роликов лейблов и каналов с текстами учитываем явный разделитель исполнителя и названия.
	if expectedArtist == "" && channel == artist {
		uploadChannel := hasVideoLabel || hasUploaderPrefix
		for _, suffix := range uploadChannelSuffixes {
			uploadChannel = uploadChannel || strings.HasSuffix(artistKey(channel), suffix)
		}
		if uploadChannel {
			for _, split := range separators {
				separator := title[split[0]:split[1]]
				if separator == "-" {
					continue
				}
				prefix, remainder := title[:split[0]], title[split[1]:]
				if prefix != "" && remainder != "" && !isVideoEdition(parseTitle(remainder).base) {
					return Track{Title: cleanSpacing(remainder), Artist: cleanSpacing(prefix), videoSearchTitle: title}
				}
			}
		}
	}
	if expectedArtist != "" {
		for _, split := range byCredit.FindAllStringIndex(title, -1) {
			song, credit := title[:split[0]], title[split[1]:]
			if creditedTo(credit, expectedArtist) {
				return Track{Title: song, Artist: credit}
			}
		}
	}
	return Track{Title: cleanSpacing(title), Artist: cleanSpacing(artist)}
}

func videoVEVOArtist(channel string) string {
	if !vevoSuffix.MatchString(channel) {
		return ""
	}
	return artistKey(vevoSuffix.ReplaceAllString(channel, ""))
}

func videoArtistAliasMatches(alias, artist string) bool {
	if alias == "" {
		return false
	}
	key := artistKey(artist)
	article, hasArticle := strings.CutPrefix(matchText(artist), "the ")
	return key != "" && (alias == key || (hasArticle && alias == "the"+artistKey(article)))
}

func videoGroupCredit(credit string) (string, []string, bool) {
	group, memberCredit, ok := strings.Cut(credit, " (")
	memberCredit, closed := strings.CutSuffix(strings.TrimSpace(memberCredit), ")")
	if !ok || !closed || group == "" || strings.ContainsAny(memberCredit, "()") {
		return "", nil, false
	}
	members := artistSeparators.Split(memberCredit, -1)
	return group, members, len(members) >= 2
}

func isVideoEdition(label string) bool {
	words := strings.Join(titleWords(label), " ")
	return isNeutralTitleLabel(words) || len(versionKinds(words)) > 0 ||
		strings.HasPrefix(words, "clean ") || strings.Contains(words, "remaster")
}

// TracksFromYouTube подготавливает сведения о видео для поиска треков.
func TracksFromYouTube(videos []youtube.Video) []Track {
	tracks := make([]Track, 0, len(videos))
	for _, video := range videos {
		tracks = append(tracks, videoTrack(video))
	}
	return tracks
}
