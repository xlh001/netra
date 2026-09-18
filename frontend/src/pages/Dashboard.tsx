import { useConfigContext } from '../config/context'
import { usePolling } from '../hooks/usePolling'
import { getFlowRate, getGeo, getReport, getTimeseries, getTopology } from '../api/client'
import type { Window } from '../api/types'
import { Overview } from '../components/panels/Overview'
import { TrafficTrend } from '../components/panels/TrafficTrend'
import { NicThroughput } from '../components/panels/NicThroughput'
import { FlowRateChart } from '../components/panels/FlowRateChart'
import { FlowTicker } from '../components/panels/FlowTicker'
import { GeoPanel } from '../components/panels/GeoPanel'

export function Dashboard({ isFullscreen, window }: { isFullscreen: boolean; window: Window }) {
  const { config } = useConfigContext()
  const intervalMs = config?.refreshIntervalMs ?? 5000

  const { data: report } = usePolling(() => getReport(window), intervalMs, [window])
  const { data: geo } = usePolling(() => getGeo(window), intervalMs, [window])
  const { data: topology } = usePolling(() => getTopology(window), intervalMs, [window])
  const { data: timeseries, loading: tsLoading } = usePolling(() => getTimeseries(window), intervalMs, [window])
  const { data: flowRate } = usePolling(() => getFlowRate(), intervalMs, [])

  return (
    <div className="dash" key={String(isFullscreen)}>
      <div className="dash-map">
        <GeoPanel
          hero
          geo={geo}
          topology={topology}
          topFlows={report?.topFlows ?? []}
          viewMode={config?.geoViewMode ?? 'auto'}
          switchIntervalSec={config?.geoSwitchIntervalSec ?? 25}
        />
      </div>

      <aside className="dash-rail dash-left">
        <Overview report={report} />
        <TrafficTrend timeseries={timeseries} loading={tsLoading} />
      </aside>

      <aside className="dash-rail dash-right">
        <NicThroughput />
        <FlowRateChart flowRate={flowRate ?? null} />
      </aside>

      <div className="dash-ticker">
        <FlowTicker flows={report?.topFlows ?? []} />
      </div>
    </div>
  )
}
