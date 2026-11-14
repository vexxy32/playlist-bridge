package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"playlist-bridge/internal/providerhttp"
)

const offlineSpotifyID = "fixture000000000000001"

type offlineReply struct {
	status        int
	body          any
	retryAfter    string
	wantWriteSize int
	optional      bool
}

type offlineRoute struct {
	method, host, path string
	query              string
}

type offlineFixture struct {
	id                                                  string
	label                                               string
	source                                              string
	destination                                         string
	routes                                              map[offlineRoute][]offlineReply
	wantIDs                                             []string
	wantFoundIDs                                        []string
	wantStatus                                          int
	wantFound, wantMissed, wantUnavailable, wantPending int
	wantRequests                                        int
	wantFallback                                        int
	wantRetries                                         int
	wantRetryAfter                                      string
	fromYouTubeAccount                                  bool
	userPlaylist                                        []userPlaylistRow
}

type offlineCall struct {
	Service   string   `json:"service"`
	Operation string   `json:"operation"`
	Query     string   `json:"query,omitempty"`
	Hits      []string `json:"hits"`
	Status    int      `json:"status"`
}

type offlineRun struct {
	mu       sync.Mutex
	routes   map[offlineRoute][]offlineReply
	calls    []offlineCall
	written  []string
	problems []string
	retries  int
	attempts map[offlineRoute]int
	delay    time.Duration
	onCall   func(int, offlineCall)
}

func (run *offlineRun) record(call offlineCall) {
	run.calls = append(run.calls, call)
	if run.onCall != nil {
		run.onCall(len(run.calls), call)
	}
}

func (run *offlineRun) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if run.delay > 0 {
		timer := time.NewTimer(run.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	run.mu.Lock()
	defer run.mu.Unlock()

	key := offlineRoute{method: req.Method, host: req.URL.Host, path: req.URL.Path}
	values := req.URL.Query()
	service := key.host
	if key.host == spotifyHost {
		service = "Spotify"
	}
	if key.host == youtubeHost {
		service = "YouTube"
	}
	operation := "Чтение"
	var written []string
	fail := func(err error) (*http.Response, error) {
		run.problems = append(run.problems, err.Error())
		run.record(offlineCall{Service: service, Operation: operation, Query: key.query})
		return nil, err
	}
	if values.Has("q") {
		key.query = values.Get("q")
		operation = "Поиск"
	} else if values.Has("offset") {
		key.query = values.Get("offset")
	} else if strings.HasSuffix(key.path, "/playlistItems") && key.method == http.MethodGet {
		key.query = values.Get("pageToken")
	}
	if key.method == http.MethodPost {
		operation = "Создание"
		var body struct {
			Name    string
			URIs    []string
			Snippet struct {
				Title      string
				PlaylistID string
				ResourceID struct{ VideoID string }
			}
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return fail(fmt.Errorf("некорректный запрос записи: %w", err))
		}
		if strings.HasSuffix(key.path, "/items") || strings.HasSuffix(key.path, "/playlistItems") {
			operation = "Добавление"
			written = body.URIs
			if service == "YouTube" {
				if body.Snippet.PlaylistID != "offlineYouTube" || body.Snippet.ResourceID.VideoID == "" {
					return fail(fmt.Errorf("неверный запрос вставки YouTube"))
				}
				written = []string{body.Snippet.ResourceID.VideoID}
			} else if len(written) == 0 || len(written) > 100 {
				return fail(fmt.Errorf("неверный размер пакета Spotify"))
			}
		} else if body.Name != "Офлайн-тест" && body.Snippet.Title != "Офлайн-тест" {
			return fail(fmt.Errorf("неверное имя тестового плейлиста"))
		}
	}
	replies := run.routes[key]
	if len(replies) == 0 {
		return fail(fmt.Errorf("неожиданный офлайн-запрос %s %s%s (%s)", key.method, key.host, key.path, key.query))
	}
	reply := replies[0]
	if reply.wantWriteSize > 0 && len(written) != reply.wantWriteSize {
		return fail(fmt.Errorf("размер пакета %d, ожидалось %d", len(written), reply.wantWriteSize))
	}
	run.routes[key] = replies[1:]
	if operation == "Поиск" && run.attempts[key] > 0 {
		run.retries++
	}
	run.attempts[key]++
	call := offlineCall{Service: service, Operation: operation, Query: key.query, Status: reply.status}
	if operation == "Поиск" && reply.status == http.StatusOK {
		call.Hits = searchFixtureLabels(key.host, reply.body)
	}
	run.record(call)
	if reply.status >= 200 && reply.status < 300 {
		run.written = append(run.written, written...)
	}
	body, err := json.Marshal(reply.body)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	if reply.retryAfter != "" {
		header.Set("Retry-After", reply.retryAfter)
	}
	return &http.Response{
		StatusCode: reply.status,
		Status:     strconv.Itoa(reply.status) + " " + http.StatusText(reply.status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    req,
	}, nil
}

func searchFixtureLabels(host string, body any) []string {
	object, _ := body.(wireObject)
	labels := []string{}
	if host == spotifyHost {
		tracks, _ := object["tracks"].(wireObject)
		items, _ := tracks["items"].([]wireObject)
		for _, item := range items {
			title, _ := item["name"].(string)
			artists, _ := item["artists"].([]wireObject)
			names := make([]string, 0, len(artists))
			for _, artist := range artists {
				name, _ := artist["name"].(string)
				names = append(names, name)
			}
			labels = append(labels, strings.Join(names, ", ")+" — "+title)
		}
	} else {
		items, _ := object["items"].([]wireObject)
		for _, item := range items {
			snippet, _ := item["snippet"].(wireObject)
			title, _ := snippet["title"].(string)
			channel, _ := snippet["channelTitle"].(string)
			labels = append(labels, channel+" — "+title)
		}
	}
	return labels
}

type offlineReport struct {
	transferResponse
	Destination string             `json:"destination"`
	Status      int                `json:"status"`
	Passed      bool               `json:"passed"`
	Calls       []offlineCall      `json:"calls"`
	Retries     int                `json:"retries"`
	RetryAfter  string             `json:"retryAfter,omitempty"`
	ElapsedMs   int64              `json:"elapsedMs"`
	Written     int                `json:"written"`
	Problems    []string           `json:"problems,omitempty"`
	Comparison  *offlineComparison `json:"comparison,omitempty"`
}

func executeOfflineTest(ctx context.Context, fixture offlineFixture, delay time.Duration, onCall func(int, offlineCall)) offlineReport {
	started := time.Now()
	run := &offlineRun{
		routes: fixture.routes, calls: []offlineCall{}, attempts: make(map[offlineRoute]int),
		delay: delay, onCall: onCall,
	}
	demo := New("", "")
	demo.SetYouTubeAPIKey("offline")
	expires := time.Now().Add(time.Hour)
	demo.sessions["spotify"] = session{accessToken: "offline", expiresAt: expires}
	demo.sessions["youtube"] = session{google: true, accessToken: "offline", expiresAt: expires}

	input := transferRequest{Name: "Офлайн-тест", Destination: fixture.destination}
	if fixture.source == "spotify" {
		input.SpotifyURL = "https://open.spotify.com/playlist/" + offlineSpotifyID
	} else {
		input.YouTubeURL = "https://www.youtube.com/playlist?list=PLfixture"
		input.YouTubeFromAccount = fixture.fromYouTubeAccount
	}
	payload, _ := json.Marshal(input)
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(string(payload)))
	req = req.WithContext(providerhttp.WithTransport(ctx, run))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "spotify"})
	req.AddCookie(&http.Cookie{Name: "google_session", Value: "youtube"})
	recorder := httptest.NewRecorder()
	demo.transfer(recorder, req)

	report := offlineReport{ElapsedMs: time.Since(started).Milliseconds(), RetryAfter: recorder.Header().Get("Retry-After"), Destination: fixture.destination, Status: recorder.Code, Calls: run.calls, Retries: run.retries, Written: len(run.written), Problems: run.problems}
	if err := json.Unmarshal(recorder.Body.Bytes(), &report.transferResponse); err != nil {
		report.Problems = append(report.Problems, "не удалось прочитать отчёт: "+err.Error())
	}
	if report.Status != fixture.wantStatus {
		report.Problems = append(report.Problems, fmt.Sprintf("HTTP %d, ожидался %d", report.Status, fixture.wantStatus))
	}
	if fixture.userPlaylist == nil && !reflect.DeepEqual(run.written, fixture.wantIDs) {
		report.Problems = append(report.Problems, fmt.Sprintf("записано %v, ожидалось %v", run.written, fixture.wantIDs))
	}
	var foundIDs []string
	var missed, unavailable, pending, searches int
	for _, result := range report.Results {
		switch {
		case result.Found:
			foundIDs = append(foundIDs, result.URI)
		case result.Unavailable:
			unavailable++
		case result.Pending:
			pending++
		default:
			missed++
		}
	}
	for _, call := range run.calls {
		if call.Operation == "Поиск" {
			searches++
		}
	}
	if got, want := [4]int{len(foundIDs), missed, unavailable, pending}, [4]int{fixture.wantFound, fixture.wantMissed, fixture.wantUnavailable, fixture.wantPending}; fixture.userPlaylist == nil && got != want {
		report.Problems = append(report.Problems, fmt.Sprintf("найденные, пропуски, недоступные и ожидающие: %v, ожидалось %v", got, want))
	}
	if fixture.wantFoundIDs != nil && !slices.Equal(foundIDs, fixture.wantFoundIDs) {
		report.Problems = append(report.Problems, "порядок или ID подобранных записей отличаются от ожидаемых")
	}
	if fixture.wantRequests > 0 && len(run.calls) != fixture.wantRequests {
		report.Problems = append(report.Problems, fmt.Sprintf("HTTP-запросов %d, ожидалось %d", len(run.calls), fixture.wantRequests))
	}
	if report.SearchRequests != nil && (report.SearchRequests.Total != searches || (fixture.userPlaylist == nil && report.SearchRequests.Fallback != fixture.wantFallback)) {
		report.Problems = append(report.Problems, fmt.Sprintf("счётчик поиска %+v, по HTTP-журналу %d", report.SearchRequests, searches))
	} else if searches > 0 && report.SearchRequests == nil {
		report.Problems = append(report.Problems, "нет счётчиков поиска")
	}
	if report.Retries != fixture.wantRetries {
		report.Problems = append(report.Problems, fmt.Sprintf("повторов %d, ожидалось %d", report.Retries, fixture.wantRetries))
	}
	if report.RetryAfter != fixture.wantRetryAfter {
		report.Problems = append(report.Problems, fmt.Sprintf("Retry-After %q, ожидалось %q", report.RetryAfter, fixture.wantRetryAfter))
	}
	for route, unused := range run.routes {
		if len(unused) > 0 && !unused[0].optional {
			report.Problems = append(report.Problems, fmt.Sprintf("не выполнен запрос %s %s (%s)", route.method, route.path, route.query))
		}
	}
	if fixture.userPlaylist != nil {
		comparison := compareUserPlaylist(fixture.userPlaylist, report.Results)
		report.Comparison = &comparison
		if len(report.Results) != len(fixture.userPlaylist) {
			report.Problems = append(report.Problems, fmt.Sprintf("получено %d позиций, ожидалось %d", len(report.Results), len(fixture.userPlaylist)))
		}
		if !slices.Equal(run.written, foundIDs) {
			report.Problems = append(report.Problems, "в плейлист записаны другие позиции или нарушен порядок")
		}
	}
	report.Passed = len(report.Problems) == 0
	// Mock playlist IDs are never presented as links to real providers.
	report.Playlist.URL = ""
	return report
}

type offlineProgress struct {
	Completed int            `json:"completed,omitempty"`
	Call      *offlineCall   `json:"call,omitempty"`
	Report    *offlineReport `json:"report,omitempty"`
}

func listOfflineTests(w http.ResponseWriter, _ *http.Request) {
	type option struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Requests int    `json:"requests"`
	}
	var options []option
	for _, fixture := range fullOfflineFixtures() {
		options = append(options, option{fixture.id, fixture.label, fixture.wantRequests})
	}
	writeOfflineJSON(w, options)
}

func runOfflineTest(w http.ResponseWriter, r *http.Request) {
	fixtures := append(fullOfflineFixtures(), offlineFixtures()...)
	for _, fixture := range fixtures {
		if fixture.id != r.PathValue("id") {
			continue
		}

		timeout := 20 * time.Second
		if len(fixture.userPlaylist) > 100 || fixture.wantRequests > 300 {
			timeout = 45 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if r.URL.Query().Get("progress") != "1" {
			writeOfflineJSON(w, executeOfflineTest(ctx, fixture, 0, nil))
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		encoder := json.NewEncoder(w)
		controller := http.NewResponseController(w)
		send := func(event offlineProgress) {
			if err := encoder.Encode(event); err != nil {
				cancel()
				return
			}
			if err := controller.Flush(); err != nil {
				cancel()
			}
		}
		report := executeOfflineTest(ctx, fixture, 45*time.Millisecond, func(completed int, call offlineCall) {
			send(offlineProgress{Completed: completed, Call: &call})
		})
		send(offlineProgress{Report: &report})
		return
	}
	http.NotFound(w, r)
}

func writeOfflineJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(data)
}
