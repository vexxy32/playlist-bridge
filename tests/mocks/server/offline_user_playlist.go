//go:build playlist_bridge_demo

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"playlist-bridge/internal/transfer"
)

var userPlaylistData = loadDemoFixture("offline_user_playlist.json")

var largeUserPlaylistData = loadDemoFixture("offline_user_playlist_320.json")

var holdoutUserPlaylistData = loadDemoFixture("offline_holdout_playlist_320.json")

func holdoutUserPlaylistFixture() offlineFixture {
	fixture := buildUserPlaylistFixture("holdout-youtube-spotify-320", holdoutUserPlaylistData, nil)
	fixture.label = "YouTube → Spotify · сложные названия и версии (320)"
	fixture.description = "Отдельный контрольный набор: разные языки, ремиксы, дубли релизов и недоступные записи. Часть случаев спорная."
	return fixture
}

type userPlaylistCandidate struct {
	ID      string
	Title   string
	Artists []string
}

type userPlaylistRow struct {
	Title         string
	Channel       string
	Expected      string
	Review        string
	Query         string
	FallbackQuery string
	First         []userPlaylistCandidate
	Fallback      []userPlaylistCandidate
}

const (
	userPlaylistUnavailable = "недоступен в источнике"
	userPlaylistMissed      = "не найден"
)

func userPlaylistFixture() offlineFixture {
	fixture := buildUserPlaylistFixture("user-youtube-spotify", userPlaylistData, nil)
	fixture.label = "YouTube → Spotify · лейблы и сложные случаи (60)"
	fixture.description = "Подборка с названиями реальных песен: лейблы вместо артистов, версии, опечатки и намеренно неоднозначные случаи. Ответы Spotify вымышлены."
	return fixture
}

func largeUserPlaylistFixture() offlineFixture {
	fixture := buildUserPlaylistFixture("user-youtube-spotify-320", largeUserPlaylistData, map[int]offlineReply{
		12: {
			status: http.StatusBadGateway,
			body:   wireObject{"error": wireObject{"status": 502, "message": "Bad Gateway"}},
		},
		35: {
			status: http.StatusTooManyRequests, retryAfter: "3",
			body: wireObject{"error": wireObject{"status": 429, "message": "API rate limit exceeded"}},
		},
	})
	fixture.label = "YouTube → Spotify · большая подборка и ошибки API (320)"
	fixture.description = "Большая размеченная подборка с лейблами и версиями; также моделируются временные ответы 502 и 429."
	return fixture
}

func buildUserPlaylistFixture(id string, data []byte, transientErrors map[int]offlineReply) offlineFixture {
	var rows []userPlaylistRow
	if err := json.Unmarshal(data, &rows); err != nil {
		panic(fmt.Errorf("не удалось прочитать офлайн-плейлист: %w", err))
	}

	videos := make([]wireObject, 0, len(rows))
	routes := make(map[offlineRoute][]offlineReply)
	for i, row := range rows {
		title := row.Title
		if row.Expected == userPlaylistUnavailable {
			// YouTube API возвращает пометки недоступных видео без скобок.
			title = strings.Trim(title, "[]")
		}
		videos = append(videos, sourceVideo(title, row.Channel, "user"+strconv.Itoa(i+1)))
		if row.Query == "" {
			continue
		}
		route := spotifySearch(row.Query)
		if _, exists := routes[route]; !exists {
			if fault, hasFault := transientErrors[i+1]; hasFault {
				routes[route] = []offlineReply{fault}
			}
			routes[route] = append(routes[route], userPlaylistSpotifyReply(row.First, false))
		}
	}
	// Одинаковые запросы Spotify используют общий ответ. Предпочитаем ответ первого поиска
	// необязательному повторному запросу для другого трека.
	for _, row := range rows {
		if row.FallbackQuery == "" {
			continue
		}
		route := spotifySearch(row.FallbackQuery)
		if _, exists := routes[route]; !exists {
			routes[route] = []offlineReply{userPlaylistSpotifyReply(row.Fallback, true)}
		}
	}

	var page string
	for start := 0; start < len(videos); start += 50 {
		end := min(start+50, len(videos))
		var next string
		if end < len(videos) {
			next = "userpage" + strconv.Itoa(end)
		}
		routes[youtubeRead(page)] = []offlineReply{ok(videoPage(next, videos[start:end]...))}
		page = next
	}

	youtubeToSpotify(routes)
	appendRoute := offlineRoute{http.MethodPost, spotifyHost, "/v1/playlists/offlineSpotify/items", ""}
	for remaining := len(rows) - 100; remaining > 0; remaining -= 100 {
		routes[appendRoute] = append(routes[appendRoute], offlineReply{
			status: http.StatusCreated, body: wireObject{"snapshot_id": "fixtureSnapshot"}, optional: true,
		})
	}
	return offlineFixture{
		id: id, label: fmt.Sprintf("YouTube → Spotify · присланный список (%d)", len(rows)),
		source: "youtube", destination: "spotify", routes: routes,
		wantStatus: 200, wantRetries: len(transientErrors), userPlaylist: rows,
	}
}

func userPlaylistSpotifyReply(candidates []userPlaylistCandidate, optional bool) offlineReply {
	items := make([]wireObject, 0, len(candidates))
	for _, candidate := range candidates {
		artists := make([]wireObject, 0, len(candidate.Artists))
		for _, name := range candidate.Artists {
			artists = append(artists, wireObject{"name": name})
		}
		items = append(items, wireObject{"name": candidate.Title, "uri": "spotify:track:" + candidate.ID, "artists": artists})
	}
	reply := ok(spotifyHits(items...))
	reply.optional = optional
	return reply
}

type offlineComparison struct {
	Agreed    int                    `json:"agreed"`
	Different int                    `json:"different"`
	Review    int                    `json:"review"`
	Details   []offlineComparisonRow `json:"details"`
}

type offlineComparisonRow struct {
	Position int    `json:"position"`
	Source   string `json:"source"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Review   string `json:"review,omitempty"`
}

func compareUserPlaylist(rows []userPlaylistRow, results []transfer.TransferResult) offlineComparison {
	var comparison offlineComparison
	for i, row := range rows {
		detail := offlineComparisonRow{
			Position: i + 1, Source: row.Title, Expected: row.expectedLabel(), Review: row.Review,
		}
		actual := "не проверен"
		if i < len(results) {
			result := results[i]
			switch {
			case result.Found:
				actual = strings.TrimPrefix(result.URI, "spotify:track:")
				detail.Actual = actual
				if result.Match != nil {
					detail.Actual = fmt.Sprintf("%s — %s (%s)", result.Match.Artist, result.Match.Title, actual)
				}
			case result.Unavailable:
				actual = userPlaylistUnavailable
			case !result.Pending:
				actual = userPlaylistMissed
			}
		}
		if detail.Actual == "" {
			detail.Actual = actual
		}

		switch {
		case row.Review != "":
			comparison.Review++
		case actual != row.Expected:
			comparison.Different++
		default:
			comparison.Agreed++
			continue
		}
		comparison.Details = append(comparison.Details, detail)
	}
	return comparison
}

func (row userPlaylistRow) expectedLabel() string {
	for _, candidates := range [][]userPlaylistCandidate{row.First, row.Fallback} {
		for _, candidate := range candidates {
			if candidate.ID == row.Expected {
				return fmt.Sprintf("%s — %s (%s)", strings.Join(candidate.Artists, ", "), candidate.Title, candidate.ID)
			}
		}
	}
	return row.Expected
}
