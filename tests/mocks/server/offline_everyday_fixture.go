//go:build playlist_bridge_demo

package server

var everydayPlaylistData = loadDemoFixture("offline_everyday_playlist.json")

func everydayPlaylistFixture() offlineFixture {
	fixture := buildUserPlaylistFixture("everyday-youtube-spotify", everydayPlaylistData, nil)
	fixture.label = "YouTube → Spotify · обычные и отсутствующие записи (52)"
	fixture.description = "Вымышленные исполнители и треки: среди ответов есть концертные версии, другие исполнители и восемь намеренно отсутствующих записей."
	return fixture
}
