
import * as echarts from 'echarts/core'
import type { ECharts } from 'echarts/core'
import { EffectScatterChart, GraphChart, LineChart, LinesChart, MapChart, PieChart, RadarChart } from 'echarts/charts'
import { GeoComponent, GridComponent, LegendComponent, RadarComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([
  LineChart,
  PieChart,
  GraphChart,
  MapChart,
  LinesChart,
  EffectScatterChart,
  RadarChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  GeoComponent,
  RadarComponent,
  CanvasRenderer,
])

export function guardZeroSizePaint(chart: ECharts, el: HTMLElement): void {
  const hasSize = () => el.clientWidth > 0 && el.clientHeight > 0
  const setOption = chart.setOption.bind(chart)
  const resize = chart.resize.bind(chart)
  chart.setOption = ((...args: Parameters<typeof setOption>) => {
    if (!hasSize()) return
    return setOption(...args)
  }) as typeof chart.setOption
  chart.resize = ((...args: Parameters<typeof resize>) => {
    if (!hasSize()) return
    return resize(...args)
  }) as typeof chart.resize
}

export default echarts
