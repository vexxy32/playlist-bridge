package server

import "net/http"

const (
	spotifyHost = "api.spotify.com"
	youtubeHost = "www.googleapis.com"
)

type wireObject = map[string]any

func offlineFixtures() []offlineFixture {
	return []offlineFixture{
		{
			id: "youtube-labels", label: "YouTube → Spotify: лейблы и две страницы", source: "youtube", destination: "spotify",
			routes: youtubeToSpotify(map[offlineRoute][]offlineReply{
				youtubeRead(""):     {ok(videoPage("next", sourceVideo("Good Times by Finger Eleven", "RockofVengeance", "sourceGood"), sourceVideo("Deleted video", "", "deleted")))},
				youtubeRead("next"): {ok(videoPage("", sourceVideo("Boys Are Back In Town Thin Lizzy (Lyrics)", "Rock Archives", "sourceBoys"), sourceVideo("Good Times by Finger Eleven", "RockofVengeance", "sourceGood")))},
				spotifySearch("Good Times by Finger Eleven"):      {ok(spotifyHits(spotifySong("Good Times", "wrong", "Tribute Band"), spotifySong("Good Times", "good", "Finger Eleven")))},
				spotifySearch("Boys Are Back In Town Thin Lizzy"): {ok(spotifyHits(spotifySong("The Boys Are Back in Town", "boys", "Thin Lizzy")))},
			}),
			wantIDs:    []string{"spotify:track:good", "spotify:track:boys", "spotify:track:good"},
			wantStatus: 200, wantFound: 3, wantUnavailable: 1, wantRequests: 6,
		},
		{
			id: "youtube-title-order", label: "YouTube → Spotify: лейблы, автор в конце и feat.", source: "youtube", destination: "spotify",
			routes: youtubeToSpotify(map[offlineRoute][]offlineReply{
				youtubeRead(""): {ok(videoPage("",
					sourceVideo("Rock That Body - The Black Eyed Peas (Lyrics)", "Pizza Music", "rockSource"),
					sourceVideo("Darin feat. Eloise - Falling In love (Music Video)", "Darin Music", "darinSource"),
					sourceVideo("Bella Kay - iloveitiloveitiloveit (Lyrics)", "VibesOnly", "bellaSource"),
				))},
				spotifySearch("Rock That Body - The Black Eyed Peas"): {ok(spotifyHits(spotifySong("Rock That Body", "tribute", "Cover Band"), spotifySong("Rock That Body", "rock", "Black Eyed Peas")))},
				spotifySearch("Darin - Falling In love"):              {ok(spotifyHits(spotifySong("Falling In Love", "solo", "Darin"), spotifySong("Falling In Love", "darin", "Darin", "Eloise")))},
				spotifySearch("Bella Kay - iloveitiloveitiloveit"):    {ok(spotifyHits(spotifySong("iloveitiloveitiloveit", "bella", "Bella Kay")))},
			}),
			wantIDs: []string{"spotify:track:rock", "spotify:track:darin", "spotify:track:bella"}, wantStatus: 200, wantFound: 3, wantRequests: 6,
		},
		{
			id: "spotify-versions", label: "YouTube → Spotify: ремикс, кавер и пустая выдача", source: "youtube", destination: "spotify",
			routes: youtubeToSpotify(map[offlineRoute][]offlineReply{
				youtubeRead(""): {ok(videoPage("", sourceVideo("This Is The Long Road Home Club Remix", "Artist - Topic", "sourceRemix"), sourceVideo("Nothing Like This", "Band - Topic", "sourceMissing")))},
				spotifySearch("track:This Is The Long Road Home Club Remix artist:Artist"): {ok(spotifyHits(spotifySong("This Is The Long Road Home", "original", "Artist")))},
				spotifySearch("This Is The Long Road Home Club Remix Artist"):              {ok(spotifyHits(spotifySong("This Is The Long Road Home (Club Remix)", "club", "Artist")))},
				spotifySearch("track:Nothing Like This artist:Band"):                       {ok(spotifyHits(spotifySong("Nothing Like This (Cover)", "cover", "Band")))},
				spotifySearch("Nothing Like This Band"):                                    {ok(spotifyHits())},
			}),
			wantIDs: []string{"spotify:track:club"}, wantStatus: 200, wantFound: 1, wantMissed: 1, wantRequests: 7, wantFallback: 2,
		},
		{
			id: "spotify-homonym", label: "YouTube → Spotify: одноимённая запись другого артиста", source: "youtube", destination: "spotify",
			routes: map[offlineRoute][]offlineReply{
				youtubeRead(""):            {ok(videoPage("", sourceVideo("Blue Moon", "WolfVEVO", "original")))},
				spotifySearch("Blue Moon"): {ok(spotifyHits(spotifySong("Blue Moon", "homonym", "Sea Wolf")))},
			},
			wantStatus: 502, wantMissed: 1, wantRequests: 2,
		},
		{
			id: "spotify-no-studio-version", label: "YouTube → Spotify: только концерт и трек с похожим названием", source: "youtube", destination: "spotify",
			routes: map[offlineRoute][]offlineReply{
				youtubeRead(""): {ok(videoPage("", sourceVideo("Long Road Home Tonight", "Band - Topic", "original")))},
				spotifySearch("track:Long Road Home Tonight artist:Band"): {ok(spotifyHits(
					spotifySong("Long Road Home Tonight (Live)", "concert", "Band"),
					spotifySong("Long Road", "short", "Band"),
				))},
				spotifySearch("Long Road Home Tonight Band"): {ok(spotifyHits())},
			},
			wantStatus: 502, wantMissed: 1, wantRequests: 3, wantFallback: 1,
		},
		{
			id: "spotify-retry", label: "YouTube → Spotify: временный ответ 502", source: "youtube", destination: "spotify",
			routes: youtubeToSpotify(map[offlineRoute][]offlineReply{
				youtubeRead(""): {ok(videoPage("", sourceVideo("Here I Go Again-Lyrics-Whitesnake", "eggs712", "sourceRetry")))},
				spotifySearch("Here I Go Again - Whitesnake"): {
					{status: 502, body: wireObject{"error": wireObject{"status": 502, "message": "Bad Gateway"}}},
					ok(spotifyHits(spotifySong("Here I Go Again", "whitesnake", "Whitesnake"))),
				},
			}),
			wantIDs: []string{"spotify:track:whitesnake"}, wantStatus: 200, wantFound: 1, wantRequests: 5, wantRetries: 1,
		},
		{
			id: "spotify-quota", label: "YouTube → Spotify: исчерпана квота 429", source: "youtube", destination: "spotify",
			routes: map[offlineRoute][]offlineReply{
				youtubeRead(""):                         {ok(videoPage("", sourceVideo("Song", "Band - Topic", "quota"), sourceVideo("Private video", "", "private")))},
				spotifySearch("track:Song artist:Band"): {{status: 429, retryAfter: "2", body: wireObject{"error": wireObject{"status": 429, "reason": "QUOTA_EXCEEDED"}}}},
			},
			wantStatus: 429, wantUnavailable: 1, wantPending: 1, wantRequests: 2, wantRetryAfter: "2", fromYouTubeAccount: true,
		},
		{
			id: "spotify-youtube", label: "Spotify → YouTube: лейбл, кавер и переиздание", source: "spotify", destination: "youtube",
			routes: withYouTubeWrites(3, map[offlineRoute][]offlineReply{
				spotifyRead("0"):                              {ok(songPage("https://api.spotify.com/v1/playlists/"+offlineSpotifyID+"/items?offset=3", spotifySong("In the End", "inTheEnd", "Linkin Park"), nil, wireObject{"type": "episode", "name": "Podcast"}))},
				spotifyRead("3"):                              {ok(songPage("", spotifySong("Bring Me to Life", "bring", "Evanescence"), alternateRelease("In the End", "inTheEndReissue", "Linkin Park")))},
				youtubeSearch("Linkin Park In the End"):       {ok(videoHits(searchVideo("In the End (Live)", "Linkin Park - Topic", "live"), searchVideo("Linkin Park - In the End [Official HD Music Video]", "Warner Records", "officialLinkin")))},
				youtubeSearch("Evanescence Bring Me to Life"): {ok(videoHits(searchVideo("Evanescence - Bring Me to Life (Cover)", "Cover Studio", "cover"), searchVideo("Bring Me to Life", "Evanescence - Topic", "evanescence")))},
			}),
			wantIDs: []string{"officialLinkin", "evanescence", "officialLinkin"}, wantStatus: 200, wantFound: 3, wantRequests: 8,
		},
		{
			id: "spotify-youtube-music", label: "Spotify → YouTube Music: гости и версии", source: "spotify", destination: "youtubeMusic",
			routes: withYouTubeWrites(1, map[offlineRoute][]offlineReply{
				spotifyRead("0"): {ok(songPage("", spotifySong("Numb / Encore", "duet", "JAY-Z", "Linkin Park"), spotifySong("Frozen (Club Remix)", "remix", "Artist")))},
				youtubeSearch("JAY-Z, Linkin Park Numb / Encore"): {ok(videoHits(searchVideo("JAY-Z - Numb / Encore", "Unofficial Upload", "solo"), searchVideo("JAY-Z &amp; Linkin Park - Numb / Encore (Official Audio)", "Warner Records", "duet")))},
				youtubeSearch("Artist Frozen (Club Remix)"):       {ok(videoHits(searchVideo("Frozen", "Artist - Topic", "studio")))},
			}),
			wantIDs: []string{"duet"}, wantStatus: 200, wantFound: 1, wantMissed: 1, wantRequests: 5,
		},
		{
			id: "youtube-copy", label: "YouTube → YouTube Music: копирование без поиска", source: "youtube", destination: "youtubeMusic", fromYouTubeAccount: true,
			routes: withYouTubeWrites(2, map[offlineRoute][]offlineReply{
				youtubeRead(""): {ok(videoPage("", sourceVideo("Song | Artist", "Artist", "keep"), sourceVideo("Private video", "", "private"), sourceVideo("Song | Artist", "Artist", "keep")))},
			}),
			wantIDs: []string{"keep", "keep"}, wantStatus: 200, wantFound: 2, wantUnavailable: 1, wantRequests: 4,
		},
		{
			id: "youtube-write-error", label: "Spotify → YouTube: ошибка при добавлении", source: "spotify", destination: "youtube",
			routes: map[offlineRoute][]offlineReply{
				spotifyRead("0"):                                                {ok(songPage("", spotifySong("First", "first", "Band"), spotifySong("Second", "second", "Band")))},
				youtubeSearch("Band First"):                                     {ok(videoHits(searchVideo("First by Band", "Unrelated Label", "first")))},
				youtubeSearch("Band Second"):                                    {ok(videoHits(searchVideo("Second", "Band - Topic", "second")))},
				{http.MethodPost, youtubeHost, "/youtube/v3/playlists", ""}:     {ok(wireObject{"id": "offlineYouTube"})},
				{http.MethodPost, youtubeHost, "/youtube/v3/playlistItems", ""}: {ok(wireObject{"id": "item"}), {status: 403, body: wireObject{"error": wireObject{"code": 403, "message": "The request cannot be completed because you have exceeded your quota."}}}},
			},
			wantIDs: []string{"first"}, wantStatus: 502, wantFound: 2, wantRequests: 6,
		},
	}
}

func youtubeRead(page string) offlineRoute {
	return offlineRoute{http.MethodGet, youtubeHost, "/youtube/v3/playlistItems", page}
}
func spotifyRead(offset string) offlineRoute {
	return offlineRoute{http.MethodGet, spotifyHost, "/v1/playlists/" + offlineSpotifyID + "/items", offset}
}
func youtubeSearch(query string) offlineRoute {
	return offlineRoute{http.MethodGet, youtubeHost, "/youtube/v3/search", query}
}
func spotifySearch(query string) offlineRoute {
	return offlineRoute{http.MethodGet, spotifyHost, "/v1/search", query}
}
func ok(body any) offlineReply { return offlineReply{status: 200, body: body} }

func sourceVideo(title, channel, id string) wireObject {
	return wireObject{"snippet": wireObject{"title": title, "videoOwnerChannelTitle": channel, "resourceId": wireObject{"videoId": id}}}
}

func videoPage(next string, videos ...wireObject) wireObject {
	return wireObject{"nextPageToken": next, "items": videos}
}

func searchVideo(title, channel, id string) wireObject {
	return wireObject{"id": wireObject{"kind": "youtube#video", "videoId": id}, "snippet": wireObject{"title": title, "channelTitle": channel}}
}

func videoHits(videos ...wireObject) wireObject {
	return wireObject{"items": videos}
}

func spotifySong(title, id string, names ...string) wireObject {
	artists := make([]wireObject, len(names))
	for i, name := range names {
		artists[i] = wireObject{"name": name}
	}
	return wireObject{"type": "track", "name": title, "uri": "spotify:track:" + id, "artists": artists, "album": wireObject{"name": "Album"}, "explicit": false, "duration_ms": 211000}
}

func alternateRelease(title, id, artist string) wireObject {
	song := spotifySong(title, id, artist)
	song["album"] = wireObject{"name": "Anniversary Collection"}
	song["duration_ms"] = 209000
	return song
}

func songPage(next string, songs ...wireObject) wireObject {
	items := make([]wireObject, len(songs))
	for i, song := range songs {
		items[i] = wireObject{"item": song}
	}
	var link any
	if next != "" {
		link = next
	}
	return wireObject{"items": items, "next": link}
}

func spotifyHits(items ...wireObject) wireObject {
	return wireObject{"tracks": wireObject{"items": items}}
}

func youtubeToSpotify(routes map[offlineRoute][]offlineReply) map[offlineRoute][]offlineReply {
	routes[offlineRoute{http.MethodPost, spotifyHost, "/v1/me/playlists", ""}] = []offlineReply{ok(wireObject{"id": "offlineSpotify", "uri": "spotify:playlist:offlineSpotify"})}
	routes[offlineRoute{http.MethodPost, spotifyHost, "/v1/playlists/offlineSpotify/items", ""}] = []offlineReply{{status: 201, body: wireObject{"snapshot_id": "fixtureSnapshot"}}}
	return routes
}

func withYouTubeWrites(writes int, routes map[offlineRoute][]offlineReply) map[offlineRoute][]offlineReply {
	routes[offlineRoute{http.MethodPost, youtubeHost, "/youtube/v3/playlists", ""}] = []offlineReply{ok(wireObject{"id": "offlineYouTube"})}
	items := make([]offlineReply, writes)
	for i := range items {
		items[i] = ok(wireObject{"id": "item"})
	}
	routes[offlineRoute{http.MethodPost, youtubeHost, "/youtube/v3/playlistItems", ""}] = items
	return routes
}
