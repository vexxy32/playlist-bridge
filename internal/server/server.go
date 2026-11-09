package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"playlist-bridge/internal/transfer"
)

const redirectURI = "http://127.0.0.1:8080/callback"

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type session struct {
	accessToken string
	expiresAt   time.Time
}

// Server хранит настройки Spotify и временные сессии только в памяти.
type Server struct {
	clientID, clientSecret string
	client                 *http.Client
	tokenURL               string
	mu                     sync.Mutex
	sessions               map[string]session
}

func New(clientID, clientSecret string) *Server {
	return &Server{clientID: clientID, clientSecret: clientSecret, client: &http.Client{Timeout: 10 * time.Second}, tokenURL: "https://accounts.spotify.com/api/token", sessions: make(map[string]session)}
}

// Handler соединяет авторизацию и собранную страницу на одном адресе.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/spotify", s.login)
	mux.HandleFunc("GET /callback", s.callback)
	mux.HandleFunc("GET /api/session", s.sessionStatus)
	mux.HandleFunc("POST /api/transfer", s.transfer)
	mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	return mux
}

type transferRequest struct {
	Name   string          `json:"name"`
	Tracks json.RawMessage `json:"tracks"`
}

type transferResponse struct {
	Playlist transfer.Playlist         `json:"playlist"`
	Results  []transfer.TransferResult `json:"results"`
	Error    string                    `json:"error,omitempty"`
}

func (s *Server) transfer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	respond := func(status int, result transferResponse) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://127.0.0.1:8080" {
		respond(http.StatusForbidden, transferResponse{Error: "Недопустимый адрес запроса"})
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		respond(http.StatusUnsupportedMediaType, transferResponse{Error: "Ожидается JSON"})
		return
	}
	cookie, err := r.Cookie("playlist_session")
	if err != nil {
		respond(http.StatusUnauthorized, transferResponse{Error: "Сначала подключи Spotify"})
		return
	}
	accessToken, ok := s.sessionToken(cookie.Value)
	if !ok {
		respond(http.StatusUnauthorized, transferResponse{Error: "Сессия закончилась, подключи Spotify заново"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	var input transferRequest
	if err := decoder.Decode(&input); err != nil {
		respond(http.StatusBadRequest, transferResponse{Error: "Не удалось прочитать запрос JSON"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respond(http.StatusBadRequest, transferResponse{Error: "Некорректный запрос JSON"})
		return
	}
	tracks, err := transfer.ParseTracks(input.Tracks)
	if err != nil {
		respond(http.StatusBadRequest, transferResponse{Error: err.Error()})
		return
	}
	if strings.TrimSpace(input.Name) == "" || len(tracks) == 0 {
		respond(http.StatusBadRequest, transferResponse{Error: "Укажи название и добавь хотя бы один трек"})
		return
	}
	playlist, results, err := transfer.CreateTransfer(
		r.Context(), tracks, input.Name,
		transfer.SearchSpotify(accessToken), transfer.CreateSpotify(accessToken), transfer.AddToSpotify(accessToken),
	)
	result := transferResponse{Playlist: playlist, Results: results}
	if err != nil {
		result.Error = err.Error()
		respond(http.StatusBadGateway, result)
		return
	}
	respond(http.StatusOK, result)
}

func randomString() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomString()
	if err != nil {
		http.Error(w, "Не удалось начать подключение", http.StatusInternalServerError)
		return
	}
	setCookie(w, "spotify_state", state, 600)
	params := url.Values{"client_id": {s.clientID}, "response_type": {"code"}, "redirect_uri": {redirectURI}, "state": {state}, "scope": {"playlist-modify-private playlist-modify-public"}}
	http.Redirect(w, r, "https://accounts.spotify.com/authorize?"+params.Encode(), http.StatusSeeOther)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("spotify_state")
	state := r.URL.Query().Get("state")
	setCookie(w, "spotify_state", "", -1)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		http.Redirect(w, r, "/?auth=invalid", http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/?auth=denied", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/?auth=failed", http.StatusSeeOther)
		return
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		http.Redirect(w, r, "/?auth=failed", http.StatusSeeOther)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.clientID, s.clientSecret)
	resp, err := s.client.Do(req)
	if err != nil {
		http.Redirect(w, r, "/?auth=failed", http.StatusSeeOther)
		return
	}
	defer resp.Body.Close()
	var result tokenResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&result) != nil || result.AccessToken == "" || result.ExpiresIn <= 0 {
		http.Redirect(w, r, "/?auth=failed", http.StatusSeeOther)
		return
	}
	id, err := randomString()
	if err != nil {
		http.Redirect(w, r, "/?auth=failed", http.StatusSeeOther)
		return
	}
	previous, _ := r.Cookie("playlist_session")
	now := time.Now()
	s.mu.Lock()
	if previous != nil {
		delete(s.sessions, previous.Value)
	}
	for key, entry := range s.sessions {
		if !now.Before(entry.expiresAt) {
			delete(s.sessions, key)
		}
	}
	s.sessions[id] = session{
		accessToken: result.AccessToken,
		expiresAt:   now.Add(time.Duration(result.ExpiresIn) * time.Second),
	}
	s.mu.Unlock()
	setCookie(w, "playlist_session", id, result.ExpiresIn)
	http.Redirect(w, r, "/?auth=connected", http.StatusSeeOther)
}

func (s *Server) sessionToken(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[id]
	if !ok {
		return "", false
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(s.sessions, id)
		return "", false
	}
	return entry.accessToken, true
}

func (s *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	connected := false
	if cookie, err := r.Cookie("playlist_session"); err == nil {
		_, connected = s.sessionToken(cookie.Value)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Connected bool `json:"connected"`
	}{connected})
}
