import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import App from './App.tsx'
import { AuthProvider } from './auth/context.tsx'
import { I18nProvider } from './i18n/context.tsx'
import { netraAntdTheme } from './styles/theme'
import './index.css'
import './styles/theme.css'
import './styles/layout.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider theme={netraAntdTheme}>
      <I18nProvider>
        <BrowserRouter>
          <AuthProvider>
            <App />
          </AuthProvider>
        </BrowserRouter>
      </I18nProvider>
    </ConfigProvider>
  </StrictMode>,
)
