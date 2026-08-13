import { useEffect, useRef } from 'react'
import { useT } from '../../i18n/context'
import type { ThreatAlert } from '../../api/types'

function colorWithAlpha(rgbStr: string, alpha: number): string {
  const m = rgbStr.match(/[\d.]+/g)
  if (!m || m.length < 3) return rgbStr
  return `rgba(${m[0]},${m[1]},${m[2]},${alpha})`
}

export function ScanRadar({ scanAlerts }: { scanAlerts: ThreatAlert[] | undefined }) {
  const t = useT()
  const elRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const activeRef = useRef(false)
  const active = (scanAlerts ?? []).length > 0
  activeRef.current = active

  useEffect(() => {
    const canvas = canvasRef.current
    const el = elRef.current
    if (!canvas || !el) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    let angle = 0
    let lastTs: number | null = null
    let startTs: number | null = null
    let raf = 0

    function draw(ts: number) {
      raf = requestAnimationFrame(draw)
      const w = canvas!.clientWidth
      const h = canvas!.clientHeight
      if (!w || !h) return
      if (canvas!.width !== w || canvas!.height !== h) {
        canvas!.width = w
        canvas!.height = h
      }
      if (startTs == null) startTs = ts

      const alert = activeRef.current
      const periodMs = alert ? 1500 : 4500
      const dt = lastTs == null ? 0 : ts - lastTs
      lastTs = ts
      angle = (angle + (dt / periodMs) * Math.PI * 2) % (Math.PI * 2)
      const elapsed = (ts - startTs) / 1000

      ctx!.clearRect(0, 0, w, h)
      const cx = w / 2
      const cy = h / 2
      const r = Math.min(w, h) / 2 - 8
      const color = getComputedStyle(el!).color

      ctx!.beginPath()
      ctx!.arc(cx, cy, r, 0, Math.PI * 2)
      ctx!.fillStyle = colorWithAlpha(color, 0.08)
      ctx!.fill()

      const spokes = 12
      ctx!.lineWidth = 1
      ctx!.strokeStyle = colorWithAlpha(color, 0.18)
      for (let s = 0; s < spokes; s++) {
        const sa = (s / spokes) * Math.PI * 2
        ctx!.beginPath()
        ctx!.moveTo(cx, cy)
        ctx!.lineTo(cx + r * Math.cos(sa), cy + r * Math.sin(sa))
        ctx!.stroke()
      }

      for (let ring = 1; ring <= 3; ring++) {
        const rr = r * (ring / 3)
        const pulse = 0.16 + 0.12 * (0.5 + 0.5 * Math.sin(elapsed * 1.1 - ring * 0.8))
        ctx!.beginPath()
        ctx!.arc(cx, cy, rr, 0, Math.PI * 2)
        ctx!.strokeStyle = colorWithAlpha(color, pulse)
        ctx!.lineWidth = 1
        ctx!.stroke()
      }

      ctx!.beginPath()
      ctx!.arc(cx, cy, r, 0, Math.PI * 2)
      ctx!.strokeStyle = colorWithAlpha(color, 0.85)
      ctx!.lineWidth = 2.5
      ctx!.shadowBlur = 8
      ctx!.shadowColor = color
      ctx!.stroke()
      ctx!.shadowBlur = 0

      const trailSpan = Math.PI / 2.2
      const steps = 40
      for (let i = 0; i < steps; i++) {
        const a = angle - (i / steps) * trailSpan - Math.PI / 2
        ctx!.strokeStyle = colorWithAlpha(color, (1 - i / steps) * 0.45)
        ctx!.lineWidth = 2
        ctx!.beginPath()
        ctx!.moveTo(cx, cy)
        ctx!.lineTo(cx + r * Math.cos(a), cy + r * Math.sin(a))
        ctx!.stroke()
      }

      const lead = angle - Math.PI / 2
      ctx!.strokeStyle = color
      ctx!.lineWidth = 2.5
      ctx!.lineCap = 'round'
      ctx!.shadowBlur = 8
      ctx!.shadowColor = color
      ctx!.beginPath()
      ctx!.moveTo(cx, cy)
      ctx!.lineTo(cx + r * Math.cos(lead), cy + r * Math.sin(lead))
      ctx!.stroke()
      ctx!.shadowBlur = 0

      ctx!.beginPath()
      ctx!.arc(cx, cy, 4, 0, Math.PI * 2)
      ctx!.fillStyle = color
      ctx!.shadowBlur = 6
      ctx!.shadowColor = color
      ctx!.fill()
      ctx!.shadowBlur = 0
    }
    raf = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(raf)
  }, [])

  return (
    <div className="panel" style={{ height: '100%' }}>
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('scanRadarTitle')}</span>
        </h2>
      </div>
      <div className={'scan-radar' + (active ? ' alert' : '')} ref={elRef}>
        <canvas ref={canvasRef} />
        {active && <div className="sr-status">{t('scanRadarAlert', { n: scanAlerts!.length })}</div>}
      </div>
    </div>
  )
}
