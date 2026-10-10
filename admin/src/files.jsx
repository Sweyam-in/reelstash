import React from 'react'
import { createRoot } from 'react-dom/client'
import FilesApp from './FilesApp'
import './index.css'
import './reelstash.css'
import './design.css'
import '@fontsource-variable/bricolage-grotesque/opsz.css'
import './discover.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <FilesApp />
  </React.StrictMode>
)
