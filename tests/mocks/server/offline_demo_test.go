//go:build playlist_bridge_demo

package server

import (
	"context"
	"strings"
	"testing"
)

func TestDemoFixturesRunWithoutNetwork(t *testing.T) {
	for _, fixture := range fullOfflineFixtures() {
		t.Run(fixture.id, func(t *testing.T) {
			report := executeOfflineTest(context.Background(), fixture, 0, nil)
			t.Logf("совпало ожидание: %v; проблем: %d", report.Passed, len(report.Problems))
			if !report.Passed || report.Status != fixture.wantStatus || len(report.Results) == 0 {
				t.Fatalf("демо не завершилось: статус %d, результаты %d, ошибки %v", report.Status, len(report.Results), report.Problems)
			}
		})
	}
}

func TestDemoExplainsExpectedMissingAndFallback(t *testing.T) {
	fixture := everydayPlaylistFixture()
	report := executeOfflineTest(context.Background(), fixture, 0, nil)
	count := 0
	for i, result := range report.Results {
		if result.Found || result.Unavailable || result.Pending {
			continue
		}
		count++
		detail := report.Diagnostics[i]
		if !strings.Contains(detail.Expected, "пропуск ожидается") || detail.Explanation == "" || len(detail.Searches) == 0 {
			t.Errorf("нет объяснения пропуска %s: %+v", result.Track.Title, detail)
		}
	}
	if count != 8 {
		t.Fatalf("ожидалось 8 пропусков, получено %d", count)
	}
	original := userPlaylistFixture()
	originalReport := executeOfflineTest(context.Background(), original, 0, nil)
	foundFallbackCase := false
	for i, result := range originalReport.Results {
		if result.Track.Title == "Don't Start Now" && !result.Found {
			foundFallbackCase = strings.Contains(originalReport.Diagnostics[i].Explanation, "дополнительного запроса")
		}
	}
	if !foundFallbackCase {
		t.Fatal("не объяснён пропуск с вариантом в дополнительной выдаче")
	}
}
