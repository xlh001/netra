import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../../i18n/context'
import echarts, { guardZeroSizePaint } from '../../lib/echarts'
import { COUNTRY_CENTROIDS, aggregateByCountry, stableVisualBytes, type CountryTotal } from '../../lib/geo'
import { countryName, flagIconSrc, formatBytes } from '../../lib/format'
import type { FlowStat, GeoReport, Topology, TopologyNode } from '../../api/types'

const MIP_CAROUSEL_TOP_N = 8
const TIP_CAROUSEL_TOP_N = 8
const TOPO_LABEL_TOP_N = 28
const MAP_POINT_TOP_N = 12
const SERVER_MIN_DEGREE = 3
const CARD_ROTATE_MS = 4000
// Ambient "bumper-ball" drift: on each tick every node takes a bounded
// random-walk step, tweened smoothly, so the whole graph gently jostles (many
// places moving at once) rather than sitting like a static picture.
const TOPO_DRIFT_TICK_MS = 1500
const TOPO_DRIFT_AMP = 20

// Node positions persist across menu switches (component remounts) so that
// re-entering the dashboard shows the topology already laid out instead of
// re-converging from a wide spread every time. Nodes are seeded from these
// (not pinned), so the force layout still nudges them slightly on each refresh
// -- a live, breathing graph rather than a frozen image.
const persistedTopoPos: Record<string, { x: number; y: number }> = {}

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

export function GeoPanel({
  geo,
  topology,
  topFlows,
  hero,
  viewMode = 'auto',
  switchIntervalSec = 25,
}: {
  geo: GeoReport | null
  topology: Topology | null
  topFlows: FlowStat[]
  hero?: boolean
  viewMode?: 'auto' | 'world' | 'topo'
  switchIntervalSec?: number
}) {
  const { t, language } = useI18n()

  const mapDivRef = useRef<HTMLDivElement>(null)
  const topoDivRef = useRef<HTMLDivElement>(null)
  const mapChartRef = useRef<echarts.ECharts | null>(null)
  const topoChartRef = useRef<echarts.ECharts | null>(null)

  const [worldMapReady, setWorldMapReady] = useState(false)
  const [mode, setMode] = useState<'map' | 'topo'>('map')
  const viewModeRef = useRef(viewMode)
  viewModeRef.current = viewMode
  const modeRef = useRef(mode)
  modeRef.current = mode
  const lastTopoSigRef = useRef('')
  // Per-node tiny offsets for the gentle "one leaf twitches" liveness, and a
  // flag so the finished handler only persists BASE positions from real
  // layouts (never the jittered ones -- otherwise the base would slowly drift).
  const driftRef = useRef<Record<string, { dx: number; dy: number }>>({})
  const structuralRenderRef = useRef(true)
  const settledRef = useRef(false)

  const geoComponentAppliedRef = useRef(false)
  const visualBytesCacheRef = useRef(new Map<string, { bytes: number; ts: number }>())
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
    guardZeroSizePaint(mapChart, mapEl)
    guardZeroSizePaint(topoChart, topoEl)
    mapChartRef.current = mapChart
    topoChartRef.current = topoChart

    topoChart.on('finished', () => {
      type InternalChart = { getModel(): { getSeriesByIndex(i: number): { getData(): { count(): number; getItemLayout(i: number): number[]; getId(i: number): string } } | undefined } }
      const model = (topoChart as unknown as InternalChart).getModel()
      const series = model?.getSeriesByIndex(0)
      const data = series?.getData()
      if (!data) return
      // Skip persisting positions produced by a drift stir -- those include the
      // per-node offsets and would let the base positions creep.
      if (!structuralRenderRef.current) return
      let maxDelta = 0
      let count = 0
      for (let i = 0; i < data.count(); i++) {
        const layout = data.getItemLayout(i)
        if (layout && typeof layout[0] === 'number' && typeof layout[1] === 'number') {
          const id = data.getId(i)
          const prev = persistedTopoPos[id]
          if (prev) maxDelta = Math.max(maxDelta, Math.hypot(layout[0] - prev.x, layout[1] - prev.y))
          persistedTopoPos[id] = { x: layout[0], y: layout[1] }
          count++
        }
      }
      // The force layout is "settled" only once consecutive finished frames
      // barely move; the drift (pin + tween) must not start before then, or it
      // would freeze half-converged positions and jump.
      if (count > 0 && maxDelta < 1.5) settledRef.current = true
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
    if (viewModeRef.current !== 'auto') return
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

    if (!geo) return

    if (!geo.enabled) {
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

      const mapCountries = countries.slice(0, MAP_POINT_TOP_N)
      const mapPointData = mapCountries.map((c) => {
        const centroid = COUNTRY_CENTROIDS[c.country]
        const lng = centroid ? centroid[0] : 0
        const lat = centroid ? centroid[1] : 0
        return {
          id: c.country,
          name: countryName(c.country, language),
          value: [lng, lat],
          country: c.country,
          code: (c.country || '--').toUpperCase(),
          bytes: c.bytes,
          packets: c.packets,
          ipCount: c.ipCount,
          topIP: c.topIP,
          sizeQ: Math.round(Math.sqrt(stableByCountry[c.country] / maxBytes) * 20) / 20,
        }
      })
      const heatRegions = mapCountries.map((c) => {
        const intensity = Math.sqrt(stableByCountry[c.country] / maxBytes)
        return {
          name: countryName(c.country, 'en'),
          itemStyle: {
            areaColor: `rgba(188, 131, 60, ${0.34 + intensity * 0.34})`,
            borderColor: `rgba(232, 185, 105, ${0.32 + intensity * 0.34})`,
          },
        }
      })

      const mapOption: Record<string, unknown> = {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: 'rgba(8,15,24,0.92)',
          borderColor: 'rgba(201,163,91,0.3)',
          textStyle: { color: '#ECE6D6' },
          formatter: (p: { data?: { country: string; bytes: number; packets: number; ipCount: number } }) => {
            if (!p.data) return ''
            const flagTag = p.data.country ? `<img src="${flagIconSrc(p.data.country)}" style="width:14px;height:10px;vertical-align:middle;margin-right:4px;border-radius:1px;">` : ''
            return `${flagTag}${countryName(p.data.country, language)}<br/>${formatBytes(p.data.bytes)}, ${p.data.packets.toLocaleString()}${t('packetsSuffix')}<br/>${p.data.ipCount} IP`
          },
        },
        series: [
          {
            name: 'Traffic footprint',
            type: 'effectScatter',
            coordinateSystem: 'geo',
            zlevel: 2,
            symbolSize: (_val: unknown, params: { data: { sizeQ: number } }) => 26 + params.data.sizeQ * 34,
            showEffectOn: 'render',
            rippleEffect: { brushType: 'fill', scale: 1.55, period: 6 },
            itemStyle: { color: 'rgba(232,168,75,0.11)', shadowBlur: 30, shadowColor: 'rgba(232,168,75,0.36)' },
            label: { show: false },
            emphasis: { disabled: true },
            data: mapPointData,
          },
          {
            name: t('geoSeriesCountryTraffic'),
            type: 'effectScatter',
            coordinateSystem: 'geo',
            zlevel: 3,
            symbolSize: (_val: unknown, params: { data: { sizeQ: number } }) => 16 + params.data.sizeQ * 26,
            showEffectOn: 'render',
            rippleEffect: { brushType: 'stroke', scale: 5, period: 4.5 },
            itemStyle: { color: '#FF5D46', shadowBlur: 18, shadowColor: 'rgba(255,93,70,0.8)' },
            label: {
              show: true,
              position: 'right',
              distance: 10,
              formatter: (p: { data: { code: string } }) => p.data.code,
              color: '#F2A99C',
              fontFamily: 'ui-monospace, "SF Mono", monospace',
              fontSize: 16,
              fontWeight: 700,
            },
            labelLayout: { hideOverlap: true },
            emphasis: { disabled: true },
            data: mapPointData,
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
          itemStyle: { areaColor: '#16293C', borderColor: 'rgba(201,163,91,0.3)' },
          emphasis: { itemStyle: { areaColor: '#1D3348' }, label: { show: false } },
          regions: heatRegions,
        }
        geoComponentAppliedRef.current = true
      } else {
        mapOption.geo = { regions: heatRegions }
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

  }, [geo, worldMapReady, topFlows, language])

  function applyTopologyToChart(topo: Topology | null, force = false, stir = false) {
    const topoChart = topoChartRef.current
    if (!topoChart) return
    // topo === null means the poll hasn't returned yet: keep whatever is on
    // screen instead of flashing the "no data" placeholder while loading.
    if (!topo) return
    const nodes = topo.nodes ?? []
    const edges = topo.edges ?? []

    if (!nodes.length) {
      setTopoEmpty(true)
      lastTopoSigRef.current = ''
      return
    }
    setTopoEmpty(false)

    // Relayout only when the host/edge set actually changes, or when the gentle
    // "live" timer stirs it (force). This decouples the graph's motion from the
    // fast data poll so it stops jerking on every refresh -- the poll still
    // updates the side cards and (on structural change) sizes.
    const sig =
      nodes.map((n) => n.ip).sort().join(',') +
      '|' +
      edges.map((e) => `${e.src}>${e.dst}`).sort().join(',')
    if (!force && sig === lastTopoSigRef.current) return
    lastTopoSigRef.current = sig
    structuralRenderRef.current = !stir
    // A structural (re)layout must re-settle before the drift may pin/tween.
    if (!stir) settledRef.current = false

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

    const neighbors: Record<string, Set<string>> = {}
    edges.forEach((e) => {
      ;(neighbors[e.src] = neighbors[e.src] || new Set()).add(e.dst)
      ;(neighbors[e.dst] = neighbors[e.dst] || new Set()).add(e.src)
    })
    const degreeOf = (ip: string) => neighbors[ip]?.size ?? 0
    const hotCount = Math.min(12, Math.max(1, Math.ceil(nodes.length * 0.35)))
    const hotIPs = new Set(nodes.slice().sort((a, b) => stableByNode[b.ip] - stableByNode[a.ip]).slice(0, hotCount).map((n) => n.ip))
    nodes.forEach((n) => {
      if (degreeOf(n.ip) >= SERVER_MIN_DEGREE) hotIPs.add(n.ip)
    })
    const isServerNode = (n: TopologyNode) => hotIPs.has(n.ip)

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
        // Long, linear position tween so the gentle drift interpolates
        // smoothly between ticks instead of stepping.
        animationDurationUpdate: TOPO_DRIFT_TICK_MS,
        animationEasingUpdate: 'linear',
        tooltip: {
          trigger: 'item',
          backgroundColor: 'rgba(8,15,24,0.92)',
          borderColor: 'rgba(201,163,91,0.3)',
          textStyle: { color: '#ECE6D6' },
          formatter: (p: { dataType?: string; data: { source?: string; target?: string; name?: string; businessLabel?: string; bytesRaw: number; packetsRaw: number; degreeRaw?: number; isServer?: boolean } }) => {
            if (p.dataType === 'edge') {
              return `${p.data.source} ↔ ${p.data.target}<br/>${formatBytes(p.data.bytesRaw)}, ${p.data.packetsRaw.toLocaleString()}${t('packetsSuffix')}`
            }
            const roleTag = p.data.isServer ? `<span style="color:#F0C674;font-weight:700">● ${t('topoRoleServer')}</span> ` : ''
            const title = p.data.businessLabel
              ? `<span style="color:#4FD0C0;font-weight:600">${p.data.businessLabel}</span><span style="color:#8b93a0"> · </span>${p.data.name}`
              : p.data.name
            const degreeLine = p.data.degreeRaw != null ? `<br/>${t('tipDegreeLbl')} ${p.data.degreeRaw}` : ''
            return `${roleTag}${title}<br/>${formatBytes(p.data.bytesRaw)}, ${p.data.packetsRaw.toLocaleString()}${t('packetsSuffix')}${degreeLine}`
          },
        },
        series: [
          {
            type: 'graph',
            // Drift renders use 'none' (pure x/y placement + tween) so the
            // graph never re-runs the force simulation and never re-converges;
            // only the initial/structural layout uses 'force'.
            layout: stir ? 'none' : 'force',
            roam: true,
            draggable: false,
            // Center the graph. Only when the rotating info card is actually
            // shown (there are nodes -> the .mip tip card overlays the
            // bottom-left corner) do we nudge the settling point gently up/right
            // to clear it; otherwise sit dead-center.
            center: nodes.length > 0 ? ['54%', '46%'] : ['50%', '50%'],
            force: { repulsion, edgeLength: edgeLen, gravity: 0.08, friction: 0.5 },
            symbolSize: (_val: unknown, params: { data: { sizeQ: number; isServer?: boolean } }) =>
              params.data.isServer ? 16 + params.data.sizeQ * 22 : 6 + params.data.sizeQ * 8,
            itemStyle: { color: '#4FD0C0', shadowBlur: 8, shadowColor: 'rgba(79,208,192,0.7)', borderColor: 'rgba(201,163,91,0.7)', borderWidth: 1 },
            label: { show: true, position: 'bottom', color: '#8A9BAB', fontSize: 9.5, fontFamily: 'ui-monospace, monospace' },
            labelLayout: { hideOverlap: true },
            emphasis: { disabled: true },
            lineStyle: { color: '#4FD0C0', curveness: 0.12 },
            data: nodes.map((n) => {
              const base = persistedTopoPos[n.ip]
              const srv = isServerNode(n)
              const item: Record<string, unknown> = {
                id: n.ip,
                name: n.ip,
                businessLabel: n.label || '',
                bytesRaw: n.bytes,
                packetsRaw: n.packets,
                degreeRaw: degreeOf(n.ip),
                isServer: srv,
                sizeQ: Math.round(Math.sqrt(stableByNode[n.ip] / maxBytes) * 20) / 20,
                itemStyle: srv
                  ? { color: '#E8A84B', borderColor: '#F0C674', borderWidth: 1.5, shadowBlur: 14, shadowColor: 'rgba(232,168,75,0.7)' }
                  : { color: '#4FD0C0', borderColor: 'rgba(201,163,91,0.5)', borderWidth: 1, shadowBlur: 6, shadowColor: 'rgba(79,208,192,0.55)' },
                label: { show: srv || labeledIPs.has(n.ip), color: srv ? '#F0C674' : '#8A9BAB', fontWeight: srv ? 700 : 400, formatter: () => n.ip },
              }
              if (base) {
                // Pin nodes at their settled base position (+ a tiny per-node
                // jitter offset when the live timer is nudging this one). Pinning
                // means updates are smooth position tweens, never a whole-graph
                // force re-converge -- so entry is already-formed and the liveness
                // is just the odd leaf drifting, not the entire topology pulsing.
                const j = driftRef.current[n.ip]
                item.x = base.x + (j ? j.dx : 0)
                item.y = base.y + (j ? j.dy : 0)
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
                lineStyle: { width: 0.65 + v * 2, opacity: 0.12 + v * 0.28 },
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

  // Gentle liveness: every tick, all nodes take a small bounded random-walk
  // step (base position + a drifting offset), so multiple places float softly
  // at once. Skipped until the initial layout has settled and every node has a
  // cached base position -- otherwise a stir would re-run the force layout on
  // not-yet-pinned nodes and cause a second full converge a few seconds in.
  useEffect(() => {
    const id = setInterval(() => {
      if (modeRef.current !== 'topo') return
      if (!settledRef.current) return // wait until the force layout has settled
      const topo = lastTopoRef.current
      const ns = topo?.nodes ?? []
      if (!ns.length) return
      for (const n of ns) {
        if (!persistedTopoPos[n.ip]) return // base not ready yet -> hold still
      }
      const d = driftRef.current
      const clamp = (v: number) => (v > TOPO_DRIFT_AMP ? TOPO_DRIFT_AMP : v < -TOPO_DRIFT_AMP ? -TOPO_DRIFT_AMP : v)
      for (const n of ns) {
        const cur = d[n.ip] ?? { dx: 0, dy: 0 }
        cur.dx = clamp((cur.dx + (Math.random() - 0.5) * 14) * 0.88)
        cur.dy = clamp((cur.dy + (Math.random() - 0.5) * 14) * 0.88)
        d[n.ip] = cur
      }
      applyTopologyToChart(topo, true, true)
    }, TOPO_DRIFT_TICK_MS)
    return () => clearInterval(id)
  }, [])

  // Fixed view modes ("world"/"topo") pin the panel; only "auto" alternates.
  useEffect(() => {
    if (viewMode === 'world') setMode('map')
    else if (viewMode === 'topo') setMode('topo')
  }, [viewMode])

  useEffect(() => {
    if (viewMode !== 'auto') return
    const id = setInterval(() => {
      setMode((cur) => {
        const next = cur === 'map' ? 'topo' : 'map'
        if (next === 'topo' && !lastTopoHasNodesRef.current) return cur
        if (next === 'map' && !(lastGeoEnabledRef.current && lastMapHasTrafficRef.current)) return cur
        return next
      })
    }, Math.max(1, switchIntervalSec) * 1000)
    return () => clearInterval(id)
  }, [viewMode, switchIntervalSec])

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
    <div className={hero ? 'geo-hero' : 'panel flex1'}>
      {!hero && (
        <div className="panel-head">
          <h2>
            <span className="panel-head-title">{t(showMap ? 'geoTitle' : 'topoTitle')}</span>
          </h2>
        </div>
      )}
      <div className="map-wrap">
        <div ref={mapDivRef} id="map-chart" className={showMap ? '' : 'map-backdrop'} style={{ visibility: mapEmpty ? 'hidden' : 'visible' }} />
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
                      {' ' + countryName(mipCountry.country, language)}
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

function svcClass(service?: string, proto?: string) {
  const s = (service || proto || '').toLowerCase()
  if (s.includes('ssh')) return 'svc ssh'
  if (s.includes('mysql')) return 'svc mysql'
  if (s.includes('http')) return 'svc http'
  return 'svc'
}

function FlowsList({ entries, emptyText }: { entries: FlowEntry[]; emptyText: string }) {
  if (!entries.length) return <div className="mip-flows"><div className="none">{emptyText}</div></div>
  return (
    <div className="mip-flows">
      {entries.map((f, i) => (
        <div key={i}>
          {'↔ '}
          <span className="peer">{f.peer}</span>
          {'  '}
          <span className={svcClass(f.service, f.proto)}>{`${(f.proto || '').toUpperCase()}/${f.port}${f.service ? ' (' + f.service + ')' : ''}`}</span>
          {'  '}
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
