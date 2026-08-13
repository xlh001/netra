import { useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { AlertOutlined, ControlOutlined, DashboardOutlined, DownOutlined, RightOutlined, TableOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useT } from '../i18n/context'
import { Logo } from './Logo'

const MAIN_NAV_ITEMS = [
  { to: '/', key: 'navDashboard', end: true, icon: <DashboardOutlined /> },
  { to: '/flows', key: 'navFlows', end: false, icon: <TableOutlined /> },
  { to: '/threats', key: 'navThreats', end: false, icon: <AlertOutlined /> },
] as const

const ADMIN_NAV_ITEMS = [
  { to: '/settings', key: 'navSettings' },
  { to: '/users', key: 'navUsers' },
  { to: '/monitor', key: 'navMonitor' },
] as const

export function Sidebar() {
  const t = useT()
  const { user } = useAuth()
  const location = useLocation()

  const [adminOpen, setAdminOpen] = useState(() => ADMIN_NAV_ITEMS.some((i) => i.to === location.pathname))

  return (
    <nav className="sidebar">
      <div className="sidebar-brand">
        <Logo />
        <h1>Netra</h1>
      </div>
      {MAIN_NAV_ITEMS.map((item) => (
        <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}>
          {item.icon}
          {t(item.key)}
        </NavLink>
      ))}

      {user?.role === 'admin' && (
        <>
          <button type="button" className="nav-group-toggle" onClick={() => setAdminOpen((o) => !o)}>
            <ControlOutlined />
            {t('navSystemMgmt')}
            {adminOpen ? <DownOutlined className="chevron" /> : <RightOutlined className="chevron" />}
          </button>
          {adminOpen &&
            ADMIN_NAV_ITEMS.map((item) => (
              <NavLink key={item.to} to={item.to} className={({ isActive }) => 'nav-item sub' + (isActive ? ' active' : '')}>
                {t(item.key)}
              </NavLink>
            ))}
        </>
      )}

      <div style={{ flex: 1 }} />
    </nav>
  )
}
