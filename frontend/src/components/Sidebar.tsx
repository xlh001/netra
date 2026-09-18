import { NavLink } from 'react-router-dom'
import { AlertOutlined, AppstoreOutlined, DashboardOutlined, DesktopOutlined, SettingOutlined, TableOutlined, TeamOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useT } from '../i18n/context'
import { Logo } from './Logo'

const MAIN_NAV_ITEMS = [
  { to: '/', key: 'navDashboard', end: true, icon: <DashboardOutlined /> },
  { to: '/overview', key: 'navInsights', end: false, icon: <AppstoreOutlined /> },
  { to: '/flows', key: 'navFlows', end: false, icon: <TableOutlined /> },
  { to: '/threats', key: 'navThreats', end: false, icon: <AlertOutlined /> },
] as const

const ADMIN_NAV_ITEMS = [
  { to: '/settings', key: 'navSettings', icon: <SettingOutlined /> },
  { to: '/users', key: 'navUsers', icon: <TeamOutlined /> },
  { to: '/monitor', key: 'navMonitor', icon: <DesktopOutlined /> },
] as const

export function Sidebar() {
  const t = useT()
  const { user } = useAuth()

  const items = user?.role === 'admin' ? [...MAIN_NAV_ITEMS, ...ADMIN_NAV_ITEMS] : MAIN_NAV_ITEMS

  return (
    <nav className="sidebar">
      <div className="sidebar-brand">
        <Logo />
      </div>
      {items.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={'end' in item ? item.end : false}
          className={({ isActive }) => 'nav-item' + (isActive ? ' active' : '')}
        >
          {item.icon}
          <span className="nav-label">{t(item.key)}</span>
        </NavLink>
      ))}
      <div style={{ flex: 1 }} />
    </nav>
  )
}
