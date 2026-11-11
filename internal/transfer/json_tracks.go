package transfer

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

// ExportTracksJSON возвращает список в формате, который можно загрузить обратно.
func ExportTracksJSON(tracks []Track) ([]byte, error) {
	if tracks == nil {
		tracks = []Track{}
	}
	data, err := json.MarshalIndent(tracks, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("создать JSON: %w", err)
	}
	return append(data, '\n'), nil
}
