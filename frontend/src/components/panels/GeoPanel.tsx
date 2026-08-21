import { useEffect, useRef, useState } from 'react'
import { useT } from '../../i18n/context'
import echarts from '../../lib/echarts'
import { COUNTRY_CENTROIDS, MAP_HUB, aggregateByCountry, stableVisualBytes, type CountryTotal } from '../../lib/geo'
import { countryName, flagIconSrc, formatBytes } from '../../lib/format'
import type { FlowStat, GeoReport, Topology, TopologyNode } from '../../api/types'

const MIP_CAROUSEL_TOP_N = 8
const TIP_CAROUSEL_TOP_N = 8
const TOPO_LABEL_TOP_N = 20
const CARD_ROTATE_MS = 4000
const GEO_ROTATE_MS = 25000

interface FlowEntry {
  peer: string
  port: number
  proto: string
  service?: string
  bytes: number
}

interface PeerEntry {
  peer: string
  bytes: number
  packets: number
}

export function GeoPanel({ geo, topology, topFlows }: { geo: GeoReport | null; topology: Topology | null; topFlows: FlowStat[] }) {
  const t = useT()

  const mapDivRef = useRef<HTMLDivElement>(null)
  const topoDivRef = useRef<HTMLDivElement>(null)
  const mapChartRef = useRef<echarts.ECharts | null>(null)
  const topoChartRef = useRef<echarts.ECharts | null>(null)

  const [worldMapReady, setWorldMapReady] = useState(false)
  const [mode, setMode] = useState<'map' | 'topo'>('map')

  const geoComponentAppliedRef = useRef(false)
  const visualBytesCacheRef = useRef(new Map<string, { bytes: number; ts: number }>())
  const topoNodePosRef = useRef<Record<string, { x: number; y: number }>>({})
  const lastGeoEnabledRef = useRef(true)
  const lastMapHasTrafficRef = useRef(true)
  const lastTopoHasNodesRef = useRef(true)
  const lastTopoRef = useRef<Topology | null>(null)
  const lastGeoReportRef = useRef<GeoReport | null>(null)

  const [mapEmpty, setMapEmpty] = useState<'disabled' | 'no-traffic' | null>(null)
  const [topoEmpty, setTopoEmpty] = useState(false)

  const [mipPoints, setMipPoints] = useState<CountryTotal[]>([])
  const [mipFlowsByCountry, setMipFlowsByCountry] = useState<Record<string, FlowEntry[]>>({})
  const [mipIdx, setMipIdx] = useState(0)
  const mipPointsRef = useRef<CountryTotal[]>([])
  mipPointsRef.current = mipPoints

  const [tipPoints, setTipPoints] = useState<TopologyNode[]>([])
  const [tipPeersByIP, setTipPeersByIP] = useState<Record<string, PeerEntry[]>>({})
  const [tipIdx, setTipIdx] = useState(0)
  const tipPointsRef = useRef<TopologyNode[]>([])
  tipPointsRef.current = tipPoints

  useEffect(() => {
    const mapEl = mapDivRef.current
    const topoEl = topoDivRef.current
    if (!mapEl || !topoEl) return
    const mapChart = echarts.init(mapEl, null, { renderer: 'canvas' })
    const topoChart = echarts.init(topoEl, null, { renderer: 'canvas' })
    mapChartRef.current = mapChart
    topoChartRef.current = topoChart

    topoChart.on('finished', () => {
      type InternalChart = { getModel(): { getSeriesByIndex(i: number): { getData(): { count(): number; getItemLayout(i: number): number[]; getId(i: number): string } } | undefined } }
      const model = (topoChart as unknown as InternalChart).getModel()
      const series = model?.getSeriesByIndex(0)
      const data = series?.getData()
      if (!data) return
      for (let i = 0; i < data.count(); i++) {
        const layout = data.getItemLayout(i)
        if (layout && typeof layout[0] === 'number' && typeof layout[1] === 'number') {
          topoNodePosRef.current[data.getId(i)] = { x: layout[0], y: layout[1] }
        }
      }
    })

    const onResize = () => {
      mapChart.resize()
      topoChart.resize()
    }
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      mapChart.dispose()
      topoChart.dispose()
    }
  }, [])

  useEffect(() => {
    fetch('/vendor/world.json')
      .then((r) => r.json())
      .then((geoJson) => {
        echarts.registerMap('world', geoJson)
        setWorldMapReady(true)
      })
      .catch(() => {
      })
  }, [])

  function reconcileMode() {
    setMode((cur) => {
      const mapHasSomething = lastGeoEnabledRef.current && lastMapHasTrafficRef.current
      if (cur === 'map' && !mapHasSomething && lastTopoHasNodesRef.current) return 'topo'
      if (cur === 'topo' && !lastTopoHasNodesRef.current && mapHasSomething) return 'map'
      return cur
    })
  }

  useEffect(() => {
    const mapChart = mapChartRef.current
    if (!mapChart) return
    lastGeoReportRef.current = geo

    if (!geo || !geo.enabled) {
      lastGeoEnabledRef.current = false
      lastMapHasTrafficRef.current = false
      setMapEmpty('disabled')
      reconcileMode()
      return
    }
    lastGeoEnabledRef.current = true

    const countries = aggregateByCountry(geo.points)
    lastMapHasTrafficRef.current = countries.length > 0
    setMapEmpty(countries.length > 0 ? null : 'no-traffic')

    if (worldMapReady) {
      const cache = visualBytesCacheRef.current
      const stableByCountry: Record<string, number> = {}
      countries.forEach((c) => {
        stableByCountry[c.country] = stableVisualBytes(cache, c.bytes, c.country)
      })
      const maxBytes = countries.reduce((m, c) => Math.max(m, stableByCountry[c.country]), 0) || 1
      const minBytes = countries.reduce((m, c) => Math.min(m, stableByCountry[c.country]), maxBytes) || 1
      const norm = (v: number) => {
        const lo = Math.log(minBytes)
        const hi = Math.log(maxBytes)
        const x = Math.log(v || 1)
        const raw = hi === lo ? 0.5 : Math.max(0, Math.min(1, (x - lo) / (hi - lo)))

        return Math.round(raw * 20) / 20
      }

      const mapOption: Record<string, unknown> = {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea' },
          formatter: (p: { data?: { country: string; bytes: number; packets: number; ipCount: number } }) => {
            if (!p.data) return ''
            const flagTag = p.data.country ? `<img src="${flagIconSrc(p.data.country)}" style="width:14px;height:10px;vertical-align:middle;margin-right:4px;border-radius:1px;">` : ''
            return `${flagTag}${countryName(p.data.country)}<br/>${formatBytes(p.data.bytes)}, ${p.data.packets.toLocaleString()}${t('packetsSuffix')}<br/>${p.data.ipCount} IP`
          },
        },
        series: [
          {
            name: '流量方向',
            type: 'lines',
            coordinateSystem: 'geo',
            zlevel: 1,
            effect: { show: true, trailLength: 0.22, symbol: 'circle', color: '#ffe6f5' },
            lineStyle: { color: '#35e0ff', curveness: 0.2 },
            data: countries.map((c) => {
              const centroid = COUNTRY_CENTROIDS[c.country]
              const lng = centroid ? centroid[0] : 0
              const lat = centroid ? centroid[1] : 0
              const v = norm(stableByCountry[c.country])
              const westOfHub = lng < MAP_HUB[0]
              const gradStops = westOfHub
                ? [{ offset: 0, color: '#35e0ff' }, { offset: 1, color: '#ff2e88' }]
                : [{ offset: 0, color: '#ff2e88' }, { offset: 1, color: '#35e0ff' }]
              return {
                id: c.country,
                coords: [MAP_HUB, [lng, lat]],
                lineStyle: {
                  color: new echarts.graphic.LinearGradient(0, 0, 1, 0, gradStops),

                  width: 0.8 + v * 1.8,
                  opacity: 0.15 + v * 0.2,
                  curveness: c.country.charCodeAt(0) % 2 === 0 ? 0.2 : -0.2,
                },
                effect: { period: 2.4 - v * 1.2, symbolSize: 3 + v * 3 },
              }
            }),
          },
          {
            name: '国家流量',
            type: 'effectScatter',
            coordinateSystem: 'geo',
            symbolSize: (_val: unknown, params: { data: { sizeQ: number } }) => 4 + params.data.sizeQ * 22,
            showEffectOn: 'render',
            rippleEffect: { brushType: 'stroke' },
            itemStyle: { color: '#35e0ff', shadowBlur: 8, shadowColor: '#35e0ff' },
            label: { show: false },

            labelLayout: { hideOverlap: true },
            emphasis: { disabled: true },
            data: countries.map((c) => {
              const centroid = COUNTRY_CENTROIDS[c.country]
              const lng = centroid ? centroid[0] : 0
              const lat = centroid ? centroid[1] : 0
              return {
                id: c.country,
                name: countryName(c.country),
                value: [lng, lat],
                country: c.country,
                bytes: c.bytes,
                packets: c.packets,
                ipCount: c.ipCount,
                topIP: c.topIP,
                sizeQ: Math.round(Math.sqrt(stableByCountry[c.country] / maxBytes) * 20) / 20,
                label: {
                  show: true,
                  formatter: '{flag|}',
                  position: 'right',
                  distance: 6,
                  rich: { flag: { height: 12, width: 16, backgroundColor: { image: flagIconSrc(c.country) } } },
                },
              }
            }),
          },
          {
            name: '汇聚点',
            type: 'effectScatter',
            coordinateSystem: 'geo',
            silent: true,
            tooltip: { show: false },
            symbol: 'circle',
            symbolSize: 12,
            showEffectOn: 'render',
            rippleEffect: { period: 3, scale: 4, brushType: 'stroke' },
            itemStyle: { color: 'rgba(255,207,92,0.35)', borderColor: 'rgba(255,207,92,0.8)', borderWidth: 1.5, shadowBlur: 8, shadowColor: 'rgba(255,207,92,0.6)' },
            data: [{ value: MAP_HUB }],
          },
        ],
      }

      if (!geoComponentAppliedRef.current) {
        mapOption.geo = {
          map: 'world',
          roam: true,
          zoom: 1.3,
          center: [10, 15],
          top: 6,
          bottom: 6,
          left: 6,
          right: 6,
          itemStyle: { areaColor: '#131720', borderColor: '#3d4250' },
          emphasis: { itemStyle: { areaColor: '#132a45' }, label: { show: false } },
        }
        geoComponentAppliedRef.current = true
      }
      mapChart.setOption(mapOption, false)
    }

    const mipCapped = countries.slice(0, MIP_CAROUSEL_TOP_N)
    const byCountry: Record<string, FlowEntry[]> = {}
    topFlows.forEach((f) => {
      if (f.srcCountry) {
        ;(byCountry[f.srcCountry] = byCountry[f.srcCountry] || []).push({ peer: f.dstIP, port: f.dstPort, proto: f.proto, service: f.service, bytes: f.bytes })
      }
      if (f.dstCountry) {
        ;(byCountry[f.dstCountry] = byCountry[f.dstCountry] || []).push({ peer: f.srcIP, port: f.dstPort, proto: f.proto, service: f.service, bytes: f.bytes })
      }
    })
    setMipPoints(mipCapped)
    setMipFlowsByCountry(byCountry)
    setMipIdx((i) => (i >= mipCapped.length ? 0 : i))

    reconcileMode()

  }, [geo, worldMapReady, topFlows])

  function applyTopologyToChart(topo: Topology | null) {
    const topoChart = topoChartRef.current
    if (!topoChart) return
    const nodes = topo?.nodes ?? []
    const edges = topo?.edges ?? []

    if (!nodes.length) {
      setTopoEmpty(true)
      return
    }
    setTopoEmpty(false)

    const cache = visualBytesCacheRef.current
    const stableByNode: Record<string, number> = {}
    nodes.forEach((n) => {
      stableByNode[n.ip] = stableVisualBytes(cache, n.bytes || 0, n.ip)
    })
    const stableByEdge: Record<string, number> = {}
    edges.forEach((e) => {
      stableByEdge[`${e.src}->${e.dst}`] = stableVisualBytes(cache, e.bytes, `${e.src}->${e.dst}`)
    })
    const maxBytes = nodes.reduce((m, n) => Math.max(m, stableByNode[n.ip]), 0) || 1
    const maxEdgeBytes = edges.reduce((m, e) => Math.max(m, stableByEdge[`${e.src}->${e.dst}`]), 0) || 1

    const chartDom = topoDivRef.current
    const boxW = chartDom?.clientWidth || 400
    const boxH = chartDom?.clientHeight || 300
    const spread = Math.min(boxW, boxH)
    // Repulsion needs to grow with node count too, not just container size --
    // a fixed floor crowds nodes together (overlapping circles/labels) once
    // there are enough hosts to fill the space regardless of how big the
    // container is.
    const repulsion = Math.max(140, spread * 1.1, nodes.length * 9)
    const edgeLen: [number, number] = [Math.max(50, spread * 0.18), Math.max(130, spread * 0.6)]

    const labeledIPs = new Set(
      nodes
        .slice()
        .sort((a, b) => stableByNode[b.ip] - stableByNode[a.ip])
        .slice(0, TOPO_LABEL_TOP_N)
        .map((n) => n.ip),
    )

    topoChart.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea' },
          formatter: (p: { dataType?: string; data: { source?: string; target?: string; name?: string; businessLabel?: string; bytesRaw: number; packetsRaw: number } }) => {
            if (p.dataType === 'edge') {
              return `${p.data.source} ↔ ${p.data.target}<br/>${formatBytes(p.data.bytesRaw)}, ${p.data.packetsRaw.toLocaleString()}${t('packetsSuffix')}`
            }
            const title = p.data.businessLabel
              ? `<span style="color:#2ee6a8;font-weight:600">${p.data.businessLabel}</span><span style="color:#8b93a0"> · </span>${p.data.name}`
              : p.data.name
            return `${title}<br/>${formatBytes(p.data.bytesRaw)}, ${p.data.packetsRaw.toLocaleString()}${t('packetsSuffix')}`
          },
        },
        series: [
          {
            type: 'graph',
            layout: 'force',
            roam: true,
            draggable: false,
            // Nudged up/right of dead-center: the .mip info card overlays the
            // bottom-left corner of this chart, so bias the force layout's
            // settling point away from there instead of the true center.
            center: ['58%', '42%'],
            force: { repulsion, edgeLength: edgeLen, gravity: 0.08, friction: 0.5 },
            symbolSize: (_val: unknown, params: { data: { sizeQ: number } }) => 10 + params.data.sizeQ * 20,
            itemStyle: { color: '#35e0ff', shadowBlur: 8, shadowColor: '#35e0ff', borderColor: 'rgba(255,207,92,0.7)', borderWidth: 1 },
            label: { show: true, position: 'bottom', color: '#8b93a0', fontSize: 9.5, fontFamily: 'ui-monospace, monospace' },
            labelLayout: { hideOverlap: true },
            emphasis: { disabled: true },
            lineStyle: { color: '#35e0ff', curveness: 0.15 },
            effect: { show: true, trailLength: 0.4, symbol: 'circle', symbolSize: 5, color: '#ffe6f5', shadowBlur: 10, shadowColor: '#ffe6f5' },
            data: nodes.map((n) => {
              const pos = topoNodePosRef.current[n.ip]
              const item: Record<string, unknown> = {
                id: n.ip,
                name: n.ip,
                businessLabel: n.label || '',
                bytesRaw: n.bytes,
                packetsRaw: n.packets,
                sizeQ: Math.round(Math.sqrt(stableByNode[n.ip] / maxBytes) * 20) / 20,
                label: { show: labeledIPs.has(n.ip), formatter: () => n.label || n.ip },
              }
              if (pos) {
                item.x = pos.x
                item.y = pos.y
                item.fixed = true
              }
              return item
            }),
            edges: edges.map((e) => {
              const v = Math.round((stableByEdge[`${e.src}->${e.dst}`] / maxEdgeBytes) * 20) / 20
              return {
                source: e.src,
                target: e.dst,
                bytesRaw: e.bytes,
                packetsRaw: e.packets,
                lineStyle: { width: 0.8 + v * 2.5, opacity: 0.2 + v * 0.35 },
                effect: { period: 2.8 - v * 1.2 },
              }
            }),
          },
        ],
      },
      false,
    )
  }

  useEffect(() => {
    lastTopoRef.current = topology
    const nodes = topology?.nodes ?? []
    lastTopoHasNodesRef.current = nodes.length > 0

    const peersByIP: Record<string, PeerEntry[]> = {}
    ;(topology?.edges ?? []).forEach((e) => {
      ;(peersByIP[e.src] = peersByIP[e.src] || []).push({ peer: e.dst, bytes: e.bytes, packets: e.packets })
      ;(peersByIP[e.dst] = peersByIP[e.dst] || []).push({ peer: e.src, bytes: e.bytes, packets: e.packets })
    })
    const tipCapped = nodes.slice().sort((a, b) => b.bytes - a.bytes).slice(0, TIP_CAROUSEL_TOP_N)
    setTipPoints(tipCapped)
    setTipPeersByIP(peersByIP)
    setTipIdx((i) => (i >= tipCapped.length ? 0 : i))

    if (mode === 'topo') applyTopologyToChart(topology)

    reconcileMode()

  }, [topology])

  useEffect(() => {
    if (mode === 'topo') {
      applyTopologyToChart(lastTopoRef.current)
      topoChartRef.current?.resize()
    } else {
      mapChartRef.current?.resize()
    }

  }, [mode])

  useEffect(() => {
    const id = setInterval(() => {
      setMode((cur) => {
        const next = cur === 'map' ? 'topo' : 'map'
        if (next === 'topo' && !lastTopoHasNodesRef.current) return cur
        if (next === 'map' && !(lastGeoEnabledRef.current && lastMapHasTrafficRef.current)) return cur
        return next
      })
    }, GEO_ROTATE_MS)
    return () => clearInterval(id)
  }, [])

  useEffect(() => {
    const id = setInterval(() => {
      if (!mipPointsRef.current.length) return
      setMipIdx((i) => (i + 1) % mipPointsRef.current.length)
    }, CARD_ROTATE_MS)
    return () => clearInterval(id)
  }, [])
  useEffect(() => {
    const id = setInterval(() => {
      if (!tipPointsRef.current.length) return
      setTipIdx((i) => (i + 1) % tipPointsRef.current.length)
    }, CARD_ROTATE_MS)
    return () => clearInterval(id)
  }, [])

  const showMap = mode === 'map'
  const mipCountry = mipPoints[mipIdx]
  const tipNode = tipPoints[tipIdx]

  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t(showMap ? 'geoTitle' : 'topoTitle')}</span>
        </h2>
      </div>
      <div className="map-wrap">
        <div ref={mapDivRef} id="map-chart" style={{ display: showMap ? '' : 'none', visibility: mapEmpty ? 'hidden' : 'visible' }} />
        {showMap && mapEmpty === 'disabled' && (
          <div className="map-disabled">
            <div className="big">{t('mapDisabledBig')}</div>
            <div className="small">{t('mapDisabledSmall')}</div>
          </div>
        )}
        <div ref={topoDivRef} id="topo-chart" style={{ display: !showMap ? '' : 'none', visibility: !showMap && topoEmpty ? 'hidden' : 'visible' }} />
        {!showMap && topoEmpty && (
          <div className="map-disabled">
            <div className="big">{t('topoDisabledBig')}</div>
            <div className="small">{t('topoDisabledSmall')}</div>
          </div>
        )}

        <div className="mip" style={{ display: showMap && mipCountry ? 'block' : 'none' }}>
          {mipCountry && (
            <>
              <div className="mip-head">
                <div className="mip-dotmark" />
                <div className="mip-title">
                  {mipCountry.country ? (
                    <>
                      <img className="flag-icon" src={flagIconSrc(mipCountry.country)} alt={mipCountry.country} onError={(e) => (e.currentTarget.style.display = 'none')} />
                      {' ' + countryName(mipCountry.country)}
                    </>
                  ) : (
                    '--'
                  )}
                </div>
              </div>
              <div className="mip-grid">
                <span className="mip-lbl">{t('mipBytesLbl')}</span>
                <span className="mip-val">{formatBytes(mipCountry.bytes)}</span>
                <span className="mip-lbl">{t('mipPacketsLbl')}</span>
                <span className="mip-val">{mipCountry.packets.toLocaleString()}</span>
                <span className="mip-lbl">{t('mipIPCountLbl')}</span>
                <span className="mip-val">{mipCountry.ipCount.toLocaleString()}</span>
                <span className="mip-lbl">{t('mipTopIPLbl')}</span>
                <span className="mip-val">{mipCountry.topIP}</span>
                {mipCountry.topIPOrg && <div className="mip-org">{mipCountry.topIPOrg}</div>}
              </div>
              <div className="mip-flows-head">{t('mipFlowsHead')}</div>
              <FlowsList entries={(mipFlowsByCountry[mipCountry.country] || []).sort((a, b) => b.bytes - a.bytes).slice(0, 3)} emptyText={t('mipNoFlows')} />
              <ProgressBar durationMs={CARD_ROTATE_MS} restartKey={`${mode}:${mipIdx}`} />
              <Dots count={mipPoints.length} activeIdx={mipIdx} />
            </>
          )}
        </div>

        <div className="mip" style={{ display: !showMap && tipNode ? 'block' : 'none' }}>
          {tipNode && (
            <>
              <div className="mip-head">
                <div className="mip-dotmark" />
                <div className="mip-title">{tipNode.ip}</div>
              </div>
              <div className="mip-grid">
                <span className="mip-lbl">{t('mipBytesLbl')}</span>
                <span className="mip-val">{formatBytes(tipNode.bytes)}</span>
                <span className="mip-lbl">{t('mipPacketsLbl')}</span>
                <span className="mip-val">{tipNode.packets.toLocaleString()}</span>
                <span className="mip-lbl">{t('tipDegreeLbl')}</span>
                <span className="mip-val">{(tipPeersByIP[tipNode.ip]?.length ?? 0).toLocaleString()}</span>
              </div>
              <div className="mip-flows-head">{t('tipPeersHead')}</div>
              <PeersList
                entries={(tipPeersByIP[tipNode.ip] || []).slice().sort((a, b) => b.bytes - a.bytes).slice(0, 3)}
                emptyText={t('tipNoPeers')}
              />
              <ProgressBar durationMs={CARD_ROTATE_MS} restartKey={`${mode}:${tipIdx}`} />
              <Dots count={tipPoints.length} activeIdx={tipIdx} />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function FlowsList({ entries, emptyText }: { entries: FlowEntry[]; emptyText: string }) {
  if (!entries.length) return <div className="mip-flows"><div className="none">{emptyText}</div></div>
  return (
    <div className="mip-flows">
      {entries.map((f, i) => (
        <div key={i}>
          {'↔ '}
          <span className="peer">{f.peer}</span>
          {`  ${(f.proto || '').toUpperCase()}/${f.port}${f.service ? ' (' + f.service + ')' : ''}  `}
          <span className="bytes">{formatBytes(f.bytes)}</span>
        </div>
      ))}
    </div>
  )
}

function PeersList({ entries, emptyText }: { entries: PeerEntry[]; emptyText: string }) {
  if (!entries.length) return <div className="mip-flows"><div className="none">{emptyText}</div></div>
  return (
    <div className="mip-flows">
      {entries.map((p, i) => (
        <div key={i}>
          {'↔ '}
          <span className="peer">{p.peer}</span>
          <span className="bytes">{'  ' + formatBytes(p.bytes)}</span>
        </div>
      ))}
    </div>
  )
}

function Dots({ count, activeIdx }: { count: number; activeIdx: number }) {
  return (
    <div className="mip-dots">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className={'mip-dot' + (i === activeIdx ? ' on' : '')} />
      ))}
    </div>
  )
}

function ProgressBar({ durationMs, restartKey }: { durationMs: number; restartKey: string }) {
  const barRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const bar = barRef.current
    if (!bar) return
    bar.style.transition = 'none'
    bar.style.width = '0%'
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        bar.style.transition = `width ${durationMs}ms linear`
        bar.style.width = '100%'
      })
    })
  }, [restartKey, durationMs])
  return (
    <div className="mip-prog">
      <div className="mip-bar" ref={barRef} />
    </div>
  )
}
