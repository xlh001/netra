import { useRef, useState } from 'react'
import { Tooltip } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
import { useAuth } from '../auth/context'
import { useConfigContext } from '../config/context'
import { useT } from '../i18n/context'
import { Chat } from '../pages/Chat'
import { AiMark } from './AiMark'

const FAB_SIZE = 52
const FAB_MARGIN = 24
const DRAG_THRESHOLD = 4

function clamp(v: number, min: number, max: number) {
  return Math.min(Math.max(v, min), max)
}

function defaultFabPos() {
  return {
    left: window.innerWidth - FAB_SIZE - FAB_MARGIN,
    top: window.innerHeight / 2 - FAB_SIZE / 2,
  }
}

export function AIChatWidget() {
  const t = useT()
  const { user } = useAuth()
  const { config } = useConfigContext()
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState(defaultFabPos)
  const dragRef = useRef<{ pointerId: number; offsetX: number; offsetY: number; startX: number; startY: number; moved: boolean } | null>(null)

  if (user?.role !== 'admin') return null

  function onPointerDown(e: React.PointerEvent<HTMLButtonElement>) {
    const rect = e.currentTarget.getBoundingClientRect()
    dragRef.current = {
      pointerId: e.pointerId,
      offsetX: e.clientX - rect.left,
      offsetY: e.clientY - rect.top,
      startX: e.clientX,
      startY: e.clientY,
      moved: false,
    }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  function onPointerMove(e: React.PointerEvent<HTMLButtonElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    if (!drag.moved) {
      const dist = Math.hypot(e.clientX - drag.startX, e.clientY - drag.startY)
      if (dist < DRAG_THRESHOLD) return
      drag.moved = true
    }
    setPos({
      left: clamp(e.clientX - drag.offsetX, 0, window.innerWidth - FAB_SIZE),
      top: clamp(e.clientY - drag.offsetY, 0, window.innerHeight - FAB_SIZE),
    })
  }

  function onPointerUp(e: React.PointerEvent<HTMLButtonElement>) {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== e.pointerId) return
    e.currentTarget.releasePointerCapture(e.pointerId)
    const wasDrag = drag.moved
    dragRef.current = null
    if (!wasDrag) {
      setOpen(true)
    }
  }

  return (
    <>
      {open && (
        <div className="ai-widget-overlay" onClick={() => setOpen(false)}>
          {}
          <div className="ai-widget-panel" onClick={(e) => e.stopPropagation()}>
            <div className="ai-widget-panel-head">
              <span className="ai-widget-brand">
                <AiMark />
                <span className="ai-widget-brand-title">{t('aiPageTitle')}</span>
                {config?.aiModel && <span className="ai-widget-model">{config.aiModel}</span>}
              </span>
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
          <button
            type="button"
            className="ai-widget-fab"
            style={{ left: pos.left, top: pos.top, right: 'auto', bottom: 'auto' }}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
          >
            <AiMark />
          </button>
        </Tooltip>
      )}
    </>
  )
}
