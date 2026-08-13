import { useEffect, useMemo, useRef } from 'react'
import { useT } from '../../i18n/context'
import { useEchart } from '../../hooks/useEchart'
import { formatBytes, protoColor } from '../../lib/format'
import type { Timeseries } from '../../api/types'

export function ProtocolPie({ timeseries }: { timeseries: Timeseries | null }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  const data = useMemo(() => {
    const totals = new Map<string, number>()
    for (const p of timeseries?.points ?? []) {
      for (const [proto, bytes] of Object.entries(p.bytes || {})) {
        totals.set(proto, (totals.get(proto) ?? 0) + bytes)
      }
    }
    return Array.from(totals.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([proto, bytes]) => ({ name: proto.toUpperCase(), value: bytes, itemStyle: { color: protoColor(proto) } }))
  }, [timeseries])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    chart.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea', fontSize: 11 },
          valueFormatter: (v: unknown) => formatBytes(v as number),
        },
        legend: {
          orient: 'vertical',
          right: 4,
          top: 'center',
          textStyle: { color: '#8b93a0', fontSize: 10.5 },
          itemWidth: 9,
          itemHeight: 9,

          formatter: (name: string) => {
            const total = data.reduce((sum, d) => sum + d.value, 0)
            const item = data.find((d) => d.name === name)
            const pct = total > 0 && item ? ((item.value / total) * 100).toFixed(1) : '0.0'
            return `${name}  ${pct}%`
          },
        },
        series: [
          {
            type: 'pie',
            radius: ['45%', '72%'],
            center: ['36%', '50%'],
            avoidLabelOverlap: true,
            label: { show: false },
            labelLine: { show: false },
            itemStyle: { borderColor: '#131720', borderWidth: 1 },
            data,
          },
        ],
      },
      false,
    )
  }, [chartRef, data])

  return (
    <div className="panel" style={{ flex: 1 }}>
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('protoShareTitle')}</span>
        </h2>
      </div>
      <div ref={divRef} style={{ flex: 1, minHeight: 0 }} />
    </div>
  )
}
