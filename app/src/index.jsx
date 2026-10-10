import React from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'
import './reelstash.css'
import './design.css'
import '@fontsource-variable/bricolage-grotesque/opsz.css'
import './discover.css'

const container = document.getElementById('root')
const root = createRoot(container)
root.render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
)
