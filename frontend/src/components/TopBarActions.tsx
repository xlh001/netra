import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { CompressOutlined, ExpandOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useI18n } from '../i18n/context'
import { formatRemaining } from '../lib/format'
import type { Window } from '../api/types'

const RANGE_OPTIONS: { value: Window; key: 'w15' | 'w30' | 'w1h' }[] = [
  { value: '15m', key: 'w15' },
  { value: '30m', key: 'w30' },
  { value: '1h', key: 'w1h' },
]

interface TopBarActionsProps {
  isFullscreen: boolean
  onToggleFullscreen: () => void
  window?: Window
  onWindowChange?: (w: Window) => void
}

export function TopBarActions({ isFullscreen, onToggleFullscreen, window: win, onWindowChange }: TopBarActionsProps) {
  const { t, language } = useI18n()
  const { user, logout } = useAuth()
  const location = useLocation()
  const onDashboard = location.pathname === '/'
  const showFullscreenButton = onDashboard

  const [, forceTick] = useState(0)
  useEffect(() => {
    const id = window.setInterval(() => forceTick((n) => n + 1), 60_000)
    return () => window.clearInterval(id)
  }, [])

  if (!user) return null

  return (
    <div className="topbar-actions">
      {onDashboard && win && onWindowChange && (
        <label className="range-select" title={t('rangeLabel')}>
          <span className="range-select-label">{t('rangeLabel')}</span>
          <select value={win} onChange={(e) => onWindowChange(e.target.value as Window)}>
            {RANGE_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.key)}
              </option>
            ))}
          </select>
        </label>
      )}
      <div className="topbar-user" title={t('sessionExpiresIn', { time: formatRemaining(user.expiresAt, language) })}>
        <span className="name">{user.username}</span>
        <span className="role">{user.role === 'admin' ? t('usersRoleAdmin') : t('usersRoleNormal')}</span>
      </div>
      {showFullscreenButton && (
        <button type="button" className="icon-btn" title={isFullscreen ? t('fullscreenExit') : t('fullscreenEnter')} onClick={onToggleFullscreen}>
          {isFullscreen ? <CompressOutlined /> : <ExpandOutlined />}
        </button>
      )}
      <button
        type="button"
        className="icon-btn"
        style={{ width: 'auto', padding: '0 10px', fontSize: '10px' }}
        title={t('logoutButton')}
        onClick={() => logout()}
      >
        {t('logoutButton')}
      </button>
    </div>
  )
}
