import React from 'react'
import { createRoot } from 'react-dom/client'
import AdminApp from './AdminApp'
import './index.css'
import './reelstash.css'
import './design.css'
import '@fontsource-variable/bricolage-grotesque/opsz.css'
import './discover.css'
// Last, so the console look wins over the styles the shared title and people editors bring.
import './console/styles/console.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <AdminApp />
  </React.StrictMode>
)
