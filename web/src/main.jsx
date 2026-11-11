import React, { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'

const platforms = {
  file: 'Файл / JSON',
  json: 'JSON-файл',
  spotify: 'Spotify',
  youtube: 'YouTube',
  youtubeMusic: 'YouTube Music',
}
const destinations = ['spotify', 'youtube', 'youtubeMusic', 'json']
const sources = ['file', 'spotify', 'youtube', 'youtubeMusic']
const quickRoutes = [
  { source: 'youtube', destination: 'spotify' },
  { source: 'spotify', destination: 'youtubeMusic' },
  { source: 'youtubeMusic', destination: 'json' },
]

function storedChoice(key, options, fallback) {
  const value = sessionStorage.getItem(key)
  return options.includes(value) ? value : fallback
}

function spotifyProfileURL(value) {
  try {
    const url = new URL(value)
    return url.origin === 'https://open.spotify.com' && url.pathname.startsWith('/user/') ? url.href : ''
  } catch {
    return ''
  }
}

function PlaylistPicker({ provider, account, loading, error, onLoad, onSelect, selectedID, busy }) {
  const profileURL = spotifyProfileURL(account?.profile?.url)

  return (
    <section className="panel">
      <h2 className="library-heading"><PlatformIcon id={provider}/>Мои плейлисты в {platforms[provider]}</h2>
      <div className="library-actions">
        <button className="account-action" type="button" onClick={onLoad} disabled={loading || busy}>
          {loading ? 'Загружаем…' : account ? 'Обновить список' : 'Показать мои плейлисты'}
        </button>
      </div>
      {error && <p className="error-message" role="alert">{error}</p>}
      {account && (
        <>
          <div className="library-profile">
            {account.profile.imageUrl
              ? <img src={account.profile.imageUrl} alt="" referrerPolicy="no-referrer"/>
              : <PlatformIcon id={provider}/>}
            <strong>{profileURL ? <a href={profileURL} target="_blank" rel="noreferrer">{account.profile.name}</a> : account.profile.name}</strong>
          </div>
          {account.playlists.length === 0
            ? <p className="form-hint">Плейлисты не найдены.</p>
            : (
              <ul className="library-playlists">
                {account.playlists.map(item => (
                  <li className="library-playlist" key={item.id}>
                    {item.imageUrl
                      ? <img src={item.imageUrl} alt="" loading="lazy" referrerPolicy="no-referrer"/>
                      : <PlatformIcon id={provider}/>}
                    <span>{item.title}</span>
                    {provider === 'spotify' && (
                      <a className="library-link" href={playlistLink(provider, item.id)} target="_blank" rel="noreferrer" aria-label={'Открыть в Spotify: ' + item.title}>↗</a>
                    )}
                    <button className="account-action" type="button" onClick={() => onSelect(item)} disabled={busy} aria-pressed={selectedID === item.id} aria-label={'Выбрать плейлист «' + item.title + '»'}>
                      {selectedID === item.id ? 'Выбран' : 'Выбрать'}
                    </button>
                  </li>
                ))}
              </ul>
            )}
        </>
      )}
    </section>
  )
}

function playlistLink(provider, id) {
  if (provider === 'spotify') return 'https://open.spotify.com/playlist/' + encodeURIComponent(id)
  const host = provider === 'youtubeMusic' ? 'music.youtube.com' : 'www.youtube.com'
  return 'https://' + host + '/playlist?list=' + encodeURIComponent(id)
}

function PlatformIcon({ id }) {
  const shapes = {
    json: <g fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d="M6 3h8l4 4v14H6zM14 3v5h4M9 12h6m-6 4h6"/></g>,
    file: <g fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d="M6 3h8l4 4v14H6zM14 3v5h4M9 12h6m-6 4h6"/></g>,
    spotify: <><circle cx="12" cy="12" r="11" fill="currentColor"/><path d="M5.9 9c3.9-1.3 8.5-1 12.2 1.2M7 12.3c3.2-.9 6.8-.6 9.8 1M8 15.4c2.6-.6 5.3-.3 7.6.8" fill="none" stroke="var(--icon-bg)" strokeWidth="1.6" strokeLinecap="round"/></>,
    youtube: <><rect x="1" y="4" width="22" height="16" rx="5" fill="currentColor"/><path d="m10 8 6 4-6 4z" fill="var(--icon-bg)"/></>,
    youtubeMusic: <><circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="1.7"/><path d="m10 7.5 7 4.5-7 4.5z" fill="currentColor"/></>,
  }
  return <span className={`platform-icon platform-icon--${id}`} aria-hidden="true"><svg viewBox="0 0 24 24">{shapes[id]}</svg></span>
}

function PlatformPicker({ label, options, selected, onSelect, disabledOption, busy }) {
  return (
    <fieldset className="platform-picker" disabled={busy}>
      <legend>{label}</legend>
      {options.map(id => (
        <button key={id} type="button" className="platform-option" aria-pressed={selected === id} disabled={id === disabledOption} onClick={() => onSelect(id)}>
          <PlatformIcon id={id}/>
          <span>{platforms[id]}</span>
          {selected === id && <span className="option-check" aria-hidden="true">✓</span>}
        </button>
      ))}
    </fieldset>
  )
}

function RouteArtwork({ source, destination }) {
  return (
    <aside className="route-card" aria-label="Выбранный маршрут">
      <div className="route-art" aria-hidden="true">
        <svg className="art-lines" viewBox="0 0 420 300" preserveAspectRatio="xMidYMid slice" fill="none">
          <path d="M-15 225C84 133 126 257 207 181S336 84 440 105M-15 239C84 147 126 271 207 195S336 98 440 119M-15 253C84 161 126 285 207 209S336 112 440 133M-15 267C84 175 126 299 207 223S336 126 440 147" stroke="currentColor" strokeWidth="1.3"/>
        </svg>
        <div className="record"><span>♪</span></div>
        <span className="art-star">✳</span>
      </div>
      <div className="route-details">
        <span className="eyebrow">ТВОЙ МАРШРУТ</span>
        <div className="route-path" aria-live="polite">
          <span><PlatformIcon id={source}/>{platforms[source]}</span>
          <span className="route-arrow" aria-hidden="true">→</span>
          <span><PlatformIcon id={destination}/>{platforms[destination]}</span>
        </div>
      </div>
    </aside>
  )
}

function App() {
  const [source, setSource] = useState(() => storedChoice('playlist-source', sources, 'file'))
  const [destination, setDestination] = useState(() => {
    const saved = storedChoice('playlist-destination', destinations, 'spotify')
    if (source === saved || (source === 'file' && saved === 'json')) {
      return source === 'spotify' ? 'youtube' : 'spotify'
    }
    return saved
  })
  const [connected, setConnected] = useState(false)
  const [googleConnected, setGoogleConnected] = useState(false)
  const [accounts, setAccounts] = useState({})
  const [accountRequests, setAccountRequests] = useState({})
  const [selectedPlaylist, setSelectedPlaylist] = useState(null)
  const [checking, setChecking] = useState(true)
  const [serverError, setServerError] = useState(false)
  const [playlistName, setPlaylistName] = useState('Мой плейлист')
  const [tracksJSON, setTracksJSON] = useState('')
  const [youtubeURL, setYoutubeURL] = useState('')
  const [spotifyURL, setSpotifyURL] = useState('')
  const [readingFile, setReadingFile] = useState(false)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState(null)
  const [message, setMessage] = useState('')
  const authResult = new URLSearchParams(window.location.search).get('auth')
  const googleAuthResult = new URLSearchParams(window.location.search).get('google')
  const youtubeSource = source === 'youtube' || source === 'youtubeMusic'
  const googleDestination = destination === 'youtube' || destination === 'youtubeMusic'
  const jsonDestination = destination === 'json'
  const availableDestinations = source === 'file' ? destinations.filter(id => id !== 'json') : destinations
  const needsSpotify = source === 'spotify' || destination === 'spotify'
  const needsGoogle = googleDestination || (youtubeSource && selectedPlaylist !== null)
  const accountsReady = (!needsSpotify || connected) && (!needsGoogle || googleConnected)
  const sourceAccount = youtubeSource ? 'youtube' : source
  const canListPlaylists = youtubeSource ? googleConnected : source === 'spotify' && connected
  const spotifyStatus = checking ? 'Проверяем подключение…' : serverError ? 'Сервер переноса недоступен.' : connected ? 'Аккаунт подключён' : authResult === 'denied' ? 'Доступ не предоставлен.' : authResult === 'invalid' || authResult === 'failed' ? 'Не удалось подключиться. Попробуй ещё раз.' : 'Откроется страница входа.'

  function selectRoute(from, to) {
    if (from !== source) {
      if (selectedPlaylist && youtubeSource) setYoutubeURL('')
      setSelectedPlaylist(null)
    }
    setSource(from)
    setDestination(to)
    sessionStorage.setItem('playlist-source', from)
    sessionStorage.setItem('playlist-destination', to)
    setMessage('')
    setResult(null)
  }

  function selectSource(id) {
    const options = id === 'file' ? destinations.filter(option => option !== 'json') : destinations
    const next = id === destination || !options.includes(destination)
      ? options.find(option => option !== id)
      : destination
    selectRoute(id, next)
  }

  function selectPlaylist(item) {
    setSelectedPlaylist(item)
    setPlaylistName(item.title.slice(0, 100))
    const url = playlistLink(source, item.id)
    if (source === 'spotify') setSpotifyURL(url)
    else setYoutubeURL(url)
    setMessage('')
    setResult(null)
    requestAnimationFrame(() => document.getElementById('prepare-playlist')?.scrollIntoView({ block: 'nearest' }))
  }

  function editLink(event, setURL) {
    setURL(event.target.value)
    setSelectedPlaylist(null)
    setMessage('')
    setResult(null)
  }

  useEffect(() => {
    fetch('/api/google-session', { cache: 'no-store' }).then(response => response.json()).then(data => setGoogleConnected(data.connected)).catch(() => {})
    fetch('/api/session', { cache: 'no-store' })
      .then(response => { if (!response.ok) throw new Error('server'); return response.json() })
      .then(data => setConnected(data.connected))
      .catch(() => setServerError(true))
      .finally(() => setChecking(false))
  }, [])

  async function readFile(event) {
    const input = event.currentTarget
    const file = input.files?.[0]
    if (!file) return
    setTracksJSON('')
    setMessage('')
    if (file.size > 2 * 1024 * 1024) {
      input.value = ''
      setMessage('Файл слишком большой: максимум 2 МБ.')
      return
    }
    setReadingFile(true)
    try {
      setTracksJSON(await file.text())
    } catch {
      input.value = ''
      setMessage('Не удалось прочитать файл.')
    } finally {
      setReadingFile(false)
    }
  }

  async function disconnectSpotify() {
    setMessage('')
    try {
      const response = await fetch('/api/logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
      if (!response.ok) throw new Error('logout')
      setConnected(false)
      setAccounts(current => ({ ...current, spotify: null }))
      if (source === 'spotify') setSelectedPlaylist(null)
      setResult(null)
    } catch { setMessage('Не удалось отключить Spotify. Проверь подключение к серверу.') }
  }

  async function disconnectGoogle() {
    setMessage('')
    try {
      const response = await fetch('/api/google-logout', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
      if (!response.ok) throw new Error('logout')
      setGoogleConnected(false)
      setAccounts(current => ({ ...current, youtube: null }))
      if (youtubeSource && selectedPlaylist) {
        setSelectedPlaylist(null)
        setYoutubeURL('')
      }
      setResult(null)
    } catch { setMessage('Не удалось отключить Google. Проверь подключение к серверу.') }
  }

  async function loadAccount(provider) {
    setAccountRequests(current => ({ ...current, [provider]: { loading: true } }))
    try {
      const response = await fetch('/api/' + provider + '-account', { cache: 'no-store' })
      if (!response.ok) {
        if (response.status === 401) {
          setAccounts(current => ({ ...current, [provider]: null }))
          if (provider === 'spotify') setConnected(false)
          else setGoogleConnected(false)
        }
        throw new Error(await response.text())
      }
      const account = await response.json()
      setAccounts(current => ({ ...current, [provider]: account }))
      setAccountRequests(current => ({ ...current, [provider]: {} }))
    } catch (error) {
      setAccountRequests(current => ({
        ...current,
        [provider]: { error: error.message || 'Не удалось получить плейлисты.' },
      }))
    }
  }

  async function createPlaylist(event) {
    event.preventDefault()
    if (!accountsReady || busy || readingFile) return
    if (jsonDestination) { await exportPlaylist(event); return }
    setMessage('')
    setResult(null)
    const input = { name: playlistName, destination }
    if (youtubeSource) {
      input.youtubeUrl = youtubeURL.trim()
      input.youtubeFromAccount = selectedPlaylist !== null
    } else if (source === 'spotify') {
      input.spotifyUrl = spotifyURL.trim()
    } else {
      try { input.tracks = JSON.parse(tracksJSON.replace(/^\uFEFF/, '')) }
      catch { setMessage('Не удалось прочитать JSON. Проверь скобки и запятые.'); return }
      if (!Array.isArray(input.tracks) || input.tracks.length === 0) { setMessage('Добавь хотя бы один трек в массив JSON.'); return }
    }
    setBusy(true)
    try {
      const response = await fetch('/api/transfer', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      })
      const data = await response.json()
      setResult(data)
      if (!response.ok) {
        setMessage(data.error || 'Не удалось выполнить перенос.')
      }
    } catch { setMessage('Не удалось связаться с сервером. Проверь подключение и попробуй снова.') }
    finally { setBusy(false) }
  }

  async function exportPlaylist(event) {
    event.preventDefault()
    if ((source === 'spotify' && !connected) || (youtubeSource && selectedPlaylist && !googleConnected) || busy) return
    setBusy(true)
    setMessage('')
    try {
      const response = await fetch(source === 'spotify' ? '/api/export/spotify' : '/api/export/youtube', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: (source === 'spotify' ? spotifyURL : youtubeURL).trim(),
          fromAccount: youtubeSource && selectedPlaylist !== null,
        }),
      })
      if (!response.ok) {
        const error = await response.text()
        if (response.status === 401) {
          if (source === 'spotify') setConnected(false)
          else setGoogleConnected(false)
        }
        throw new Error(error || 'Не удалось получить плейлист.')
      }
      const file = await response.blob()
      const objectURL = URL.createObjectURL(file)
      const link = document.createElement('a')
      link.href = objectURL
      link.download = 'playlist.json'
      link.click()
      URL.revokeObjectURL(objectURL)
      setMessage('Файл с треками готов.')
    } catch (error) { setMessage(error.message || 'Не удалось связаться с сервером.') }
    finally { setBusy(false) }
  }

  const copyingVideos = youtubeSource && googleDestination
  const missed = result?.results?.filter(item => !item.found) || []
  const matched = result?.results?.filter(item => item.found).length || 0
  const playlistURL = /^https:\/\/(?:open\.spotify\.com\/playlist\/[a-zA-Z0-9]+|(?:www\.youtube\.com|music\.youtube\.com)\/playlist\?list=[a-zA-Z0-9_-]+)(?:\?.*)?$/.test(result?.playlist?.url || '') ? result.playlist.url : ''

  return (
    <div className="app">
      <header className="header">
        <a className="brand" href="#home">
          <span className="brand-icon" aria-hidden="true">♪</span>
          <span>Playlist <span>Bridge</span></span>
        </a>
        <span className="header-note">Перенос плейлистов</span>
      </header>

      <main id="home">
        <div className="intro">
          <h1>Твоя музыка.<br/><em>Новое место.</em></h1>
          <p>Выбери, откуда и куда перенести плейлист.</p>
        </div>

        <div className="workspace">
          <div className="transfer-steps">
            <section className="panel" id="route-picker">
              <div className="step-heading"><span aria-hidden="true">01</span><h2>Выбери маршрут</h2></div>
              <PlatformPicker label="Откуда" options={sources} selected={source} onSelect={selectSource} busy={busy}/>
              <PlatformPicker label="Куда" options={availableDestinations} selected={destination} onSelect={id => selectRoute(source, id)} disabledOption={source} busy={busy}/>
            </section>

            <section className="panel">
              <div className="step-heading"><span aria-hidden="true">02</span><h2>Подключи аккаунты</h2></div>
              {needsSpotify && (
                <div className="account-row">
                  <PlatformIcon id="spotify"/>
                  <div><strong>Spotify</strong><small role="status">{spotifyStatus}</small></div>
                  {connected ? (
                    <button className="account-action" type="button" onClick={disconnectSpotify} disabled={busy || Boolean(accountRequests.spotify?.loading)}>Отключить</button>
                  ) : (
                    <a className="account-action" href="/auth/spotify">Войти <span aria-hidden="true">↗</span></a>
                  )}
                </div>
              )}
              {(googleDestination || youtubeSource) && (
                <div className="account-row">
                  <PlatformIcon id={googleDestination ? destination : source}/>
                  <div><strong>Google / YouTube</strong><small role="status">{googleConnected ? 'Аккаунт подключён' : googleAuthResult === 'failed' || googleAuthResult === 'invalid' ? 'Не удалось войти. Попробуй ещё раз.' : googleDestination ? 'Вход для создания плейлиста' : 'Вход для просмотра своих плейлистов'}</small></div>
                  {googleConnected ? <button className="account-action" type="button" onClick={disconnectGoogle} disabled={busy || Boolean(accountRequests.youtube?.loading)}>Отключить</button> : <a className="account-action" href="/auth/google">Войти <span aria-hidden="true">↗</span></a>}
                </div>
              )}
              {youtubeSource && !googleDestination && !selectedPlaylist && (
                <p className="form-hint route-hint">Публичный плейлист можно открыть по ссылке без входа в Google.</p>
              )}
            </section>

            {canListPlaylists && (
              <PlaylistPicker
                provider={source}
                account={accounts[sourceAccount]}
                loading={accountRequests[sourceAccount]?.loading}
                error={accountRequests[sourceAccount]?.error}
                onLoad={() => loadAccount(sourceAccount)}
                onSelect={selectPlaylist}
                selectedID={selectedPlaylist?.id}
                busy={busy}
              />
            )}

            <form className="panel transfer-form" id="prepare-playlist" onSubmit={createPlaylist}>
              <div className="step-heading"><span aria-hidden="true">03</span><h2>Подготовь плейлист</h2></div>
              <p className="form-hint">{youtubeSource ? 'Выбери свой плейлист или вставь ссылку.' : source === 'spotify' ? 'Вставь ссылку на трек или выбери плейлист, где ты владелец или соавтор.' : 'Загрузи файл или вставь список треков в формате JSON.'}</p>
              {!jsonDestination && <><label className="field-label" htmlFor="playlist-name">Название плейлиста</label>
              <input id="playlist-name" value={playlistName} onChange={event => setPlaylistName(event.target.value)} maxLength="100" required/></>}
              {youtubeSource ? (
                <>
                  <label className="field-label" htmlFor="youtube-url">Ссылка на плейлист</label>
                  <input id="youtube-url" type="url" value={youtubeURL} onChange={event => editLink(event, setYoutubeURL)} disabled={busy} required/>
                </>
              ) : source === 'spotify' ? (
                <>
                  <label className="field-label" htmlFor="spotify-transfer-url">Ссылка на трек или плейлист Spotify</label>
                  <input id="spotify-transfer-url" type="url" value={spotifyURL} onChange={event => editLink(event, setSpotifyURL)} disabled={busy} required/>
                  {!jsonDestination && <button className="account-action" type="button" onClick={exportPlaylist} disabled={!connected || busy || !spotifyURL.trim()}>Скачать список в JSON</button>}
                </>
              ) : (
                <>
                  <label className="field-label" htmlFor="tracks-file">JSON-файл</label>
                  <input id="tracks-file" type="file" accept=".json,application/json" onChange={readFile} disabled={readingFile || busy}/>
                  <label className="field-label" htmlFor="tracks-json">Список треков</label>
                  <textarea id="tracks-json" value={tracksJSON} onChange={event => setTracksJSON(event.target.value)} rows="5" spellCheck="false" placeholder={'[\n  {"title": "Numb", "artist": "Linkin Park", "album": "Meteora"}\n]'} disabled={readingFile || busy} required/>
                </>
              )}
              <button className="primary-button" type="submit" disabled={!accountsReady || busy || readingFile || !(youtubeSource ? youtubeURL : source === 'spotify' ? spotifyURL : tracksJSON).trim()}>{busy ? 'Обрабатываем…' : jsonDestination ? 'Скачать JSON' : 'Перенести плейлист'} <span aria-hidden="true">↗</span></button>
              {googleDestination && youtubeSource && (
                <p className="form-hint route-hint">Видео копируются по порядку. В YouTube Music отображаются записи, которые сервис считает музыкальными.</p>
              )}
              {!accountsReady && !checking && <p className="form-hint route-hint">Для переноса подключи необходимые аккаунты.</p>}
              <div aria-live="polite" className="transfer-result">
                {message && <p className={message === 'Файл с треками готов.' ? undefined : 'error-message'}>{message}</p>}
                {result?.playlist?.id && <p>{result.error ? 'Плейлист уже создан. Проверь его перед повторной попыткой.' : 'Плейлист создан.'} {playlistURL ? <a href={playlistURL} target="_blank" rel="noreferrer">Открыть плейлист ↗</a> : `ID: ${result.playlist.id}`}</p>}
                {result?.results && <p>{copyingVideos ? 'Доступно для копирования' : 'Подобрано'}: {matched} из {result.results.length}. {copyingVideos ? 'Недоступно' : 'Не найдено'}: {missed.length}.</p>}
                {missed.length > 0 && <ul>{missed.map((item, index) => <li key={index}>{item.track.artist} — {item.track.title}</li>)}</ul>}
              </div>
            </form>
          </div>
          <div className="route-sidebar">
            <RouteArtwork source={source} destination={destination}/>
            <section className="quick-routes" aria-labelledby="quick-routes-heading">
              <h2 id="quick-routes-heading">Быстрые маршруты</h2>
              {quickRoutes.map(route => (
                <button
                  className="quick-route"
                  key={route.source + route.destination}
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    selectRoute(route.source, route.destination)
                    document.getElementById('route-picker')?.scrollIntoView({ block: 'nearest' })
                  }}
                >
                  <PlatformIcon id={route.source}/>
                  <span>{platforms[route.source]} <span aria-hidden="true">→</span> {platforms[route.destination]}</span>
                  <span aria-hidden="true">↗</span>
                </button>
              ))}
            </section>
          </div>
        </div>
      </main>

      <footer className="footer"><span>Playlist Bridge</span><span>© 2026</span></footer>
    </div>
  )
}

createRoot(document.getElementById('root')).render(<App />)
