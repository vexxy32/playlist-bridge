//go:build playlist_bridge_demo

package server

import (
	"fmt"
	"strings"

	"playlist-bridge/internal/transfer"
)

type offlineDiagnostic struct {
	Source      string                    `json:"source,omitempty"`
	Expected    string                    `json:"expected,omitempty"`
	Explanation string                    `json:"explanation,omitempty"`
	Searches    []offlineDiagnosticSearch `json:"searches,omitempty"`
}

type offlineDiagnosticSearch struct {
	Query  string   `json:"query"`
	Status int      `json:"status"`
	Hits   []string `json:"hits"`
}

func buildOfflineDiagnostics(fixture offlineFixture, results []transfer.TransferResult, calls []offlineCall) []offlineDiagnostic {
	details := make([]offlineDiagnostic, len(results))
	for i, result := range results {
		if i < len(fixture.notes) {
			details[i].Explanation = fixture.notes[i]
		}
		if i >= len(fixture.userPlaylist) {
			continue
		}
		row := fixture.userPlaylist[i]
		detail := &details[i]
		detail.Source = fmt.Sprintf("Видео: %s; канал: %s", row.Title, row.Channel)
		if row.Channel == "" {
			detail.Source = "Видео: " + row.Title + "; канал не указан"
		}
		switch {
		case row.Review != "":
			detail.Expected = "По разметке требуется проверка: " + row.Review
		case row.Expected == userPlaylistUnavailable:
			detail.Expected = "Видео недоступно в источнике."
		case row.Expected == userPlaylistMissed:
			detail.Expected = "По условиям сценария подходящего трека нет; пропуск ожидается."
		default:
			detail.Expected = "Ожидаемый результат: " + row.expectedLabel()
		}
		for _, call := range calls {
			if call.Service != "Spotify" || call.Operation != "Поиск" || call.Query == "" {
				continue
			}
			if call.Query == row.Query || (row.FallbackQuery != "" && call.Query == row.FallbackQuery) {
				detail.Searches = append(detail.Searches, offlineDiagnosticSearch{Query: call.Query, Status: call.Status, Hits: call.Hits})
			}
		}
		if result.Found || result.Unavailable || result.Pending {
			continue
		}
		if row.Expected == userPlaylistMissed {
			if len(row.First) == 0 && len(row.Fallback) == 0 {
				detail.Explanation = "В подготовленных ответах поиска нет подходящих записей."
			} else {
				detail.Explanation = "В подготовленных ответах есть только неподходящие варианты; трек может быть пропущен намеренно."
			}
		} else if row.Review != "" {
			detail.Explanation = "Результат неоднозначен; объяснение указано в разметке сценария."
		} else if row.Expected != "" {
			inFirst, inFallback := false, false
			for _, c := range row.First {
				if c.ID == row.Expected {
					inFirst = true
				}
			}
			for _, c := range row.Fallback {
				if c.ID == row.Expected {
					inFallback = true
				}
			}
			firstCalled, fallbackCalled := false, false
			for _, search := range detail.Searches {
				if search.Status != 200 {
					continue
				}
				if search.Query == row.Query {
					firstCalled = true
				}
				if row.FallbackQuery != "" && search.Query == row.FallbackQuery {
					fallbackCalled = true
				}
			}
			switch {
			case inFirst && firstCalled, inFallback && fallbackCalled:
				detail.Explanation = "Ожидаемый вариант был в полученной выдаче, но алгоритм его не выбрал. Точная причина отказа в отчёте не фиксируется."
			case inFallback && !fallbackCalled:
				detail.Explanation = "Ожидаемый вариант задан для дополнительного запроса, но этот запрос в текущем прогоне не выполнен."
			default:
				detail.Explanation = "Посмотрите ответы выполненных поисков ниже: точная причина отказа в отчёте не фиксируется."
			}
		}
		if len(detail.Searches) == 0 && strings.TrimSpace(row.Query) != "" {
			detail.Explanation += " Запросы этой строки не совпали с сохранёнными запросами сценария либо результат был переиспользован."
		}
	}
	return details
}
