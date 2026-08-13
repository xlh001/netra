import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { CompressOutlined, ExpandOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useT } from '../i18n/context'
import { formatRemaining } from '../lib/format'

interface TopBarActionsProps {
  isFullscreen: boolean
  onToggleFullscreen: () => void
}

export function TopBarActions({ isFullscreen, onToggleFullscreen }: TopBarActionsProps) {
  const t = useT()
  const { user, logout } = useAuth()
  const location = useLocation()
  const showFullscreenButton = location.pathname === '/'

  const [, forceTick] = useState(0)
  useEffect(() => {
    const id = window.setInterval(() => forceTick((n) => n + 1), 60_000)
    return () => window.clearInterval(id)
  }, [])

  if (!user) return null

  return (
    <div className="topbar-actions">
      <div className="topbar-user" title={t('sessionExpiresIn', { time: formatRemaining(user.expiresAt) })}>
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
