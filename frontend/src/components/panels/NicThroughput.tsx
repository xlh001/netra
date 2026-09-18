import { useEffect, useRef, useState } from 'react'
import { useI18n, useT } from '../../i18n/context'
import { usePolling } from '../../hooks/usePolling'
import { useAnimatedNumber } from '../../hooks/useAnimatedNumber'
import { getIfaces } from '../../api/client'
import { formatBps, formatCount } from '../../lib/format'
import type { IfaceStatus } from '../../api/types'

const SAMPLE_WINDOW_MS = 60_000
const MAX_SAMPLES = 60
const NIC_BLUE = '#5B8DEF'
const AGG_LIST_MAX = 5

interface Sample {
  t: number
  bps: number
}

function speedLabel(speedMbps?: number): string {
  if (!speedMbps) return ''
  return speedMbps >= 1000 ? speedMbps / 1000 + 'G' : speedMbps + 'M'
}

function drawSparkline(canvas: HTMLCanvasElement, vals: number[], up: boolean) {
  const wrap = canvas.parentElement
  const ctx = canvas.getContext('2d')
  if (!wrap || !ctx) return
  const dpr = window.devicePixelRatio || 1
  const w = wrap.clientWidth
  const h = wrap.clientHeight
  if (!w || !h) return
  canvas.width = w * dpr
  canvas.height = h * dpr
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, w, h)

  ctx.strokeStyle = 'rgba(150,160,180,0.10)'
  ctx.lineWidth = 1
  for (let i = 1; i < 3; i++) {
    const y = (h / 3) * i
    ctx.beginPath()
    ctx.moveTo(0, y)
    ctx.lineTo(w, y)
    ctx.stroke()
  }

  const series = vals.length ? vals : [0]
  const max = Math.max(0.05, ...series) * 1.15
  const stepX = series.length > 1 ? w / (series.length - 1) : w
  const points: [number, number][] = series.map((v, i) => [i * stepX, h - (v / max) * h * 0.82 - 2])

  if (up) {
    const grad = ctx.createLinearGradient(0, 0, 0, h)
    grad.addColorStop(0, 'rgba(91,141,239,0.34)')
    grad.addColorStop(1, 'rgba(91,141,239,0.02)')
    ctx.beginPath()
    ctx.moveTo(0, h)
    points.forEach(([x, y]) => ctx.lineTo(x, y))
    ctx.lineTo(w, h)
    ctx.closePath()
    ctx.fillStyle = grad
    ctx.fill()
  }

  ctx.beginPath()
  points.forEach(([x, y], i) => (i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y)))
  ctx.strokeStyle = up ? NIC_BLUE : 'rgba(139,147,160,.5)'
  ctx.lineWidth = 1.4
  if (up) {
    ctx.shadowColor = 'rgba(91,141,239,.55)'
    ctx.shadowBlur = 5
  }
  ctx.stroke()
  ctx.shadowBlur = 0

  if (up && points.length) {
    const [ex, ey] = points[points.length - 1]
    ctx.beginPath()
    ctx.arc(ex, ey, 2.4, 0, Math.PI * 2)
    ctx.fillStyle = NIC_BLUE
    ctx.shadowColor = 'rgba(91,141,239,.85)'
    ctx.shadowBlur = 6
    ctx.fill()
    ctx.shadowBlur = 0
  }
}

export function NicThroughput() {
  const { t } = useI18n()
  const { data } = usePolling(() => getIfaces(), 5000, [])
  const [history, setHistory] = useState<Record<string, Sample[]>>({})
  const [aggHistory, setAggHistory] = useState<Sample[]>([])

  useEffect(() => {
    if (!data) return
    const now = Date.now()
    const cutoff = now - SAMPLE_WINDOW_MS
    setHistory((prev) => {
      const next: Record<string, Sample[]> = {}
      for (const ifc of data.ifaces) {
        const buf = (prev[ifc.name] ?? []).concat({ t: now, bps: ifc.rxBPS })
        next[ifc.name] = buf.filter((s) => s.t >= cutoff).slice(-MAX_SAMPLES)
      }
      return next
    })
    const total = data.ifaces.reduce((a, i) => a + i.rxBPS, 0)
    setAggHistory((prev) => prev.concat({ t: now, bps: total }).filter((s) => s.t >= cutoff).slice(-MAX_SAMPLES))
  }, [data])

  const ifaces = data?.ifaces ?? []
  const sorted = [...ifaces].sort((a, b) => b.rxBPS - a.rxBPS)

  return (
    <div className="panel nic-panel">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('nicPanelTitle')}</span>
        </h2>
        {ifaces.length > 0 && (
          <span className={'mode-tag' + (data?.xdpGenericMode ? ' generic' : '')}>
            {data?.xdpGenericMode ? t('nicModeGeneric') : t('nicModeNative')}
          </span>
        )}
      </div>
      {ifaces.length === 0 && <div className="empty">{t('noData')}</div>}
      {ifaces.length === 1 && (
        <div className="nic-hero-fill">
          <NicChart iface={sorted[0]} buffer={history[sorted[0].name] ?? []} compact={false} />
          <HeroFooter buffer={history[sorted[0].name] ?? []} />
        </div>
      )}
      {ifaces.length >= 2 && <NicAggregate ifaces={sorted} buffer={aggHistory} />}
    </div>
  )
}

function HeroFooter({ buffer }: { buffer: Sample[] }) {
  const t = useT()
  if (!buffer.length) return null
  const vals = buffer.map((s) => s.bps)
  const peak = Math.max(...vals)
  const avg = vals.reduce((a, b) => a + b, 0) / vals.length
  const animatedPeak = useAnimatedNumber(peak, formatBps)
  const animatedAvg = useAnimatedNumber(avg, formatBps)
  return (
    <div className="kpi-grid">
      <div className="kpi-tile">
        <div className="kpi-label">{t('nicPeak60s')}</div>
        <div className="kpi-value" style={{ fontSize: 14 }}>
          {animatedPeak}
        </div>
      </div>
      <div className="kpi-tile">
        <div className="kpi-label">{t('nicAvg60s')}</div>
        <div className="kpi-value" style={{ fontSize: 14 }}>
          {animatedAvg}
        </div>
      </div>
    </div>
  )
}

function NicAggregate({ ifaces, buffer }: { ifaces: IfaceStatus[]; buffer: Sample[] }) {
  const { t, language } = useI18n()
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const upCount = ifaces.filter((i) => i.carrierUp).length
  const sumBps = ifaces.reduce((a, i) => a + i.rxBPS, 0)
  const sumPps = ifaces.reduce((a, i) => a + i.rxPPS, 0)
  const animatedSumBps = useAnimatedNumber(sumBps, formatBps)
  const animatedSumPps = useAnimatedNumber(sumPps, (value) => formatCount(value, language))

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const draw = () => drawSparkline(canvas, buffer.map((s) => s.bps), upCount > 0)
    draw()
    window.addEventListener('resize', draw)
    return () => window.removeEventListener('resize', draw)
  }, [buffer, upCount])

  const shown = ifaces.slice(0, AGG_LIST_MAX)
  const overflow = ifaces.length - shown.length

  return (
    <div className="nic-agg">
      <div className="nic-chart-cell">
        <div className="nic-chart-head">
          <span className="nic-agg-badge">
            {t('nicAggregate')} · {ifaces.length} {t('nicUnit')}
          </span>
          <span className="nic-agg-up">{upCount}/{ifaces.length} UP</span>
        </div>
        <div className="nic-chart-body">
          <canvas ref={canvasRef} />
          <div className="nic-chart-readout">
            <div className="big">{animatedSumBps}</div>
            <div className="sub">
              <b className="pps-num">{animatedSumPps}</b> pps
            </div>
          </div>
        </div>
      </div>
      <div className="nic-agg-list">
        {shown.map((ifc) => (
          <NicAggregateItem key={ifc.name} iface={ifc} downLabel={t('monitorIfaceDown')} />
        ))}
        {overflow > 0 && <div className="nic-agg-more">+{overflow}</div>}
      </div>
    </div>
  )
}

function NicAggregateItem({ iface, downLabel }: { iface: IfaceStatus; downLabel: string }) {
  const animatedBps = useAnimatedNumber(iface.rxBPS, formatBps)
  return (
    <div className="nic-agg-item" title={iface.name}>
      <span className={'nic-dot' + (iface.carrierUp ? '' : ' down')} />
      <span className="nic-agg-name">{iface.name}</span>
      {speedLabel(iface.speedMbps) && <span className="nic-speed">{speedLabel(iface.speedMbps)}</span>}
      <span className="nic-agg-bps">{iface.carrierUp ? animatedBps : downLabel}</span>
    </div>
  )
}

function NicChart({ iface, buffer, compact }: { iface: IfaceStatus; buffer: Sample[]; compact: boolean }) {
  const { t, language } = useI18n()
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const draw = () => drawSparkline(canvas, buffer.map((s) => s.bps), iface.carrierUp)
    draw()
    window.addEventListener('resize', draw)
    return () => window.removeEventListener('resize', draw)
  }, [buffer, iface.carrierUp])

  const cur = buffer.length ? buffer[buffer.length - 1].bps : 0
  const animatedCur = useAnimatedNumber(cur, formatBps)
  const animatedPps = useAnimatedNumber(iface.rxPPS, (value) => formatCount(value, language))

  return (
    <div className={'nic-chart-cell' + (compact ? ' compact' : '')}>
      <div className="nic-chart-head">
        <span className={'nic-dot' + (iface.carrierUp ? '' : ' down')} />
        <span className="nic-name">{iface.name}</span>
        {speedLabel(iface.speedMbps) && <span className="nic-speed">{speedLabel(iface.speedMbps)}</span>}
      </div>
      <div className="nic-chart-body">
        <canvas ref={canvasRef} />
        <div className="nic-chart-readout">
          {iface.carrierUp ? (
            <>
              <div className="big">{animatedCur}</div>
              <div className="sub"><b className="pps-num">{animatedPps}</b> pps</div>
            </>
          ) : (
            <div className="big" style={{ color: 'var(--rose)', fontSize: compact ? 13 : 18, textShadow: 'none' }}>
              {t('monitorIfaceDown')}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
