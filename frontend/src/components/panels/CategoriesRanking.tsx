import { useEffect, useMemo, useRef } from 'react'
import { useT } from '../../i18n/context'
import { useEchart } from '../../hooks/useEchart'
import { categoryColor, formatBytes } from '../../lib/format'
import type { CategoryStat } from '../../api/types'

export function CategoriesRanking({ items }: { items: CategoryStat[] }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  const sorted = useMemo(() => [...items].sort((a, b) => b.bytes - a.bytes), [items])

  useEffect(() => {
    const chart = chartRef.current
    if (!chart || sorted.length === 0) return

    const colors = sorted.map((c, i) => categoryColor(c.category, i))
    const logValues = sorted.map((c) => Math.log10(Math.max(c.bytes, 1)))
    const axisMin = Math.max(0, Math.floor(Math.min(...logValues)) - 1)
    const axisMax = Math.ceil(Math.max(...logValues)) + 0.5
    const rich: Record<string, { color: string; fontSize: number; fontFamily: string }> = {}
    sorted.forEach((_, i) => {
      rich['c' + i] = { color: colors[i], fontSize: 10.5, fontFamily: 'ui-monospace, "SF Mono", Consolas, monospace' }
    })

    chart.setOption(
      {
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea', fontSize: 11 },
          formatter: () => sorted.map((c, i) => `<span style="color:${colors[i]}">●</span> ${c.category}: ${formatBytes(c.bytes)}`).join('<br/>'),
        },
        radar: {
          center: ['50%', '54%'],
          radius: '62%',
          axisName: {
            formatter: (name: string) => `{c${Math.max(0, sorted.findIndex((c) => c.category === name))}|${name}}`,
            rich,
          },
          axisNameGap: 10,
          splitNumber: 4,
          splitLine: { lineStyle: { color: 'rgba(150,160,180,0.14)' } },
          splitArea: { show: false },
          axisLine: { lineStyle: { color: 'rgba(150,160,180,0.14)' } },
          indicator: sorted.map((c) => ({ name: c.category, min: axisMin, max: axisMax })),
        },
        series: [
          {
            type: 'radar',
            data: [
              {
                value: logValues,
                lineStyle: { color: '#35e0ff', width: 1.5 },
                areaStyle: { color: 'rgba(53,224,255,0.14)' },
                itemStyle: { color: '#35e0ff' },
                symbolSize: 4,
              },
            ],
          },
        ],
      },
      false,
    )
  }, [chartRef, sorted])

  return (
    <div className="panel" style={{ height: '100%' }}>
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('categoryRankTitle')}</span>
        </h2>
      </div>
      <div style={{ flex: 1, minHeight: 0, position: 'relative' }}>
        <div ref={divRef} style={{ width: '100%', height: '100%' }} />
        {sorted.length === 0 && (
          <div className="empty" style={{ position: 'absolute', inset: 0 }}>
            {t('noData')}
          </div>
        )}
      </div>
    </div>
  )
}
