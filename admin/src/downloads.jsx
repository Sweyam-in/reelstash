import React from 'react'
import { createRoot } from 'react-dom/client'
import DownloadsApp from './DownloadsApp'
import './index.css'
import './reelstash.css'
import './design.css'
import '@fontsource-variable/bricolage-grotesque/opsz.css'
import './discover.css'
import './downloads.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <DownloadsApp />
  </React.StrictMode>
)
