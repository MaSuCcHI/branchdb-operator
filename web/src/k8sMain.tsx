import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import K8sApp from './K8sApp'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <K8sApp />
  </StrictMode>,
)
