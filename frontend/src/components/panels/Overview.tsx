import { useEffect } from 'react'
import { useT } from '../../i18n/context'
import { useAnimatedNumber } from '../../hooks/useAnimatedNumber'
import { formatBps, formatBytes, windowToSeconds } from '../../lib/format'
import type { Report } from '../../api/types'

export function Overview({ report }: { report: Report | null }) {
  const t = useT()
  const activeFlows = useAnimatedNumber(report?.activeFlowsNow ?? 0, (v) => Math.round(v).toLocaleString())
  const packets = useAnimatedNumber(report?.totalPackets ?? 0, (v) => Math.round(v).toLocaleString())
  const bytes = useAnimatedNumber(report?.totalBytes ?? 0, formatBytes)

  const windowSeconds = windowToSeconds(report?.window ?? '15m')
  const bps = report ? (report.totalBytes * 8) / windowSeconds : 0

  useEffect(() => {
    const intensity = Math.min(1, bps / 5e7)
    document.documentElement.style.setProperty('--scan-glow', `rgba(53,224,255,${0.25 + intensity * 0.5})`)
    const sweep = document.querySelector<HTMLElement>('.mark path.sweep')
    if (sweep) sweep.style.animationDuration = 6 - intensity * 3.5 + 's'
  }, [bps])

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('overview')}</span>
        </h2>
      </div>
      <div className="kpi-grid">
        <div className="kpi-tile live">
          <div className="kpi-label">
            <span className="kpi-dot" />
            <span>{t('kpiActive')}</span>
          </div>
          <div className="kpi-value">{activeFlows}</div>
        </div>
        <div className="kpi-tile">
          <div className="kpi-label">{t('kpiPackets')}</div>
          <div className="kpi-value">{packets}</div>
        </div>
        <div className="kpi-tile">
          <div className="kpi-label">{t('kpiBytes')}</div>
          <div className="kpi-value">{bytes}</div>
        </div>
        <div className="kpi-tile">
          <div className="kpi-label">{t('kpiRate')}</div>
          <div className="kpi-value">{formatBps(bps)}</div>
        </div>
      </div>
    </div>
  )
}
