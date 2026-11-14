//go:build playlist_bridge_demo

package server

func showcaseYouTubeToSpotify() offlineFixture {
	return offlineFixture{
		id: "showcase-youtube-spotify", label: "YouTube → Spotify · лейбл, автор и кавер (4)",
		description: "Короткий сценарий: артист в названии, неверный исполнитель, кавер и недоступное видео.",
		source:      "youtube", destination: "spotify",
		routes: youtubeToSpotify(map[offlineRoute][]offlineReply{
			youtubeRead(""): {ok(videoPage("", sourceVideo("Rock That Body - The Black Eyed Peas (Lyrics)", "Pizza Music", "rockSource"), sourceVideo("Darin feat. Eloise - Falling In love (Music Video)", "Darin Music", "darinSource"), sourceVideo("Bella Kay - iloveitiloveitiloveit (Lyrics)", "VibesOnly", "bellaSource"), sourceVideo("Deleted video", "", "deleted")))},
			spotifySearch("Rock That Body - The Black Eyed Peas"): {ok(spotifyHits(spotifySong("Rock That Body", "tribute", "Cover Band"), spotifySong("Rock That Body", "rock", "Black Eyed Peas")))},
			spotifySearch("Darin - Falling In love"):              {ok(spotifyHits(spotifySong("Falling In Love", "solo", "Darin"), spotifySong("Falling In Love", "darin", "Darin", "Eloise")))},
			spotifySearch("Bella Kay - iloveitiloveitiloveit"):    {ok(spotifyHits(spotifySong("iloveitiloveitiloveit", "bella", "Bella Kay")))},
		}),
		notes: []string{
			"Название и исполнитель стоят в обратном порядке, а канал принадлежит загрузчику; в выдаче есть запись другого исполнителя.",
			"Имя канала отличается от исполнителя; важно сохранить гостя Eloise.",
			"Название содержит исполнителя, канал принадлежит загрузчику.",
			"Видео удалено: его нельзя искать как обычную песню.",
		},
		wantIDs:    []string{"spotify:track:rock", "spotify:track:darin", "spotify:track:bella"},
		wantStatus: 200, wantFound: 3, wantUnavailable: 1, wantRequests: 6,
	}
}

func showcaseSpotifyToYouTube() offlineFixture {
	return offlineFixture{
		id: "showcase-spotify-youtube", label: "Spotify → YouTube · клип, кавер и ремикс (4)",
		description: "Короткий сценарий: лейбл загрузил клип, кавер в выдаче, недоступный ремикс и повторный релиз.",
		source:      "spotify", destination: "youtube",
		routes: withYouTubeWrites(3, map[offlineRoute][]offlineReply{
			spotifyRead("0"):                              {ok(songPage("", spotifySong("In the End", "inTheEnd", "Linkin Park"), spotifySong("Bring Me to Life", "bring", "Evanescence"), spotifySong("Frozen (Club Remix)", "remix", "Artist"), alternateRelease("In the End", "inTheEndReissue", "Linkin Park")))},
			youtubeSearch("Linkin Park In the End"):       {ok(videoHits(searchVideo("In the End (Live)", "Linkin Park - Topic", "live"), searchVideo("Linkin Park - In the End [Official HD Music Video]", "Warner Records", "officialLinkin")))},
			youtubeSearch("Evanescence Bring Me to Life"): {ok(videoHits(searchVideo("Evanescence - Bring Me to Life (Cover)", "Cover Studio", "cover"), searchVideo("Bring Me to Life", "Evanescence - Topic", "evanescence")))},
			youtubeSearch("Artist Frozen (Club Remix)"):   {ok(videoHits(searchVideo("Frozen", "Artist - Topic", "studio")))},
		}),
		notes: []string{
			"Клип загружен лейблом; концертная версия не должна подменить оригинал.",
			"В выдаче сначала кавер, затем запись исполнителя.",
			"Ремикса в подготовленной выдаче нет, есть только студийный оригинал. Пропуск ожидается.",
			"Повторный релиз той же песни проверяет переиспользование результатов поиска.",
		},
		wantIDs: []string{"officialLinkin", "evanescence", "officialLinkin"}, wantStatus: 200, wantFound: 3, wantMissed: 1, wantRequests: 8,
	}
}
