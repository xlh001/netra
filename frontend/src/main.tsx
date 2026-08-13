import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { ConfigProvider, theme as antdTheme } from 'antd'
import App from './App.tsx'
import { AuthProvider } from './auth/context.tsx'
import { I18nProvider } from './i18n/context.tsx'
import './index.css'
import './styles/theme.css'
import './styles/layout.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider
      theme={{
        algorithm: antdTheme.darkAlgorithm,
        token: {
          colorPrimary: '#35e0ff',
          colorBgContainer: '#131720',
          colorBgElevated: '#1b202c',
          colorBgLayout: '#0a0b0f',
          colorBorder: 'rgba(150,160,180,0.20)',
          colorBorderSecondary: 'rgba(150,160,180,0.14)',
          colorText: '#e2e6ea',
          colorTextSecondary: '#8b93a0',
          fontFamily: "'Microsoft YaHei', 'PingFang SC', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
          borderRadius: 6,
        },
      }}
    >
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
