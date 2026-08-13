import { useState } from 'react'
import { Tooltip } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useConfigContext } from '../config/context'
import { useT } from '../i18n/context'
import { Chat } from '../pages/Chat'
import { Logo } from './Logo'

export function AIChatWidget() {
  const t = useT()
  const { user } = useAuth()
  const { config } = useConfigContext()
  const [open, setOpen] = useState(false)

  if (user?.role !== 'admin') return null

  return (
    <>
      {open && (
        <div className="ai-widget-overlay" onClick={() => setOpen(false)}>
          {}
          <div className="ai-widget-panel" onClick={(e) => e.stopPropagation()}>
            <div className="ai-widget-panel-head">
              <span>{t('aiPageTitle')}</span>
              <div className="ai-widget-panel-head-actions">
                <button type="button" className="icon-btn" onClick={() => setOpen(false)} title={t('aiWidgetClose')}>
                  <CloseOutlined />
                </button>
              </div>
            </div>
            <div className="ai-widget-panel-body">
              <Chat aiEnabled={config?.aiEnabled ?? false} />
            </div>
          </div>
        </div>
      )}
      {}
      {!open && (
        <Tooltip title={t('aiWidgetTooltip')} placement="left">
          <button type="button" className="ai-widget-fab" onClick={() => setOpen(true)}>
            <Logo />
          </button>
        </Tooltip>
      )}
    </>
  )
}
