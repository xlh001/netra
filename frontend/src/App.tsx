import { useState } from 'react'
import { Route, Routes } from 'react-router-dom'
import type { Window } from './api/types'
import { AIChatWidget } from './components/AIChatWidget'
import { ProtectedRoute } from './components/ProtectedRoute'
import { Sidebar } from './components/Sidebar'
import { TopBarActions } from './components/TopBarActions'
import { WorldBackdrop } from './components/WorldBackdrop'
import { ConfigProvider } from './config/context'
import { useFullscreen } from './hooks/useFullscreen'
import { Dashboard } from './pages/Dashboard'
import { Insights } from './pages/Insights'
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
  const [window, setWindow] = useState<Window>('15m')
  return (
    <div className="shell">
      <WorldBackdrop />
      {!isFullscreen && <Sidebar />}
      <div className={'content' + (isFullscreen ? ' content-fullscreen' : '')}>
        {!isFullscreen && (
          <TopBarActions isFullscreen={isFullscreen} onToggleFullscreen={toggleFullscreen} window={window} onWindowChange={setWindow} />
        )}
        <Routes>
          <Route path="/" element={<Dashboard isFullscreen={isFullscreen} window={window} />} />
          <Route path="/overview" element={<Insights />} />
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
