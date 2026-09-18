import { useEffect, useMemo, useRef } from 'react'
import { useT } from '../../i18n/context'
import { useEchart } from '../../hooks/useEchart'
import { useAnimatedNumber } from '../../hooks/useAnimatedNumber'
import type { FlowRate } from '../../api/types'

function MetricValue({ text }: { text: string }) {
  const match = text.match(/^([\d.,]+)\s*(.*)$/)
  return <strong><b>{match?.[1] ?? text}</b>{match?.[2] && <i>{match[2]}</i>}</strong>
}

export function FlowRateChart({ flowRate }: { flowRate: FlowRate | null }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  const points = useMemo(() => flowRate?.points ?? [], [flowRate])
  const latest = points.length ? points[points.length - 1].perSec : 0
  const animatedLatest = useAnimatedNumber(latest, (value) => value.toFixed(1) + t('flowRateNowSuffix'))

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const xData = points.map((p) => new Date(p.time).toLocaleTimeString('zh-CN', { hour12: false }))
    const yData = points.map((p) => Math.round(p.perSec * 10) / 10)
    chart.setOption(
      {
        backgroundColor: 'transparent',
        grid: { left: 4, right: 4, top: 54, bottom: 4 },
        xAxis: { type: 'category', show: false, data: xData },
        yAxis: { type: 'value', show: false, min: 0 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea', fontSize: 11 },
          formatter: (params: unknown) => {
            const p = params as { axisValue: string; data: number }[]
            return `${p[0].axisValue}<br/>${p[0].data} flows/s`
          },
        },
        series: [
          {
            type: 'line',
            data: yData,
            smooth: true,
            symbol: 'none',
            lineStyle: { width: 1.5, color: '#4FD0C0' },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: 'rgba(79,208,192,.35)' },
                  { offset: 1, color: 'rgba(79,208,192,0)' },
                ],
              },
            },
          },
        ],
      },
      false,
    )
  }, [chartRef, points])

  return (
    <div className="panel">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('flowRateTitle')}</span>
        </h2>
      </div>
      <div className="chart-metric-stage">
        <div ref={divRef} className="flowrate-canvas" />
        <div className="chart-metric chart-metric-connection"><MetricValue text={animatedLatest} /></div>
      </div>
    </div>
  )
}
