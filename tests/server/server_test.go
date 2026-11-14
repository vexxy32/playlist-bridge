//go:build playlist_bridge_tests

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpotifyLoginAndCallback(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		id, secret, ok := r.BasicAuth()
		if !ok || id != "client" || secret != "secret" || r.ParseForm() != nil || r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != redirectURI {
			t.Error("invalid token request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"test-token","refresh_token":"test-refresh","expires_in":3600}`))}, nil
	})
	s := New("client", "secret")
	s.client = &http.Client{Transport: transport}
	s.sessions["previous"] = session{accessToken: "previous-token", expiresAt: time.Now().Add(time.Hour)}
	login := httptest.NewRecorder()
	s.Handler().ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/auth/spotify", nil))
	location, err := url.Parse(login.Header().Get("Location"))
	if err != nil || location.Host != "accounts.spotify.com" || location.Query().Get("state") == "" {
		t.Fatal("invalid authorization redirect")
	}
	if location.Query().Get("scope") != "playlist-modify-private playlist-read-private" {
		t.Fatal("запрошены лишние разрешения")
	}
	stateCookie := login.Result().Cookies()[0]
	callback := httptest.NewRequest(http.MethodGet, "/callback?code=test-code&state="+url.QueryEscape(location.Query().Get("state")), nil)
	callback.AddCookie(stateCookie)
	callback.AddCookie(&http.Cookie{Name: "playlist_session", Value: "previous"})
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, callback)
	if response.Header().Get("Location") != "/?auth=connected" {
		t.Fatalf("callback: %s", response.Header().Get("Location"))
	}
	if len(s.sessions) != 1 || strings.Contains(response.Header().Get("Set-Cookie"), "test-token") || strings.Contains(response.Header().Get("Set-Cookie"), "test-refresh") {
		t.Fatal("старая сессия осталась или токен попал в cookie")
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	for _, c := range response.Result().Cookies() {
		if c.Name == "playlist_session" {
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Value == "previous" || c.Value == "test-token" || c.Value == "test-refresh" {
				t.Fatal("ожидался новый идентификатор сессии в HttpOnly cookie")
			}
			statusReq.AddCookie(c)
		}
	}
	status := httptest.NewRecorder()
	s.Handler().ServeHTTP(status, statusReq)
	var result struct {
		Connected bool `json:"connected"`
	}
	if json.Unmarshal(status.Body.Bytes(), &result) != nil || !result.Connected {
		t.Fatal("session not connected")
	}
	if strings.Contains(status.Body.String(), "test-token") || strings.Contains(status.Body.String(), "test-refresh") {
		t.Fatal("token exposed")
	}
}

func TestSessionsAreIsolatedAndLogoutOnlyRemovesCurrentSession(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var tokens []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		body := `{"tracks":{"items":[{"name":"Song","uri":"spotify:track:1","artists":[{"name":"Artist"}]}]}}`
		status := http.StatusOK
		if r.URL.Path == "/v1/me/playlists" {
			status = http.StatusCreated
			body = `{"id":"playlist"}`
		}
		if r.URL.Path == "/v1/playlists/playlist/items" {
			status = http.StatusCreated
			body = `{}`
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("client", "secret")
	s.sessions["first"] = session{accessToken: "token-first", expiresAt: time.Now().Add(time.Hour)}
	s.sessions["second"] = session{accessToken: "token-second", expiresAt: time.Now().Add(time.Hour)}
	for _, id := range []string{"first", "second"} {
		tokens = nil
		req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","tracks":[{"title":"Song","artist":"Artist"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "playlist_session", Value: id})
		res := httptest.NewRecorder()
		s.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK || len(tokens) != 3 {
			t.Fatalf("%s: %d, %v", id, res.Code, tokens)
		}
		for _, token := range tokens {
			if token != "Bearer token-"+id {
				t.Fatalf("неверный токен для %s", id)
			}
		}
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/logout", strings.NewReader(`{}`))
	logout.Header.Set("Content-Type", "application/json")
	logout.AddCookie(&http.Cookie{Name: "playlist_session", Value: "first"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, logout)
	if res.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", res.Code)
	}
	if _, ok := s.sessionToken("first", "playlist_session"); ok {
		t.Fatal("первая сессия осталась")
	}
	if token, ok := s.sessionToken("second", "playlist_session"); !ok || token != "token-second" {
		t.Fatal("вторая сессия повреждена")
	}
	if cookies := res.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatal("cookie не удалена")
	}
}

func TestBaseURLAndSecureCookies(t *testing.T) {
	if _, err := NewWithBaseURL("id", "secret", "http://example.com"); err == nil {
		t.Fatal("небезопасный публичный HTTP принят")
	}
	s, err := NewWithBaseURL("id", "secret", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/auth/spotify", nil))
	u, err := url.Parse(res.Header().Get("Location"))
	if err != nil || u.Query().Get("redirect_uri") != "https://example.com/callback" {
		t.Fatal("неверный redirect URI")
	}
	if cookies := res.Result().Cookies(); len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("cookie должна требовать HTTPS")
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logout.Header.Set("Origin", "https://evil.example")
	logout.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, logout)
	if res.Code != http.StatusForbidden {
		t.Fatal("чужой источник принят")
	}
}

func TestCanceledTransferStopsBeforePlaylistCreation(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/v1/search" {
			t.Errorf("после отмены отправлен запрос %s", r.URL.Path)
		}
		cancel()
		if r.Context().Err() != context.Canceled {
			t.Error("отмена не дошла до запроса Spotify")
		}
		body := `{"tracks":{"items":[{"name":"Song","uri":"spotify:track:1","artists":[{"name":"Artist"}]}]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("client", "secret")
	s.sessions["session"] = session{accessToken: "user-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","tracks":[{"title":"Song","artist":"Artist"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code == http.StatusOK || len(calls) != 1 || calls[0] != "GET /v1/search" {
		t.Fatalf("ответ: %d, запросы: %v", res.Code, calls)
	}
}

func TestCallbackRejectsInvalidState(t *testing.T) {
	s := New("client", "secret")
	req := httptest.NewRequest(http.MethodGet, "/callback?code=test&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: "spotify_state", Value: "expected"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Header().Get("Location") != "/?auth=invalid" || len(s.sessions) != 0 {
		t.Fatal("invalid state accepted")
	}
}

func TestTransferEndpoint(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var calls []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("токен пользователя не передан")
		}
		body := `{}`
		status := http.StatusOK
		switch r.URL.Path {
		case "/v1/search":
			body = `{"tracks":{"items":[{"name":"Numb","uri":"spotify:track:1","album":{"name":"Meteora"},"artists":[{"name":"Linkin Park"}]}]}}`
		case "/v1/me/playlists":
			status = http.StatusCreated
			body = `{"id":"abc123","uri":"spotify:playlist:abc123","external_urls":{"spotify":"https://open.spotify.com/playlist/abc123"}}`
		case "/v1/playlists/abc123/items":
			status = http.StatusCreated
			var data struct {
				URIs []string `json:"uris"`
			}
			if json.NewDecoder(r.Body).Decode(&data) != nil || len(data.URIs) != 1 || data.URIs[0] != "spotify:track:1" {
				t.Error("неверные треки")
			}
		default:
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("client", "secret")
	s.sessions["session"] = session{accessToken: "user-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","tracks":[{"title":"Numb","artist":"Linkin Park","album":"Meteora"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	var result transferResponse
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != http.StatusOK || result.Playlist.ID != "abc123" || len(result.Results) != 1 || !result.Results[0].Found {
		t.Fatalf("ответ: %d %s", res.Code, res.Body.String())
	}
	if len(calls) != 3 {
		t.Fatalf("запросы: %v", calls)
	}
	if result.SearchRequests == nil || result.SearchRequests.Total != 1 || result.SearchRequests.Fallback != 0 {
		t.Fatalf("неверные счётчики: %+v", result.SearchRequests)
	}
	if match := result.Results[0].Match; match == nil || match.Title != "Numb" || match.Artist != "Linkin Park" {
		t.Fatalf("неверное совпадение: %+v", match)
	}
	if strings.Contains(res.Body.String(), "user-token") {
		t.Fatal("токен попал в ответ")
	}
}

func TestYouTubeLinkCreatesSpotifyPlaylist(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	var calls []string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		status := http.StatusOK
		var body string
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			if r.URL.Host != "www.googleapis.com" || r.URL.Query().Get("playlistId") != "PLexample" || r.URL.Query().Get("key") != "test-youtube-key" || r.Header.Get("Authorization") != "" {
				t.Error("неверный запрос YouTube")
			}
			body = `{"items":[{"snippet":{"title":"Artist - Song (Official Video) ft. Guest","videoOwnerChannelTitle":"ArtistVEVO","resourceId":{"videoId":"video"}}}]}`
		case "/v1/search":
			if r.URL.Query().Get("q") != "track:Song artist:Artist" {
				t.Errorf("неверный запрос Spotify: %s", r.URL.Query().Get("q"))
			}
			body = `{"tracks":{"items":[{"name":"Song","uri":"spotify:track:solo","artists":[{"name":"Artist"}]},{"name":"Song","uri":"spotify:track:duet","artists":[{"name":"Artist"},{"name":"Guest"}]}]}}`
		case "/v1/me/playlists":
			status = http.StatusCreated
			body = `{"id":"abc123","uri":"spotify:playlist:abc123"}`
		case "/v1/playlists/abc123/items":
			status = http.StatusCreated
			var data struct {
				URIs []string `json:"uris"`
			}
			if json.NewDecoder(r.Body).Decode(&data) != nil || len(data.URIs) != 1 || data.URIs[0] != "spotify:track:duet" {
				t.Errorf("неверные треки: %v", data.URIs)
			}
		default:
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("client", "secret")
	s.SetYouTubeAPIKey("test-youtube-key")
	s.sessions["session"] = session{accessToken: "user-token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","youtubeUrl":"https://music.youtube.com/watch?v=video&list=PLexample&si=share"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	var result transferResponse
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != http.StatusOK || result.Playlist.ID != "abc123" || len(result.Results) != 1 || !result.Results[0].Found || len(calls) != 4 {
		t.Fatalf("ответ: %d %s, запросы: %v", res.Code, res.Body.String(), calls)
	}
}

func TestTransferRequiresSessionAndRejectsCrossOrigin(t *testing.T) {
	s := New("client", "secret")
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("без сессии: %d", res.Code)
	}
	req.Header.Set("Origin", "http://example.com")
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("чужой источник: %d", res.Code)
	}
}

func TestTransferForwardsSpotifySearchRateLimit(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/search" {
			t.Errorf("неожиданный запрос: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"45"}},
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	})

	s := New("client", "secret")
	s.sessions["session"] = session{accessToken: "token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","tracks":[{"title":"Song","artist":"Artist"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") != "45" {
		t.Fatalf("ответ: %d %v %s", res.Code, res.Header(), res.Body.String())
	}
}

func TestTransferReturnsPlaylistWhenAddingFails(t *testing.T) {
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{}`
		switch r.URL.Path {
		case "/v1/search":
			body = `{"tracks":{"items":[{"name":"Song","uri":"spotify:track:1","artists":[{"name":"Artist"}]}]}}`
		case "/v1/me/playlists":
			status = http.StatusCreated
			body = `{"id":"created123","external_urls":{"spotify":"https://open.spotify.com/playlist/created123"}}`
		case "/v1/playlists/created123/items":
			status = http.StatusTooManyRequests
		default:
			t.Errorf("неожиданный путь: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s := New("client", "secret")
	s.sessions["session"] = session{accessToken: "token", expiresAt: time.Now().Add(time.Hour)}
	req := httptest.NewRequest(http.MethodPost, "/api/transfer", strings.NewReader(`{"name":"Test","tracks":[{"title":"Song","artist":"Artist"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "playlist_session", Value: "session"})
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	var result transferResponse
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != http.StatusBadGateway || result.Playlist.ID != "created123" || result.Error == "" {
		t.Fatalf("не сохранены данные созданного плейлиста: %d %s", res.Code, res.Body.String())
	}
}

func TestOfflineTransferScenarios(t *testing.T) {
	handler := New("", "").Handler()
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/offline-tests", nil))
	var scenarios []struct {
		ID, Label string
		Requests  int
	}
	featured := fullOfflineFixtures()
	if err := json.Unmarshal(list.Body.Bytes(), &scenarios); err != nil || list.Code != http.StatusOK ||
		len(scenarios) != len(featured) {
		t.Fatalf("список сценариев: HTTP %d, %s, %v", list.Code, list.Body.String(), err)
	}
	for i, scenario := range scenarios {
		if scenario.ID != featured[i].id || scenario.Label != featured[i].label || scenario.Requests != featured[i].wantRequests {
			t.Fatalf("неверная кнопка %d: %+v", i, scenario)
		}
	}
	for _, fixture := range append(featured, offlineFixtures()...) {
		t.Run(fixture.id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/offline-tests/"+fixture.id, nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			var report offlineReport
			if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if res.Code != http.StatusOK || !report.Passed {
				t.Fatalf("HTTP %d: %+v, ошибка переноса %q", res.Code, report.Problems, report.Error)
			}
			if fixture.id == "holdout-youtube-spotify-320" {
				c := report.Comparison
				if c == nil || len(report.Results) != 320 || c.Agreed+c.Different+c.Review != 320 {
					t.Fatalf("неполный результат контрольного набора: %+v", c)
				}
				t.Logf("контрольный набор: совпали %d, расхождения %d, спорные %d, записано %d, запросов %d, поиск %+v", c.Agreed, c.Different, c.Review, report.Written, len(report.Calls), report.SearchRequests)
			}
			if fixture.id == "user-youtube-spotify-320" {
				c := report.Comparison
				if c == nil || len(report.Results) != 320 || c.Agreed+c.Different+c.Review != 320 ||
					c.Review != 6 || len(c.Details) != c.Different+c.Review || report.Retries != 2 {
					t.Fatalf("неполный результат для плейлиста на 320 позиций: %+v, повторов %d", c, report.Retries)
				}
				for _, position := range []int{14, 15, 79, 136, 230, 231, 317} {
					if !report.Results[position-1].Unavailable {
						t.Errorf("позиция %d должна быть недоступна", position)
					}
				}
				var pages, batches int
				searchStatuses := make(map[string][]int)
				for _, call := range report.Calls {
					switch call.Operation {
					case "Чтение":
						pages++
					case "Добавление":
						batches++
					case "Поиск":
						searchStatuses[call.Query] = append(searchStatuses[call.Query], call.Status)
					}
				}
				if pages != 7 || report.Written < 200 || batches != (report.Written+99)/100 {
					t.Errorf("прочитано %d страниц, записано %d треков в %d пакетах", pages, report.Written, batches)
				}
				for _, failure := range []struct{ position, status int }{{12, 502}, {35, 429}} {
					query := fixture.userPlaylist[failure.position-1].Query
					got := searchStatuses[query]
					if len(got) != 2 || got[0] != failure.status || got[1] != http.StatusOK {
						t.Errorf("позиция %d: ожидался повтор после HTTP %d, получены статусы %v", failure.position, failure.status, got)
					}
				}
				t.Logf("сравнение: %d совпало, %d разошлось, %d спорных; записано %d; HTTP-запросов %d, поисковых %+v", c.Agreed, c.Different, c.Review, report.Written, len(report.Calls), report.SearchRequests)
			}
			if fixture.id == "user-youtube-spotify" {
				c := report.Comparison
				if c == nil || len(report.Results) != 60 || c.Review != 3 ||
					c.Agreed+c.Different+c.Review != len(report.Results) || len(c.Details) != c.Different+c.Review {
					t.Fatalf("неполное сравнение с присланным списком: %+v", c)
				}
				for _, pair := range [][2]int{{0, 52}, {2, 18}} {
					first, duplicate := report.Results[pair[0]], report.Results[pair[1]]
					if first.URI == "" || first.URI != duplicate.URI {
						t.Fatalf("потерян дубль в позициях %d и %d: %+v, %+v", pair[0]+1, pair[1]+1, first, duplicate)
					}
				}
				if !report.Results[13].Unavailable || !report.Results[14].Unavailable {
					t.Fatal("закрытое и удалённое видео должны быть недоступны")
				}
				// Verify the metadata fixes against IDs from the original mocked responses.
				for _, position := range []int{8, 10, 18, 26, 28, 30, 31, 39, 45, 49, 56} {
					row := fixture.userPlaylist[position-1]
					result := report.Results[position-1]
					if want := "spotify:track:" + row.Expected; !result.Found || result.URI != want {
						t.Errorf("позиция %d (%q): получена запись %q, ожидалась %q", position, row.Title, result.URI, want)
					}
				}
				for _, position := range []int{11, 13, 24, 60} {
					result := report.Results[position-1]
					if result.Found || result.Unavailable || result.Pending {
						t.Errorf("позиция %d (%q): ожидался пропуск, получено %+v", position, result.Track.Title, result)
					}
				}
				if got := report.Results[8]; got.Found && got.URI != "spotify:track:mock-009-a" {
					t.Errorf("Hotel California: концерт заменён другой версией (%q)", got.URI)
				}
				t.Logf("сравнение: %d совпало, %d разошлось, %d спорных; записано %d; HTTP-запросов %d, поисковых %+v", c.Agreed, c.Different, c.Review, report.Written, len(report.Calls), report.SearchRequests)
			}
		})
	}
}

func TestOfflineTransferStreamsProgress(t *testing.T) {
	handler := New("", "").Handler()
	for _, fixture := range fullOfflineFixtures() {
		t.Run(fixture.id, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/offline-tests/"+fixture.id+"?progress=1", nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "application/x-ndjson" || !res.Flushed {
				t.Fatalf("ответ потока: HTTP %d, %v", res.Code, res.Header())
			}

			decoder := json.NewDecoder(res.Body)
			events := 0
			for {
				var event offlineProgress
				if err := decoder.Decode(&event); err != nil {
					t.Fatal(err)
				}
				if event.Report != nil {
					if !event.Report.Passed || (fixture.wantRequests > 0 && events != fixture.wantRequests) || len(event.Report.Calls) != events || event.Report.ElapsedMs < 1 {
						t.Fatalf("событий %d, результат %v, длительность %d мс", events, event.Report.Problems, event.Report.ElapsedMs)
					}
					t.Logf("%d HTTP-ответов за %d мс", events, event.Report.ElapsedMs)
					break
				}
				events++
				if event.Call == nil || event.Completed != events {
					t.Fatalf("событие %d: %+v", events, event)
				}
			}
			if err := decoder.Decode(&offlineProgress{}); err != io.EOF {
				t.Fatalf("после отчёта ожидался конец потока, получено %v", err)
			}
		})
	}
}
