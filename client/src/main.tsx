import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { ActionProvider } from '@/lib/action-context'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ActionProvider>
      <App />
    </ActionProvider>
  </StrictMode>,
)
