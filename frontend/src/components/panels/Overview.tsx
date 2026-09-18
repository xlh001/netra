import { useEffect } from 'react'
import { useI18n } from '../../i18n/context'
import { useAnimatedNumber } from '../../hooks/useAnimatedNumber'
import { formatBps, formatBytes, formatCount, windowToSeconds } from '../../lib/format'
import type { Report } from '../../api/types'

function KpiValue({ text }: { text: string }) {
  const m = text.match(/^([\d.,]+)\s*(.*)$/)
  const num = m ? m[1] : text
  const unit = m ? m[2] : ''
  return (
    <div className="kpi-value">
      <span className="kpi-num">{num}</span>
      {unit && <u className="kpi-unit">{unit}</u>}
    </div>
  )
}

type Delta = { text: string; up: boolean } | null

function KpiRow({ label, valueText, delta, live, deltaTitle }: { label: string; valueText: string; delta: Delta; live?: boolean; deltaTitle: string }) {
  return (
    <div className="kpi-row">
      <div className="kpi-row-label">
        {live && <span className="kpi-dot" />}
        <span>{label}</span>
      </div>
      <div className="kpi-row-right">
        {delta && <span title={deltaTitle} className={'kpi-delta' + (delta.up ? ' up' : ' down')}>{delta.text}</span>}
        <KpiValue text={valueText} />
      </div>
    </div>
  )
}

export function Overview({ report }: { report: Report | null }) {
  const { t, language } = useI18n()
  const formatCountLocalized = (n: number) => formatCount(n, language)
  const activeFlows = useAnimatedNumber(report?.activeFlowsNow ?? 0, formatCountLocalized)
  const packets = useAnimatedNumber(report?.totalPackets ?? 0, formatCountLocalized)
  const bytes = useAnimatedNumber(report?.totalBytes ?? 0, formatBytes)

  const windowSeconds = windowToSeconds(report?.window ?? '15m')
  const bps = report ? (report.totalBytes * 8) / windowSeconds : 0
  const prevBps = report?.prevDayTotalBytes ? (report.prevDayTotalBytes * 8) / windowSeconds : undefined

  const pctDelta = (cur: number, prev: number | undefined) => {
    if (prev === undefined || prev === 0) return undefined
    return ((cur - prev) / prev) * 100
  }
  const deltaLabel = (pct: number | undefined) => {
    if (pct === undefined) return null
    const up = pct >= 0
    const sign = up ? '↑' : '↓'
    return { text: `${sign}${Math.abs(pct).toFixed(1)}%`, up }
  }
  const activeFlowsDelta = deltaLabel(pctDelta(report?.activeFlowsNow ?? 0, report?.prevDayActiveFlows))
  const packetsDelta = deltaLabel(pctDelta(report?.totalPackets ?? 0, report?.prevDayTotalPackets))
  const bytesDelta = deltaLabel(pctDelta(report?.totalBytes ?? 0, report?.prevDayTotalBytes))
  const rateDelta = deltaLabel(pctDelta(bps, prevBps))
  const hasDelta = !!(activeFlowsDelta || packetsDelta || bytesDelta || rateDelta)

  useEffect(() => {
    const intensity = Math.min(1, bps / 5e7)
    document.documentElement.style.setProperty('--scan-glow', `rgba(79,208,192,${0.25 + intensity * 0.5})`)
    const sweep = document.querySelector<HTMLElement>('.mark path.sweep')
    if (sweep) sweep.style.animationDuration = 6 - intensity * 3.5 + 's'
  }, [bps])

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('overview')}</span>
        </h2>
        {hasDelta && <span className="count kpi-vs">{t('kpiVsYesterday')}</span>}
      </div>
      <div className="kpi-rows">
        <KpiRow label={t('kpiActive')} valueText={activeFlows} delta={activeFlowsDelta} deltaTitle={t('kpiVsYesterday')} />
        <KpiRow label={t('kpiPackets')} valueText={packets} delta={packetsDelta} deltaTitle={t('kpiVsYesterday')} />
        <KpiRow label={t('kpiBytes')} valueText={bytes} delta={bytesDelta} deltaTitle={t('kpiVsYesterday')} />
        <KpiRow label={t('kpiRate')} valueText={formatBps(bps)} delta={rateDelta} deltaTitle={t('kpiVsYesterday')} />
      </div>
    </div>
  )
}
