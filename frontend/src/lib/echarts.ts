
import * as echarts from 'echarts/core'
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

export default echarts
