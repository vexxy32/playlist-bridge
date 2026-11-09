import React, { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'

function SpotifyIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="11" fill="currentColor"/><path d="M6.2 9.1c3.8-1.2 8.1-.9 11.8 1.1M7 12.3c3.2-.9 6.6-.6 9.7 1M7.7 15.4c2.6-.6 5.2-.3 7.6.8" fill="none" stroke="#10120f" strokeWidth="1.5" strokeLinecap="round"/></svg>
}

function App() {
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
    if (!connected || busy || readingFile) return
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
        <a className="brand" href="#home" aria-label="Playlist Bridge — главная">
          <span className="brand-icon" aria-hidden="true">↗</span>
          <span>playlist<span className="brand-light">bridge</span><span className="brand-dot">.</span></span>
        </a>
        <span className="header-note"><span className="status-dot"/> ТВОЯ МУЗЫКА, ГДЕ УДОБНО</span>
      </header>

      <main id="home" className="main">
        <section className="intro">
          <div className="eyebrow"><span className="eyebrow-line"/> МУЗЫКА БЕЗ ГРАНИЦ <span className="eyebrow-number">/ 01</span></div>
          <h1>Любимые треки.<br/><span>Новое место.</span></h1>
          <p className="lead">Твоя музыка заслуживает быть рядом. Переноси плейлисты в Spotify и продолжай слушать то, что любишь.</p>

          <div className="connect-card">
            <div className="card-top"><span>НАЧНЁМ С ПРОСТОГО</span><span className="card-number">01 — 03</span></div>
            <h2>Подключи Spotify</h2>
            <p>{connected ? 'Аккаунт подключён. Теперь можно переходить к переносу.' : 'Войди через Spotify, чтобы подготовиться к переносу плейлиста.'}</p>
            <a className="spotify-button" href="/auth/spotify"><SpotifyIcon/> {connected ? 'Подключить другой аккаунт' : 'Подключить Spotify'} <span aria-hidden="true">↗</span></a>
            {connected && <button type="button" onClick={disconnectSpotify} disabled={busy}>Отключить Spotify</button>}
            <span className="connect-note" id="connect-note" role="status"><span className="small-dot"/> {checking ? 'Проверяем подключение…' : serverError ? 'Запусти Go-сервер на 127.0.0.1:8080' : connected ? 'Spotify подключён' : authResult === 'denied' ? 'Доступ не предоставлен. Можно попробовать ещё раз.' : authResult === 'invalid' || authResult === 'failed' ? 'Не удалось подключиться. Попробуй ещё раз.' : 'Вход откроется на сайте Spotify'}</span>
          </div>
          <form className="transfer-card" onSubmit={createPlaylist}>
            <div className="card-top"><span>ТВОЙ ПЛЕЙЛИСТ</span><span className="card-number">02 — 03</span></div>
            <h2>Добавь свои треки</h2>
            <p className="form-hint">Выбери JSON-файл или вставь массив треков вручную.</p>
            <label className="field-label" htmlFor="playlist-name">Название плейлиста</label>
            <input id="playlist-name" value={playlistName} onChange={event => setPlaylistName(event.target.value)} maxLength="100" required />
            <label className="field-label" htmlFor="tracks-file">JSON-файл</label>
            <input id="tracks-file" type="file" accept=".json,application/json" onChange={readFile} disabled={readingFile} />
            <label className="field-label" htmlFor="tracks-json">Список треков</label>
            <textarea id="tracks-json" value={tracksJSON} onChange={event => setTracksJSON(event.target.value)} rows="6" spellCheck="false" placeholder={'[\n  {"title": "Numb", "artist": "Linkin Park", "album": "Meteora"}\n]'} disabled={readingFile} required />
            <button className="spotify-button transfer-button" type="submit" disabled={!connected || busy || readingFile || !tracksJSON.trim()}>{busy ? 'Ищем треки и создаём плейлист…' : 'Создать плейлист'} <span aria-hidden="true">↗</span></button>
            {!connected && <p className="form-hint">Сначала подключи Spotify.</p>}
            <div aria-live="polite" className="transfer-result">
              {message && <p className="error-message">{message}</p>}
              {result?.playlist?.id && <p>{result.error ? 'Плейлист уже создан. Проверь его перед повторной попыткой.' : 'Плейлист создан.'} {playlistURL ? <a href={playlistURL} target="_blank" rel="noreferrer">Открыть в Spotify ↗</a> : `ID: ${result.playlist.id}`}</p>}
              {result?.results && <p>Подобрано: {matched} из {result.results.length}. Не найдено: {missed.length}.</p>}
              {missed.length > 0 && <ul>{missed.map((item, index) => <li key={index}>{item.track.artist} — {item.track.title}</li>)}</ul>}
            </div>
          </form>
        </section>

        <aside className="visual" aria-label="Музыка переезжает вместе с вами">
          <div className="visual-top"><span>СОБИРАЕМ ТВОЙ ЗВУК</span><span>✳</span></div>
          <div className="artwork" aria-hidden="true">
            <div className="art-grid"/>
            <div className="disc"><div className="disc-label"><span>YOUR<br/>MUSIC</span><b>↗</b></div></div>
            <div className="art-tag">GOOD MUSIC<br/>GOES PLACES <span>↗</span></div>
            <div className="art-index">PB—001<br/>SIDE A</div>
          </div>
          <div className="visual-bottom"><div><span className="tiny-label">СЛЕДУЮЩАЯ ОСТАНОВКА</span><strong>Твой Spotify <span>↗</span></strong></div><div className="sound-bars" aria-hidden="true"><i/><i/><i/><i/><i/><i/><i/><i/><i/></div></div>
        </aside>
      </main>

      <footer className="footer"><span>PLAYLIST BRIDGE <span className="footer-year">© 2026</span></span><span>СДЕЛАНО ДЛЯ ТЕХ, КТО ЛЮБИТ МУЗЫКУ <span className="footer-spark">✳</span></span></footer>
    </div>
  )
}

createRoot(document.getElementById('root')).render(<App />)
