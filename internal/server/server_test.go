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
	if strings.Contains(res.Body.String(), "user-token") {
		t.Fatal("токен попал в ответ")
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
