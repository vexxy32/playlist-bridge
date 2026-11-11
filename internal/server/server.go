package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"playlist-bridge/internal/transfer"
	"playlist-bridge/internal/youtube"
)

const redirectURI = "http://127.0.0.1:8080/callback"

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type session struct {
	google      bool
	accessToken string
	expiresAt   time.Time
}

// Server хранит настройки Spotify и временные сессии только в памяти.
type Server struct {
	clientID, clientSecret             string
	youtubeAPIKey                      string
	googleClientID, googleClientSecret string
	client                             *http.Client
	tokenURL                           string
	baseURL, redirectURL               string
	secureCookies                      bool
	mu                                 sync.Mutex
	sessions                           map[string]session
}

func New(clientID, clientSecret string) *Server {
	s, _ := NewWithBaseURL(clientID, clientSecret, "http://127.0.0.1:8080")
	return s
}

// NewWithBaseURL настраивает публичный адрес сайта и адрес возврата Spotify.
func NewWithBaseURL(clientID, clientSecret, baseURL string) (*Server, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("адрес сайта должен быть HTTPS или локальным http://127.0.0.1:порт")
	}
	localHTTP := u.Scheme == "http" && u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !localHTTP {
		return nil, fmt.Errorf("адрес сайта должен быть HTTPS или локальным http://127.0.0.1:порт")
	}
	origin := u.Scheme + "://" + u.Host
	return &Server{
		clientID:      clientID,
		clientSecret:  clientSecret,
		client:        &http.Client{Timeout: 10 * time.Second},
		tokenURL:      "https://accounts.spotify.com/api/token",
		baseURL:       origin,
		redirectURL:   origin + "/callback",
		secureCookies: u.Scheme == "https",
		sessions:      make(map[string]session),
	}, nil
}

// SetYouTubeAPIKey включает перенос публичных плейлистов по ссылке.
func (s *Server) SetYouTubeAPIKey(key string) {
	s.youtubeAPIKey = strings.TrimSpace(key)
}

// SetGoogleCredentials включает вход Google для создания плейлистов.
func (s *Server) SetGoogleCredentials(id, secret string) {
	s.googleClientID, s.googleClientSecret = strings.TrimSpace(id), strings.TrimSpace(secret)
}

// Handler соединяет авторизацию и собранную страницу на одном адресе.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/spotify", s.login)
	mux.HandleFunc("GET /auth/google", s.googleLogin)
	mux.HandleFunc("GET /callback/google", s.googleCallback)
	mux.HandleFunc("GET /api/google-session", s.googleSession)
	mux.HandleFunc("GET /api/youtube-account", s.youtubeAccount)
	mux.HandleFunc("GET /api/spotify-account", s.spotifyAccount)
	mux.HandleFunc("POST /api/google-logout", s.googleLogout)
	mux.HandleFunc("GET /callback", s.callback)
	mux.HandleFunc("GET /api/session", s.sessionStatus)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("POST /api/transfer", s.transfer)
	mux.HandleFunc("POST /api/export/spotify", s.exportSpotify)
	mux.HandleFunc("POST /api/export/youtube", s.exportYouTube)
	mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	return mux
}

type transferRequest struct {
	Name               string          `json:"name"`
	Tracks             json.RawMessage `json:"tracks"`
	YouTubeFromAccount bool            `json:"youtubeFromAccount"`
	YouTubeURL         string          `json:"youtubeUrl"`
	SpotifyURL         string          `json:"spotifyUrl"`
	Destination        string          `json:"destination"`
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
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseURL {
		respond(http.StatusForbidden, transferResponse{Error: "Недопустимый адрес запроса"})
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		respond(http.StatusUnsupportedMediaType, transferResponse{Error: "Ожидается JSON"})
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
	destination := input.Destination
	if destination == "" {
		destination = "spotify"
	}
	if destination != "spotify" && destination != "youtube" && destination != "youtubeMusic" {
		respond(http.StatusBadRequest, transferResponse{Error: "Неизвестный сервис назначения"})
		return
	}
	cookieName := "playlist_session"
	if destination != "spotify" {
		cookieName = "google_session"
	}
	cookie, cookieErr := r.Cookie(cookieName)
	if cookieErr != nil {
		respond(http.StatusUnauthorized, transferResponse{Error: "Сначала подключи аккаунт назначения"})
		return
	}
	accessToken, ok := s.sessionToken(cookie.Value, cookie.Name)
	if !ok {
		respond(http.StatusUnauthorized, transferResponse{Error: "Сессия закончилась, подключи аккаунт заново"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		respond(http.StatusBadRequest, transferResponse{Error: "Укажи название плейлиста"})
		return
	}
	if input.hasMultipleSources() {
		respond(http.StatusBadRequest, transferResponse{Error: "Выбери один источник треков"})
		return
	}
	if destination != "spotify" && strings.TrimSpace(input.YouTubeURL) != "" {
		videos, status, err := s.readYouTubeVideos(r, input.YouTubeURL, input.YouTubeFromAccount)
		if err != nil {
			respond(status, transferResponse{Error: err.Error()})
			return
		}
		playlist, results, err := transfer.CopyYouTubePlaylist(r.Context(), accessToken, strings.TrimSpace(input.Name), videos, destination == "youtubeMusic")
		result := transferResponse{Playlist: playlist, Results: results}
		if err != nil {
			result.Error = err.Error()
			respond(http.StatusBadGateway, result)
			return
		}
		respond(http.StatusOK, result)
		return
	}

	tracks, status, err := s.readSource(r, input)
	if err != nil {
		writeRetryAfter(w, err)
		respond(status, transferResponse{Error: err.Error()})
		return
	}
	if len(tracks) == 0 {
		respond(http.StatusBadRequest, transferResponse{Error: "Добавь хотя бы один трек"})
		return
	}
	var search transfer.SearchFunc
	var create transfer.CreateFunc
	var add transfer.AddFunc
	if destination == "spotify" {
		search = transfer.SearchSpotify(accessToken)
		create = transfer.CreateSpotify(accessToken)
		add = transfer.AddToSpotify(accessToken)
	} else {
		if s.youtubeAPIKey == "" {
			respond(http.StatusServiceUnavailable, transferResponse{Error: "Поиск YouTube не настроен"})
			return
		}
		search = transfer.SearchYouTube(s.youtubeAPIKey)
		create = transfer.CreateYouTube(accessToken, destination == "youtubeMusic")
		add = transfer.AddToYouTube(accessToken)
	}
	playlist, results, err := transfer.CreateTransfer(r.Context(), tracks, input.Name, search, create, add)
	result := transferResponse{Playlist: playlist, Results: results}
	if err != nil {
		result.Error = err.Error()
		if writeRetryAfter(w, err) {
			respond(http.StatusTooManyRequests, result)
			return
		}
		respond(http.StatusBadGateway, result)
		return
	}
	respond(http.StatusOK, result)
}

// exportSpotify возвращает список треков в формате, пригодном для повторной загрузки.
func (s *Server) exportSpotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseURL {
		http.Error(w, "Недопустимый адрес запроса", http.StatusForbidden)
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		http.Error(w, "Ожидается JSON", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var input struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "Не удалось прочитать запрос", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}
	tracks, status, err := s.readSpotifySource(r, input.URL)
	if err != nil {
		writeRetryAfter(w, err)
		http.Error(w, err.Error(), status)
		return
	}
	writeTracksJSON(w, tracks)
}

// exportYouTube сохраняет записи доступного по ссылке плейлиста в общем JSON-формате.
func (s *Server) exportYouTube(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseURL {
		http.Error(w, "Недопустимый адрес запроса", http.StatusForbidden)
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		http.Error(w, "Ожидается JSON", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var input struct {
		URL         string `json:"url"`
		FromAccount bool   `json:"fromAccount"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "Не удалось прочитать запрос", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Некорректный запрос", http.StatusBadRequest)
		return
	}
	tracks, status, err := s.readYouTubeSource(r, input.URL, input.FromAccount)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	if len(tracks) == 0 {
		http.Error(w, "В плейлисте нет доступных записей", http.StatusBadRequest)
		return
	}
	writeTracksJSON(w, tracks)
}

func writeTracksJSON(w http.ResponseWriter, tracks []transfer.Track) {
	data, err := transfer.ExportTracksJSON(tracks)
	if err != nil {
		http.Error(w, "Не удалось создать JSON", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="playlist.json"`)
	_, _ = w.Write(data)
}

func (s *Server) googleLogin(w http.ResponseWriter, r *http.Request) {
	if s.googleClientID == "" || s.googleClientSecret == "" {
		http.Error(w, "Вход Google ещё не настроен", http.StatusServiceUnavailable)
		return
	}
	state, err := randomString()
	if err != nil {
		http.Error(w, "Не удалось начать вход", http.StatusInternalServerError)
		return
	}
	s.setCookie(w, "google_state", state, 600)
	queryParams := url.Values{
		"client_id":     {s.googleClientID},
		"redirect_uri":  {s.baseURL + "/callback/google"},
		"response_type": {"code"},
		"scope":         {"https://www.googleapis.com/auth/youtube.force-ssl"},
		"state":         {state},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+queryParams.Encode(), http.StatusSeeOther)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("google_state")
	state := r.URL.Query().Get("state")
	s.setCookie(w, "google_state", "", -1)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Redirect(w, r, "/?google=invalid", http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/?google=failed", http.StatusSeeOther)
		return
	}
	formValues := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {s.baseURL + "/callback/google"},
		"client_id":     {s.googleClientID},
		"client_secret": {s.googleClientSecret},
	}
	requestURL := "https://oauth2.googleapis.com/token"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, requestURL, strings.NewReader(formValues.Encode()))
	if err != nil {
		http.Redirect(w, r, "/?google=failed", http.StatusSeeOther)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		http.Redirect(w, r, "/?google=failed", http.StatusSeeOther)
		return
	}
	defer resp.Body.Close()
	var result tokenResponse
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&result) != nil || result.AccessToken == "" || result.ExpiresIn <= 0 {
		http.Redirect(w, r, "/?google=failed", http.StatusSeeOther)
		return
	}
	id, err := randomString()
	if err != nil {
		http.Redirect(w, r, "/?google=failed", http.StatusSeeOther)
		return
	}
	previous, _ := r.Cookie("google_session")
	s.mu.Lock()
	if previous != nil {
		s.removeSession(previous)
	}
	s.sessions[id] = session{
		google:      true,
		accessToken: result.AccessToken,
		expiresAt:   time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}
	s.mu.Unlock()
	s.setCookie(w, "google_session", id, result.ExpiresIn)
	http.Redirect(w, r, "/?google=connected", http.StatusSeeOther)
}
func (s *Server) googleSession(w http.ResponseWriter, r *http.Request) {
	connected := false
	if cookie, err := r.Cookie("google_session"); err == nil {
		_, connected = s.sessionToken(cookie.Value, cookie.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Connected bool `json:"connected"`
	}{connected})
}

// youtubeAccount отдаёт сведения канала и плейлисты текущей сессии.
func (s *Server) youtubeAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie("google_session")
	if err != nil {
		http.Error(w, "Сначала подключи Google", http.StatusUnauthorized)
		return
	}
	token, ok := s.sessionToken(cookie.Value, cookie.Name)
	if !ok {
		http.Error(w, "Сессия закончилась, подключи Google заново", http.StatusUnauthorized)
		return
	}
	profile, err := youtube.GetUserProfileContext(r.Context(), token)
	if err != nil {
		http.Error(w, "Не удалось получить канал YouTube", http.StatusBadGateway)
		return
	}
	playlists, err := youtube.GetUserPlaylistsContext(r.Context(), token)
	if err != nil {
		http.Error(w, "Не удалось получить плейлисты YouTube", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Profile   youtube.ProfileInfo    `json:"profile"`
		Playlists []youtube.PlaylistInfo `json:"playlists"`
	}{profile, playlists})
}

func (s *Server) googleLogout(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseURL {
		http.Error(w, "Недопустимый адрес запроса", http.StatusForbidden)
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		http.Error(w, "Ожидается JSON", http.StatusUnsupportedMediaType)
		return
	}
	if cookie, err := r.Cookie("google_session"); err == nil {
		s.mu.Lock()
		s.removeSession(cookie)
		s.mu.Unlock()
	}
	s.setCookie(w, "google_session", "", -1)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func randomString() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomString()
	if err != nil {
		http.Error(w, "Не удалось начать подключение", http.StatusInternalServerError)
		return
	}
	s.setCookie(w, "spotify_state", state, 600)
	queryParams := url.Values{
		"client_id":     {s.clientID},
		"response_type": {"code"},
		"redirect_uri":  {s.redirectURL},
		"state":         {state},
		"scope":         {"playlist-modify-private playlist-read-private"},
	}
	http.Redirect(w, r, "https://accounts.spotify.com/authorize?"+queryParams.Encode(), http.StatusSeeOther)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("spotify_state")
	state := r.URL.Query().Get("state")
	s.setCookie(w, "spotify_state", "", -1)
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
	formValues := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {s.redirectURL},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.tokenURL, strings.NewReader(formValues.Encode()))
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
		s.removeSession(previous)
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
	s.setCookie(w, "playlist_session", id, result.ExpiresIn)
	http.Redirect(w, r, "/?auth=connected", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseURL {
		http.Error(w, "Недопустимый адрес запроса", http.StatusForbidden)
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		http.Error(w, "Ожидается JSON", http.StatusUnsupportedMediaType)
		return
	}
	if cookie, err := r.Cookie("playlist_session"); err == nil {
		s.mu.Lock()
		s.removeSession(cookie)
		s.mu.Unlock()
	}
	s.setCookie(w, "playlist_session", "", -1)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// removeSession вызывается под блокировкой s.mu.
func (s *Server) removeSession(cookie *http.Cookie) {
	entry, ok := s.sessions[cookie.Value]
	if ok && entry.google == (cookie.Name == "google_session") {
		delete(s.sessions, cookie.Value)
	}
}

func (s *Server) sessionToken(id, cookieName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[id]
	if !ok || entry.google != (cookieName == "google_session") {
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
		_, connected = s.sessionToken(cookie.Value, cookie.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Connected bool `json:"connected"`
	}{connected})
}
