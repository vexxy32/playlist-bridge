package server

func fullOfflineFixtures() []offlineFixture {
	return []offlineFixture{userPlaylistFixture(), largeUserPlaylistFixture(), holdoutUserPlaylistFixture(), scaleYouTubeToSpotifyFixture(), scaleSpotifyToYouTubeFixture()}
}
