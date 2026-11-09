package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Track — входной трек из исходного JSON.
type Track struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// TransferResult — результат поиска для одного трека.
type TransferResult struct {
	Track Track  `json:"track"`
	Found bool   `json:"found"`
	URI   string `json:"uri,omitempty"`
}

// Candidate — данные найденного трека в выбранном сервисе.
type Candidate struct {
	Title   string
	Artists []string
	Album   string
	URI     string
}
type Playlist struct {
	ID  string `json:"id"`
	URI string `json:"uri"`
	URL string `json:"url"`
}

// SearchFunc ищет варианты трека. Функция должна допускать параллельные вызовы.
type SearchFunc func(ctx context.Context, track Track) ([]Candidate, error)

// ParseTracks читает входной JSON и превращает его в []Track.
func ParseTracks(data []byte) ([]Track, error) {
	var tracks []Track
	if err := json.Unmarshal(data, &tracks); err != nil {
		return nil, fmt.Errorf("повреждён JSON: %w", err)
	}

	for i, t := range tracks {
		if strings.TrimSpace(t.Title) == "" {
			return nil, fmt.Errorf("запись #%d: отсутствует обязательное поле title", i+1)
		}
		if strings.TrimSpace(t.Artist) == "" {
			return nil, fmt.Errorf("запись #%d (%q): отсутствует обязательное поле artist", i+1, t.Title)
		}
	}

	return tracks, nil
}

var remasterSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—]\s*|[\(\[]\s*)(?:\d{4}\s+)?remaster(?:ed)?(?:\s+\d{4})?\s*[\)\]]?\s*$`)

// selectCandidate допускает разные издания, но не подменяет название или исполнителя.
func selectCandidate(track Track, candidates []Candidate) (Candidate, bool) {
	wantTitle := strings.TrimSpace(track.Title)
	explicitRemaster := remasterSuffix.MatchString(wantTitle)
	wantArtist := strings.TrimSpace(track.Artist)
	wantAlbum := strings.TrimSpace(track.Album)
	if wantTitle == "" || wantArtist == "" {
		return Candidate{}, false
	}
	bestScore := -1
	var best Candidate
	for _, c := range candidates {
		if c.URI == "" {
			continue
		}
		candidateTitle := strings.TrimSpace(c.Title)
		exactTitle := strings.EqualFold(candidateTitle, wantTitle)
		if !exactTitle {
			baseTitle := strings.TrimSpace(remasterSuffix.ReplaceAllString(candidateTitle, ""))
			if explicitRemaster || !strings.EqualFold(baseTitle, wantTitle) {
				continue
			}
		}
		artistIndex := -1
		for i, a := range c.Artists {
			if strings.EqualFold(strings.TrimSpace(a), wantArtist) {
				artistIndex = i
				break
			}
		}
		if artistIndex < 0 {
			continue
		}
		score := 0
		if exactTitle {
			score += 8
		}
		if artistIndex == 0 {
			score++
		}
		if len(c.Artists) == 1 {
			score += 2
		}
		if wantAlbum != "" && strings.EqualFold(strings.TrimSpace(c.Album), wantAlbum) {
			score += 4
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best, bestScore >= 0
}

// TransferTracks ищет треки выбранной функцией поиска и сохраняет их порядок.
func TransferTracks(ctx context.Context, tracks []Track, search SearchFunc) ([]TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if search == nil {
		return nil, fmt.Errorf("функция поиска не задана")
	}
	type searchResult struct {
		candidates []Candidate
		err        error
	}
	// Один запрос для повторяющихся входных записей; результаты сохраняют исходный порядок.
	unique := make([]Track, 0, len(tracks))
	lookup := make(map[Track]int, len(tracks))
	indices := make([]int, len(tracks))
	for i, track := range tracks {
		key := Track{Title: strings.ToLower(strings.TrimSpace(track.Title)), Artist: strings.ToLower(strings.TrimSpace(track.Artist)), Album: strings.ToLower(strings.TrimSpace(track.Album))}
		index, ok := lookup[key]
		if !ok {
			index = len(unique)
			lookup[key] = index
			unique = append(unique, track)
		}
		indices[i] = index
	}
	responses := make([]searchResult, len(unique))
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
				responses[i].candidates, responses[i].err = search(ctx, unique[i])
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]TransferResult, len(tracks))
	for i, track := range tracks {
		response := responses[indices[i]]
		if response.err != nil {
			return nil, fmt.Errorf("ошибка поиска трека %q — %q: %w", track.Artist, track.Title, response.err)
		}
		chosen, found := selectCandidate(track, response.candidates)
		results[i] = TransferResult{Track: track, Found: found}
		if found {
			results[i].URI = chosen.URI
		}
	}
	return results, nil
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
func CreateTransfer(ctx context.Context, tracks []Track, name string, search SearchFunc, create CreateFunc, add AddFunc) (Playlist, []TransferResult, error) {
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
		return Playlist{}, nil, err
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
