import React, { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'

const platforms = {
  file: 'Файл / JSON',
  yandex: 'Яндекс Музыка',
  spotify: 'Spotify',
  youtube: 'YouTube',
  youtubeMusic: 'YouTube Music',
}
const destinations = ['yandex', 'spotify', 'youtube', 'youtubeMusic']
const sources = ['file', ...destinations]

function PlatformIcon({ id }) {
  const shapes = {
    file: <g fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d="M6 3h8l4 4v14H6zM14 3v5h4M9 12h6m-6 4h6"/></g>,
    yandex: <path fill="currentColor" d="M12 1.5h.8l1 8.3 6.9-4.7.6.6-5.4 6.1 8 .9v.8l-8.3 1 4.7 6.9-.6.6-6.1-5.4-.9 7.4h-.8l-1-8-6.9 4.7-.6-.6 5.4-6.1-8-.9v-.8l8.3-1-4.7-6.9.6-.6 6.1 5.4z"/>,
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
        <div className="art-caption">PLAYLIST<br/>BRIDGE</div>
        <svg className="art-lines" viewBox="0 0 420 300" preserveAspectRatio="xMidYMid slice" fill="none">
          <path d="M-15 225C84 133 126 257 207 181S336 84 440 105M-15 239C84 147 126 271 207 195S336 98 440 119M-15 253C84 161 126 285 207 209S336 112 440 133M-15 267C84 175 126 299 207 223S336 126 440 147" stroke="currentColor" strokeWidth="1.3"/>
        </svg>
        <div className="record"><span>♪</span></div>
        <span className="art-star">✳</span>
        <span className="art-index">PB / 01</span>
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
  const [source, setSource] = useState('file')
  const [destination, setDestination] = useState('spotify')
  const [connected, setConnected] = useState(false)
  const [checking, setChecking] = useState(true)
  const [serverError, setServerError] = useState(false)
  const [playlistName, setPlaylistName] = useState('Мой плейлист')
  const [tracksJSON, setTracksJSON] = useState('')
  const [readingFile, setReadingFile] = useState(false)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState(null)
  const [message, setMessage] = useState('')
  const authResult = new URLSearchParams(window.location.search).get('auth')
  const availableRoute = source === 'file' && destination === 'spotify'
  const pendingAccounts = [source !== 'file' && source, destination !== 'spotify' && destination].filter(Boolean)
  const spotifyStatus = checking ? 'Проверяем подключение…' : serverError ? 'Сервер переноса недоступен.' : connected ? 'Аккаунт подключён' : authResult === 'denied' ? 'Доступ не предоставлен.' : authResult === 'invalid' || authResult === 'failed' ? 'Не удалось подключиться. Попробуй ещё раз.' : 'Откроется страница входа.'

  function selectSource(id) {
    setSource(id)
    if (id === destination) setDestination(destinations.find(option => option !== id))
    setMessage('')
    setResult(null)
  }

  function selectDestination(id) {
    setDestination(id)
    setMessage('')
    setResult(null)
  }

  useEffect(() => {
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
      setResult(null)
    } catch { setMessage('Не удалось отключить Spotify. Проверь подключение к серверу.') }
  }

  async function createPlaylist(event) {
    event.preventDefault()
    if (!availableRoute || !connected || busy || readingFile) return
    setMessage('')
    setResult(null)
    let tracks
    try { tracks = JSON.parse(tracksJSON.replace(/^\uFEFF/, '')) }
    catch { setMessage('Не удалось прочитать JSON. Проверь скобки и запятые.'); return }
    if (!Array.isArray(tracks) || tracks.length === 0) { setMessage('Добавь хотя бы один трек в массив JSON.'); return }
    setBusy(true)
    try {
      const response = await fetch('/api/transfer', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: playlistName, tracks }),
      })
      const data = await response.json()
      setResult(data)
      if (!response.ok) {
        setMessage(data.error || 'Не удалось выполнить перенос.')
        if (response.status === 401) setConnected(false)
      }
    } catch { setMessage('Не удалось связаться с сервером. Проверь подключение и попробуй снова.') }
    finally { setBusy(false) }
  }

  const missed = result?.results?.filter(item => !item.found) || []
  const matched = result?.results?.filter(item => item.found).length || 0
  const playlistURL = /^https:\/\/open\.spotify\.com\/playlist\/[a-zA-Z0-9]+(?:\?.*)?$/.test(result?.playlist?.url || '') ? result.playlist.url : ''

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
          <span className="eyebrow"><span aria-hidden="true"/> PLAYLIST BRIDGE</span>
          <h1>Твоя музыка.<br/><em>Новое место.</em></h1>
          <p>Выбери, откуда и куда перенести плейлист.</p>
        </div>

        <div className="workspace">
          <div className="transfer-steps">
            <section className="panel">
              <div className="step-heading"><span aria-hidden="true">01</span><h2>Выбери маршрут</h2></div>
              <PlatformPicker label="Откуда" options={sources} selected={source} onSelect={selectSource} busy={busy}/>
              <PlatformPicker label="Куда" options={destinations} selected={destination} onSelect={selectDestination} disabledOption={source} busy={busy}/>
            </section>

            <section className="panel">
              <div className="step-heading"><span aria-hidden="true">02</span><h2>Подключи аккаунты</h2></div>
              {pendingAccounts.map(id => (
                <div key={id} className="account-row">
                  <PlatformIcon id={id}/>
                  <div><strong>{platforms[id]}</strong><small>{id === source ? 'Вход для чтения плейлиста' : 'Вход для создания плейлиста'}</small></div>
                  <span className="badge">В разработке</span>
                </div>
              ))}
              {destination === 'spotify' && (
                <div className="account-row">
                  <PlatformIcon id={destination}/>
                  <div><strong>{platforms[destination]}</strong><small role="status">{spotifyStatus}</small></div>
                  {connected ? (
                    <button className="account-action" type="button" onClick={disconnectSpotify} disabled={busy}>Отключить</button>
                  ) : (
                    <a className="account-action" href="/auth/spotify">Войти <span aria-hidden="true">↗</span></a>
                  )}
                </div>
              )}
              {!availableRoute && <p className="form-hint route-hint">Этот маршрут пока недоступен для переноса.</p>}
              {!availableRoute && message && <p className="error-message" role="alert">{message}</p>}
            </section>

            {availableRoute && (
              <form className="panel transfer-form" onSubmit={createPlaylist}>
                <div className="step-heading"><span aria-hidden="true">03</span><h2>Подготовь плейлист</h2></div>
                <p className="form-hint">Загрузи файл или вставь список треков в формате JSON.</p>
                <label className="field-label" htmlFor="playlist-name">Название плейлиста</label>
                <input id="playlist-name" value={playlistName} onChange={event => setPlaylistName(event.target.value)} maxLength="100" required/>
                <label className="field-label" htmlFor="tracks-file">JSON-файл</label>
                <input id="tracks-file" type="file" accept=".json,application/json" onChange={readFile} disabled={readingFile || busy}/>
                <label className="field-label" htmlFor="tracks-json">Список треков</label>
                <textarea id="tracks-json" value={tracksJSON} onChange={event => setTracksJSON(event.target.value)} rows="5" spellCheck="false" placeholder={'[\n  {"title": "Numb", "artist": "Linkin Park", "album": "Meteora"}\n]'} disabled={readingFile || busy} required/>
                <button className="primary-button" type="submit" disabled={!connected || busy || readingFile || !tracksJSON.trim()}>{busy ? 'Переносим треки…' : 'Перенести плейлист'} <span aria-hidden="true">↗</span></button>
                {!connected && !checking && <p className="form-hint route-hint">Для переноса сначала подключи аккаунт назначения.</p>}
                <div aria-live="polite" className="transfer-result">
                  {message && <p className="error-message">{message}</p>}
                  {result?.playlist?.id && <p>{result.error ? 'Плейлист уже создан. Проверь его перед повторной попыткой.' : 'Плейлист создан.'} {playlistURL ? <a href={playlistURL} target="_blank" rel="noreferrer">Открыть плейлист ↗</a> : `ID: ${result.playlist.id}`}</p>}
                  {result?.results && <p>Подобрано: {matched} из {result.results.length}. Не найдено: {missed.length}.</p>}
                  {missed.length > 0 && <ul>{missed.map((item, index) => <li key={index}>{item.track.artist} — {item.track.title}</li>)}</ul>}
                </div>
              </form>
            )}
          </div>
          <RouteArtwork source={source} destination={destination}/>
        </div>
      </main>

      <footer className="footer"><span>Playlist Bridge</span><span>© 2026</span></footer>
    </div>
  )
}

createRoot(document.getElementById('root')).render(<App />)
