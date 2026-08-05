import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'

if (new URLSearchParams(window.location.search).get('window') === 'subtitle') {
  document.documentElement.classList.add('subtitle-window')
  document.body.classList.add('subtitle-window')
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
