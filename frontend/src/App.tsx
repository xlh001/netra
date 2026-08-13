import { Route, Routes } from 'react-router-dom'
import { AIChatWidget } from './components/AIChatWidget'
import { ProtectedRoute } from './components/ProtectedRoute'
import { RadarField } from './components/RadarField'
import { Sidebar } from './components/Sidebar'
import { TopBarActions } from './components/TopBarActions'
import { ConfigProvider } from './config/context'
import { useFullscreen } from './hooks/useFullscreen'
import { Dashboard } from './pages/Dashboard'
import { FlowExplorer } from './pages/FlowExplorer'
import { Login } from './pages/Login'
import { Monitoring } from './pages/Monitoring'
import { Settings } from './pages/Settings'
import { ThreatAlerts } from './pages/ThreatAlerts'
import { UserManagement } from './pages/UserManagement'

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/*"
        element={
          <ProtectedRoute>
            <ConfigProvider>
              <Shell />
            </ConfigProvider>
          </ProtectedRoute>
        }
      />
    </Routes>
  )
}

function Shell() {
  const { isFullscreen, toggleFullscreen } = useFullscreen()
  return (
    <div className="shell">
      <RadarField />
      {!isFullscreen && <Sidebar />}
      <div className={'content' + (isFullscreen ? ' content-fullscreen' : '')}>
        {!isFullscreen && <TopBarActions isFullscreen={isFullscreen} onToggleFullscreen={toggleFullscreen} />}
        <Routes>
          <Route path="/" element={<Dashboard isFullscreen={isFullscreen} />} />
          <Route path="/flows" element={<FlowExplorer />} />
          <Route path="/threats" element={<ThreatAlerts />} />
          <Route
            path="/settings"
            element={
              <ProtectedRoute role="admin">
                <Settings />
              </ProtectedRoute>
            }
          />
          <Route
            path="/users"
            element={
              <ProtectedRoute role="admin">
                <UserManagement />
              </ProtectedRoute>
            }
          />
          <Route
            path="/monitor"
            element={
              <ProtectedRoute role="admin">
                <Monitoring />
              </ProtectedRoute>
            }
          />
        </Routes>
      </div>
      {}
      {!isFullscreen && <AIChatWidget />}
    </div>
  )
}

export default App
