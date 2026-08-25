import { useState } from 'react'
import { useConfigContext } from '../config/context'
import { usePolling } from '../hooks/usePolling'
import { getFlowRate, getGeo, getReport, getServiceCategoriesWindow, getTimeseries, getTopology } from '../api/client'
import type { Window } from '../api/types'
import { WindowSelector } from '../components/WindowSelector'
import { Overview } from '../components/panels/Overview'
import { CountriesRanking } from '../components/panels/CountriesRanking'
import { IPsRanking } from '../components/panels/IPsRanking'
import { DomainsRanking } from '../components/panels/DomainsRanking'
import { CategoriesRanking } from '../components/panels/CategoriesRanking'
import { FlowsTable } from '../components/panels/FlowsTable'
import { FlowRateChart } from '../components/panels/FlowRateChart'
import { TrendChart } from '../components/panels/TrendChart'
import { ScanRadar } from '../components/panels/ScanRadar'
import { GeoPanel } from '../components/panels/GeoPanel'

export function Dashboard({ isFullscreen }: { isFullscreen: boolean }) {
  const [window, setWindow] = useState<Window>('15m')
  const { config } = useConfigContext()
  const intervalMs = config?.refreshIntervalMs ?? 5000

  const { data: report } = usePolling(() => getReport(window), intervalMs, [window])
  const { data: geo } = usePolling(() => getGeo(window), intervalMs, [window])
  const { data: timeseries } = usePolling(() => getTimeseries(window), intervalMs, [window])
  const { data: flowRate } = usePolling(() => getFlowRate(), intervalMs, [])
  const { data: topology } = usePolling(() => getTopology(window), intervalMs, [window])
  const { data: categories } = usePolling(() => getServiceCategoriesWindow(window), intervalMs, [window])

  return (
    <>
      {!isFullscreen && (
        <div className="hdr">
          <WindowSelector value={window} onChange={setWindow} />
        </div>
      )}

      <div className="main" key={String(isFullscreen)}>
        <div className="col">
          <Overview report={report} />
          <FlowRateChart flowRate={flowRate} />
          <CountriesRanking geo={geo} />
        </div>

        <div className="col" style={{ flex: 1.6 }}>
          <TrendChart timeseries={timeseries} />
          <GeoPanel geo={geo} topology={topology} topFlows={report?.topFlows ?? []} />
        </div>

        <div className="col">
          <IPsRanking items={report?.topIPs ?? []} />
          <DomainsRanking items={report?.topDomains ?? []} />
        </div>
      </div>

      <div className="bot bot-grid">
        <FlowsTable report={report} />
        <ScanRadar scanAlerts={report?.scanAlerts} />
        <CategoriesRanking items={categories?.categories ?? []} />
      </div>
    </>
  )
}
