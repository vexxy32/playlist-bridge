//go:build playlist_bridge_demo

package server

import (
	"fmt"
	"net/http"
	"strconv"
)

const scaleFixtureSize = 300

func scaleFixtureEntry(i int) (string, string, string) {
	titles := []string{"Silver Rain", "Nocne światło", "Último andén", "Échos du port", "Тихий причал", "Glass Orchard"}
	artists := []string{"Luz Marina", "Cichy Port", "Duo Équinoxe", "Северный Берег", "Pátio Vazio"}
	return fmt.Sprintf("%s %03d", titles[i%len(titles)], i+1), artists[(i+i/6)%len(artists)], fmt.Sprintf("scale%03d", i+1)
}

func scaleYouTubeToSpotifyFixture() offlineFixture {
	routes := make(map[offlineRoute][]offlineReply)
	var videos []wireObject
	var expected []string
	missed, unavailable := 0, 0
	for i := range scaleFixtureSize - 24 {
		title, artist, id := scaleFixtureEntry(i)
		if i%23 == 0 {
			videos = append(videos, sourceVideo("Private video", "", id))
			unavailable++
			continue
		}
		raw, channel, query := title, artist+" - Topic", "track:"+title+" artist:"+artist
		if i%3 == 0 {
			raw, channel, query = title+" - "+artist+" (Lyrics) [1080p / 60fps]", "Harbor Records", title+" - "+artist
		}
		videos = append(videos, sourceVideo(raw, channel, id))
		hits := []wireObject{spotifySong(title, id+"other", "Other Band")}
		if i%11 == 0 {
			missed++
		} else {
			hits = append(hits, spotifySong(title+" (Live)", id+"live", artist), spotifySong(title, id, artist))
			expected = append(expected, "spotify:track:"+id)
		}
		routes[spotifySearch(query)] = []offlineReply{ok(spotifyHits(hits...))}
	}
	for i, added := 1, 0; added < 24; i++ {
		if i%11 == 0 || i%23 == 0 {
			continue
		}
		added++
		videos = append(videos, videos[i])
		_, _, id := scaleFixtureEntry(i)
		expected = append(expected, "spotify:track:"+id)
	}
	for start := 0; start < len(videos); start += 50 {
		end := min(start+50, len(videos))
		page, next := "", ""
		if start > 0 {
			page = strconv.Itoa(start)
		}
		if end < len(videos) {
			next = strconv.Itoa(end)
		}
		routes[youtubeRead(page)] = []offlineReply{ok(videoPage(next, videos[start:end]...))}
	}
	searches := scaleFixtureSize - 24 - unavailable
	youtubeToSpotify(routes)
	appendRoute := offlineRoute{http.MethodPost, spotifyHost, "/v1/playlists/offlineSpotify/items", ""}
	routes[appendRoute] = nil
	for offset := 0; offset < len(expected); offset += 100 {
		routes[appendRoute] = append(routes[appendRoute], offlineReply{status: 201, body: wireObject{"snapshot_id": "scale"}, wantWriteSize: min(100, len(expected)-offset)})
	}
	return offlineFixture{
		id: "youtube-spotify-scale-300", label: "YouTube → Spotify · объём, дубли и недоступные видео (300)",
		description: "Синтетический набор с повторяющимися названиями: страницы плейлиста, дубликаты и отсутствующие видео.",
		source:      "youtube", destination: "spotify", routes: routes, wantIDs: expected, wantStatus: 200,
		wantFound: len(expected), wantMissed: missed, wantUnavailable: unavailable, wantRequests: 6 + searches + 1 + len(routes[appendRoute]),
	}
}

func scaleSpotifyToYouTubeFixture() offlineFixture {
	routes := make(map[offlineRoute][]offlineReply)
	var songs []wireObject
	var expected []string
	missed := 0
	for i := range scaleFixtureSize - 24 {
		title, artist, id := scaleFixtureEntry(i)
		song := spotifySong(title, id, artist)
		songs = append(songs, song)
		hits := []wireObject{searchVideo(title+" (Slowed)", artist+" - Topic", id+"slowed")}
		if i%11 == 0 {
			missed++
		} else {
			label, channel := title, artist+" - Topic"
			if i%2 == 0 {
				label, channel = artist+" — "+title+" [4K | HDR]", "Archive Records"
			}
			hits = append(hits, searchVideo(label, channel, id))
			expected = append(expected, id)
		}
		routes[youtubeSearch(artist+" "+title)] = []offlineReply{ok(videoHits(hits...))}
	}
	for i, added := 1, 0; added < 24; i++ {
		if i%11 == 0 {
			continue
		}
		songs = append(songs, songs[i])
		_, _, id := scaleFixtureEntry(i)
		expected = append(expected, id)
		added++
	}
	for start := 0; start < len(songs); start += 50 {
		end := min(start+50, len(songs))
		next := ""
		if end < len(songs) {
			next = fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/items?offset=%d", offlineSpotifyID, end)
		}
		routes[spotifyRead(strconv.Itoa(start))] = []offlineReply{ok(songPage(next, songs[start:end]...))}
	}
	return offlineFixture{
		id: "spotify-youtube-scale-300", label: "Spotify → YouTube · объём, дубли и неподходящие версии (300)",
		description: "Синтетический набор с повторяющимися названиями: дубликаты, замедленные версии и добавление видео по одному.",
		source:      "spotify", destination: "youtube", routes: withYouTubeWrites(len(expected), routes), wantIDs: expected, wantStatus: 200,
		wantFound: len(expected), wantMissed: missed, wantRequests: 6 + scaleFixtureSize - 24 + 1 + len(expected),
	}
}
