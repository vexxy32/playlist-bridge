package transfer

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Track — входной трек из исходного JSON.
type Track struct {
	Title            string `json:"title"`
	Artist           string `json:"artist"`
	Album            string `json:"album"`
	DurationMs       int    `json:"-"`
	ISRC             string `json:"-"`
	videoSearchTitle string
	videoArtistAlias string
	fromYouTube      bool
	unavailable      bool
}

// TransferResult — результат поиска для одного трека.
type TransferResult struct {
	Track       Track  `json:"track"`
	Found       bool   `json:"found"`
	URI         string `json:"uri,omitempty"`
	Match       *Track `json:"match,omitempty"`
	Pending     bool   `json:"pending,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

// Candidate — данные найденного трека в выбранном сервисе.
type Candidate struct {
	Title      string
	Artists    []string
	Album      string
	URI        string
	Explicit   *bool
	DurationMs int    `json:"-"`
	ISRC       string `json:"-"`
}

type Playlist struct {
	ID  string `json:"id"`
	URI string `json:"uri"`
	URL string `json:"url"`
}

// SearchFunc ищет варианты трека. Функция должна допускать параллельные вызовы.
type SearchFunc func(ctx context.Context, track Track) ([]Candidate, error)

// TransferTracks ищет треки выбранной функцией поиска и сохраняет их порядок.
func TransferTracks(ctx context.Context, tracks []Track, search SearchFunc) ([]TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if search == nil {
		return nil, fmt.Errorf("функция поиска не задана")
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	// Один запрос для повторяющихся входных записей; результаты сохраняют исходный порядок.
	unique := make([]Track, 0, len(tracks))
	lookup := make(map[Track]int, len(tracks))
	indices := make([]int, len(tracks))
	for i, track := range tracks {
		key := Track{
			Title:            matchText(track.Title),
			Artist:           matchText(track.Artist),
			Album:            matchText(track.Album),
			DurationMs:       track.DurationMs,
			ISRC:             normalizeISRC(track.ISRC),
			videoSearchTitle: track.videoSearchTitle,
			videoArtistAlias: track.videoArtistAlias,
			fromYouTube:      track.fromYouTube,
			unavailable:      track.unavailable,
		}
		index, ok := lookup[key]
		if !ok {
			index = len(unique)
			lookup[key] = index
			unique = append(unique, track)
		}
		indices[i] = index
	}
	responses := make([][]Candidate, len(unique))
	searched := make([]bool, len(unique))
	jobs := make(chan int, len(unique))
	for i := range unique {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(3, len(unique)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				track := unique[i]
				if track.unavailable {
					continue
				}
				candidates, err := search(ctx, track)
				if err != nil {
					cancel(fmt.Errorf("ошибка поиска трека %q — %q: %w", track.Artist, track.Title, err))
					return
				}
				responses[i] = candidates
				searched[i] = true
			}
		}()
	}
	wg.Wait()

	results := make([]TransferResult, len(tracks))
	for i, track := range tracks {
		results[i] = TransferResult{Track: track, Unavailable: track.unavailable}
		if track.unavailable {
			continue
		}
		index := indices[i]
		if !searched[index] {
			results[i].Pending = true
			continue
		}
		chosen, found := selectCandidate(track, responses[index])
		results[i].Found = found
		if found {
			results[i].URI = chosen.URI
			results[i].Match = &Track{Title: chosen.Title, Artist: strings.Join(chosen.Artists, ", "), Album: chosen.Album}
		}
	}
	return results, context.Cause(ctx)
}

func collectFoundURIs(results []TransferResult) []string {
	uris := make([]string, 0, len(results))

	for _, r := range results {
		if r.Found && r.URI != "" {
			uris = append(uris, r.URI)
		}
	}

	return uris
}

type AddFunc func(ctx context.Context, playlistID string, uris []string) error
type CreateFunc func(ctx context.Context, name string) (Playlist, error)

// CreateTransfer ищет треки, создаёт плейлист и добавляет найденные записи.
// При ошибке добавления уже созданный плейлист остаётся в результате.
func CreateTransfer(
	ctx context.Context,
	tracks []Track,
	name string,
	search SearchFunc,
	create CreateFunc,
	add AddFunc,
) (Playlist, []TransferResult, error) {
	if strings.TrimSpace(name) == "" {
		return Playlist{}, nil, fmt.Errorf("укажи название плейлиста")
	}
	if len(tracks) == 0 {
		return Playlist{}, nil, fmt.Errorf("список треков пуст")
	}
	if search == nil || create == nil || add == nil {
		return Playlist{}, nil, fmt.Errorf("сервис переноса не настроен")
	}
	results, err := TransferTracks(ctx, tracks, search)
	if err != nil {
		return Playlist{}, results, err
	}
	uris := collectFoundURIs(results)
	if len(uris) == 0 {
		return Playlist{}, results, fmt.Errorf("подходящие треки не найдены")
	}
	if err := ctx.Err(); err != nil {
		return Playlist{}, results, err
	}
	playlist, err := create(ctx, strings.TrimSpace(name))
	if err != nil {
		return Playlist{}, results, fmt.Errorf("создать плейлист: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return playlist, results, err
	}
	if err := add(ctx, playlist.ID, uris); err != nil {
		return playlist, results, fmt.Errorf("плейлист создан, но добавить треки не удалось: %w", err)
	}
	return playlist, results, nil
}
