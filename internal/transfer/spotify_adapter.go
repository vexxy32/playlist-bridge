package transfer

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"playlist-bridge/internal/spotify"
)

// SearchSpotify создаёт поиск для одного переноса и переиспользует ответы на одинаковые запросы.
func SearchSpotify(token string) SearchFunc {
	search, _ := NewSpotifySearch(token, SpotifySearchStandard)
	return search
}

type SpotifySearchMode string

const (
	SpotifySearchStandard        SpotifySearchMode = "standard"
	SpotifySearchBroadFirst      SpotifySearchMode = "broad-first"
	SpotifySearchReuseCandidates SpotifySearchMode = "reuse-candidates"
)

// SpotifySearchStats считает попытки обращения к поиску и результаты по уникальным входным записям.
type SpotifySearchStats struct {
	FirstRequests    int
	FallbackRequests int
	SharedQueries    int
	FirstMatches     int
	FallbackMatches  int
	ReusedMatches    int
}

// NewSpotifySearch создаёт поиск и функцию получения его счётчиков для одного прогона.
func NewSpotifySearch(token string, mode SpotifySearchMode) (SearchFunc, func() SpotifySearchStats) {
	var mu sync.Mutex
	var stats SpotifySearchStats
	queries := make(map[string]func() ([]Candidate, error))
	// Выполняем поиск Spotify по одному запросу, чтобы очередь учитывала Retry-After.
	requestSlot := make(chan struct{}, 1)
	var rateLimit *spotify.RateLimitError // Guarded by requestSlot.
	request := func(ctx context.Context, query string, fallback bool) ([]spotify.Candidate, error) {
		const maxRetryAfter = 5
		const maxRetries = 2
		select {
		case requestSlot <- struct{}{}:
			defer func() { <-requestSlot }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if rateLimit != nil {
			return nil, rateLimit
		}
		for attempt := 0; ; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			mu.Lock()
			if fallback {
				stats.FallbackRequests++
			} else {
				stats.FirstRequests++
			}
			mu.Unlock()

			found, err := spotify.SearchTrackQuery(ctx, token, query)
			var limited *spotify.RateLimitError
			var delay time.Duration
			if errors.As(err, &limited) {
				seconds := limited.RetryAfterSeconds
				if limited.Reason == "QUOTA_EXCEEDED" || seconds <= 0 || seconds > maxRetryAfter || attempt >= maxRetries {
					rateLimit = limited
					return nil, err
				}
				delay = time.Duration(seconds) * time.Second
			} else {
				var status *spotify.HTTPStatusError
				if attempt >= maxRetries || !errors.As(err, &status) ||
					(status.StatusCode != http.StatusBadGateway &&
						status.StatusCode != http.StatusServiceUnavailable &&
						status.StatusCode != http.StatusGatewayTimeout) {
					return found, err
				}
				delay = time.Duration(attempt+1) * 250 * time.Millisecond
			}
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
		}
	}
	var knownCandidates map[Track]Candidate
	if mode == SpotifySearchReuseCandidates {
		knownCandidates = make(map[Track]Candidate)
	}
	searchQuery := func(ctx context.Context, query string, fallback bool) ([]Candidate, error) {
		mu.Lock()
		search, ok := queries[query]
		if ok {
			stats.SharedQueries++
		} else {
			search = sync.OnceValues(func() ([]Candidate, error) {
				found, err := request(ctx, query, fallback)
				if err != nil {
					return nil, err
				}
				candidates := spotifyCandidates(found)
				if knownCandidates != nil {
					mu.Lock()
					indexAlbumCandidates(knownCandidates, candidates)
					mu.Unlock()
				}
				return candidates, nil
			})
			queries[query] = search
		}
		mu.Unlock()
		return search()
	}

	search := func(ctx context.Context, track Track) ([]Candidate, error) {
		parsedTitle := parseTitle(track.Title)
		title := parsedTitle.base
		for i := len(parsedTitle.versions) - 1; i >= 0; i-- {
			title += " " + parsedTitle.versions[i]
		}
		primary := spotifyPrimaryArtist(track.Artist)
		isrc := normalizeISRC(track.ISRC)
		if isrc == "" && (title == "" || (primary == "" && track.videoSearchTitle == "")) {
			return nil, nil
		}
		if knownCandidates != nil && matchText(track.Album) != "" {
			key := albumCandidateKey(track.Title, track.Artist, track.Album)
			mu.Lock()
			candidate := knownCandidates[key]
			mu.Unlock()
			if candidate.URI != "" && (isrc == "" || isrc == normalizeISRC(candidate.ISRC)) {
				candidates := []Candidate{candidate}
				if _, matched := selectCandidate(track, candidates); matched {
					mu.Lock()
					stats.ReusedMatches++
					mu.Unlock()
					return candidates, nil
				}
			}
		}

		firstQuery := spotify.TrackSearchQuery(typography.Replace(title), typography.Replace(primary))
		fallbackQuery := typography.Replace(title + " " + primary)
		if track.videoSearchTitle != "" {
			if rawTitle := parseTitle(track.videoSearchTitle).base; rawTitle != "" {
				fallbackQuery = typography.Replace(rawTitle)
			}
		}
		// В названии ролика исполнитель может стоять с любой стороны.
		// Сначала ищем обе части вместе, не угадывая их порядок.
		if mode == SpotifySearchBroadFirst || track.videoSearchTitle != "" {
			firstQuery, fallbackQuery = fallbackQuery, firstQuery
		}
		if primary == "" || track.videoArtistAlias != "" {
			firstQuery, fallbackQuery = typography.Replace(title), ""
		}
		if isrc != "" {
			fallbackQuery, firstQuery = firstQuery, "isrc:"+isrc
			if title == "" {
				fallbackQuery = ""
			}
		}

		candidates, err := searchQuery(ctx, firstQuery, false)
		if err != nil {
			return nil, err
		}
		if _, matched := selectCandidate(track, candidates); matched {
			mu.Lock()
			stats.FirstMatches++
			mu.Unlock()
			return candidates, nil
		}
		if fallbackQuery == "" || fallbackQuery == firstQuery {
			return candidates, nil
		}
		// Для повторного текстового поиска нужны признаки нужного исполнителя.
		// После неудачного поиска по ISRC и в режиме broad-first текстовый поиск сохраняется.
		if isrc == "" && mode != SpotifySearchBroadFirst &&
			!slices.ContainsFunc(candidates, func(candidate Candidate) bool {
				return artistIndex(primary, candidate.Artists) >= 0
			}) {
			return candidates, nil
		}
		found, err := searchQuery(ctx, fallbackQuery, true)
		if err != nil {
			return nil, err
		}
		if _, matched := selectCandidate(track, found); matched {
			mu.Lock()
			stats.FallbackMatches++
			mu.Unlock()
		}
		return slices.Concat(candidates, found), nil
	}
	return search, func() SpotifySearchStats {
		mu.Lock()
		defer mu.Unlock()
		return stats
	}
}

func spotifyPrimaryArtist(artist string) string {
	artist = bracketedFeature.ReplaceAllString(cleanSpacing(artist), "")
	if feature := featureMarker.FindStringIndex(artist); feature != nil {
		artist = artist[:feature[0]]
	}
	// Исполнители Spotify разделены запятыми; символы & и and в названиях групп сохраняем.
	primary, _, _ := strings.Cut(artist, ",")
	return parseTitle(primary).base
}

func albumCandidateKey(title, artist, album string) Track {
	return Track{Title: matchText(title), Artist: matchText(artist), Album: matchText(album)}
}

// indexAlbumCandidates хранит точные совпадения из ответов этого прогона.
// Пустой URI отмечает совпадение с несколькими версиями.
func indexAlbumCandidates(index map[Track]Candidate, candidates []Candidate) {
	for _, candidate := range candidates {
		if candidate.URI == "" || len(candidate.Artists) != 1 || matchText(candidate.Album) == "" {
			continue
		}
		key := albumCandidateKey(candidate.Title, candidate.Artists[0], candidate.Album)
		previous, exists := index[key]
		if exists && previous.URI != candidate.URI {
			index[key] = Candidate{}
			continue
		}
		index[key] = candidate
	}
}

func spotifyCandidates(found []spotify.Candidate) []Candidate {
	candidates := make([]Candidate, 0, len(found))
	for _, item := range found {
		candidates = append(candidates, Candidate{
			Title:      item.Title,
			Artists:    item.Artists,
			Album:      item.Album,
			URI:        item.URI,
			Explicit:   item.Explicit,
			DurationMs: item.DurationMs,
			ISRC:       item.ISRC,
		})
	}
	return candidates
}

func AddToSpotify(token string) AddFunc {
	return func(ctx context.Context, playlistID string, uris []string) error {
		return spotify.AddTrack(ctx, token, playlistID, uris)
	}
}

func CreateSpotify(token string) CreateFunc {
	return func(ctx context.Context, name string) (Playlist, error) {
		playlist, err := spotify.CreatePlaylist(ctx, token, name, "", false)
		if err != nil {
			return Playlist{}, err
		}
		return Playlist{
			ID:  playlist.ID,
			URI: playlist.URI,
			URL: playlist.ExternalURLs.Spotify,
		}, nil
	}
}

// TracksFromSpotify преобразует записи Spotify в общий формат переноса.
func TracksFromSpotify(songs []spotify.Song) []Track {
	tracks := make([]Track, 0, len(songs))
	for _, song := range songs {
		artists := make([]string, 0, len(song.Artists))
		for _, artist := range song.Artists {
			if strings.TrimSpace(artist.Name) != "" {
				artists = append(artists, artist.Name)
			}
		}
		if song.Title == "" || len(artists) == 0 {
			continue
		}
		tracks = append(tracks, Track{
			Title:      song.Title,
			Artist:     strings.Join(artists, ", "),
			Album:      song.Album.Name,
			DurationMs: song.DurationMs,
			ISRC:       song.ISRC,
		})
	}
	return tracks
}
