import { useState } from 'react'
import { useT } from '../i18n/context'
import { useConfigContext } from '../config/context'
import { usePolling } from '../hooks/usePolling'
import { TOPN, getDomainsPaged, getGeoRange, getIPsPaged, getPortsPaged, getThreatStats, getTimeseriesRange } from '../api/client'
import type { ThreatStats, TimeRange } from '../api/types'
import { TimeRangeSelector } from '../components/TimeRangeSelector'
import { TrendChart } from '../components/panels/TrendChart'
import { CountriesRanking } from '../components/panels/CountriesRanking'
import { IPsRanking } from '../components/panels/IPsRanking'
import { PortsRanking } from '../components/panels/PortsRanking'
import { DomainsRanking } from '../components/panels/DomainsRanking'

export function Insights() {
  const { config } = useConfigContext()
  const intervalMs = config?.refreshIntervalMs ?? 5000
  const [range, setRange] = useState<TimeRange>({ kind: 'window', window: '15m' })

  const { data: threat } = usePolling(() => getThreatStats(range), intervalMs, [range])
  const { data: timeseries } = usePolling(() => getTimeseriesRange(range), intervalMs, [range])
  const { data: geo } = usePolling(() => getGeoRange(range), intervalMs, [range])
  const { data: ips } = usePolling(() => getIPsPaged(range, 0, TOPN), intervalMs, [range])
  const { data: ports } = usePolling(() => getPortsPaged(range, 0, TOPN), intervalMs, [range])
  const { data: domains } = usePolling(() => getDomainsPaged(range, 0, TOPN), intervalMs, [range])

  return (
    <>
      <div className="hdr">
        <TimeRangeSelector value={range} onChange={setRange} />
      </div>
      <div className="insights">
        <ThreatStrip stats={threat ?? null} />
        <div className="ins-trend">
          <TrendChart timeseries={timeseries ?? null} />
        </div>
        <div className="ins-ranks">
          <CountriesRanking geo={geo ?? null} />
          <IPsRanking items={ips?.ips ?? []} />
          <PortsRanking items={ports?.ports ?? []} />
          <DomainsRanking items={domains?.domains ?? []} />
        </div>
      </div>
    </>
  )
}

function ThreatStrip({ stats }: { stats: ThreatStats | null }) {
  const t = useT()
  const s = stats ?? { scan: 0, ddos: 0, volume: 0, ioc: 0, total: 0 }
  const cards = [
    { kind: 'ddos', color: 'var(--rose)', label: t('threatsKindDDoS'), v: s.ddos },
    { kind: 'scan', color: 'var(--amber)', label: t('threatsKindScan'), v: s.scan },
    { kind: 'ioc', color: 'var(--iris)', label: t('threatsKindIOC'), v: s.ioc },
    { kind: 'volume', color: 'var(--scan)', label: t('threatsKindVolume'), v: s.volume },
  ]
  const pct = (v: number) => (s.total > 0 ? Math.round((v / s.total) * 100) + '%' : '—')
  return (
    <div className="ins-section">
      <div className="ins-section-head">
        <span className="ins-section-title">{t('insightsThreatTitle')}</span>
        <span className="ins-section-total">{t('insightsThreatTotal')} {s.total.toLocaleString()}</span>
      </div>
      <div className="sevstrip">
        {cards.map((c) => (
          <div key={c.kind} className="sevcard">
            <span className="sev-bar" style={{ background: c.color }} />
            <div className="sev-k">{c.label}</div>
            <div className="sev-v" style={{ color: c.color }}>{c.v.toLocaleString()}</div>
            <div className="sev-s">{pct(c.v)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}
