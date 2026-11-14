//go:build playlist_bridge_demo

package server

func fullOfflineFixtures() []offlineFixture {
	return []offlineFixture{showcaseYouTubeToSpotify(), showcaseSpotifyToYouTube(), userPlaylistFixture(), largeUserPlaylistFixture(), holdoutUserPlaylistFixture(), everydayPlaylistFixture(), scaleYouTubeToSpotifyFixture(), scaleSpotifyToYouTubeFixture()}
}
