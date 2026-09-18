import { theme as antdTheme, type ThemeConfig } from 'antd'

export const tokens = {
  abyss: '#080F18',
  ocean: '#0B1626',
  land: '#16293C',
  panel: 'rgba(10,19,30,0.72)',
  panelSolid: '#0C1826',
  elevated: '#101E30',
  line: 'rgba(201,163,91,0.15)',
  lineBright: 'rgba(201,163,91,0.30)',
  parchment: '#ECE6D6',
  slate: '#8A9BAB',
  slateDim: '#546472',
  coral: '#FF5D46',
  teal: '#4FD0C0',
  amber: '#E8A84B',
  violet: '#9B8CFF',
  coast: '#C9A35B',
  fontUi:
    "'Segoe UI Variable','Segoe UI',-apple-system,BlinkMacSystemFont,system-ui,'Microsoft YaHei','PingFang SC',Roboto,sans-serif",
  fontMono: 'ui-monospace,"Cascadia Code","SF Mono","Consolas","Liberation Mono","Menlo",monospace',
} as const

export const netraAntdTheme: ThemeConfig = {
  algorithm: antdTheme.darkAlgorithm,
  token: {
    colorPrimary: tokens.teal,
    colorInfo: tokens.teal,
    colorSuccess: tokens.teal,
    colorWarning: tokens.amber,
    colorError: tokens.coral,
    colorBgBase: tokens.abyss,
    colorBgLayout: tokens.abyss,
    colorBgContainer: tokens.panelSolid,
    colorBgElevated: tokens.elevated,
    colorBorder: tokens.line,
    colorBorderSecondary: 'rgba(201,163,91,0.10)',
    colorText: tokens.parchment,
    colorTextSecondary: tokens.slate,
    colorTextTertiary: tokens.slateDim,
    fontFamily: tokens.fontUi,
    borderRadius: 10,
  },
  components: {
    Table: {
      headerBg: tokens.panelSolid,
      headerColor: tokens.slate,
      rowHoverBg: 'rgba(255,255,255,0.03)',
      borderColor: 'rgba(201,163,91,0.06)',
    },
  },
}

export type Severity = 'critical' | 'warning' | 'normal'

export const severityColor = (s: Severity): string =>
  s === 'critical' ? tokens.coral : s === 'warning' ? tokens.amber : tokens.teal

export const protocolColor = (proto: string): string => {
  switch (proto.toLowerCase()) {
    case 'ssh':
      return tokens.coral
    case 'mysql':
      return tokens.amber
    case 'http':
    case 'https':
      return tokens.teal
    case 'dpi':
      return tokens.violet
    default:
      return tokens.slate
  }
}

export const NETRA_ECHARTS_THEME = 'netra'

export const netraEchartsTheme = {
  color: [tokens.teal, tokens.amber, tokens.violet, tokens.coral, tokens.coast],
  backgroundColor: 'transparent',
  textStyle: { fontFamily: tokens.fontMono, color: tokens.slate },
  title: { textStyle: { color: tokens.parchment, fontFamily: tokens.fontUi } },
  legend: { textStyle: { color: tokens.slate } },
  tooltip: {
    backgroundColor: 'rgba(8,15,24,0.92)',
    borderColor: tokens.line,
    borderWidth: 1,
    textStyle: { color: tokens.parchment, fontFamily: tokens.fontMono },
  },
  line: { lineStyle: { width: 2 }, symbolSize: 0, smooth: true },
  categoryAxis: {
    axisLine: { lineStyle: { color: 'rgba(201,163,91,0.25)' } },
    axisTick: { show: false },
    axisLabel: { color: tokens.slateDim, fontFamily: tokens.fontMono },
    splitLine: { show: false },
  },
  valueAxis: {
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: tokens.slateDim, fontFamily: tokens.fontMono },
    splitLine: { lineStyle: { color: 'rgba(201,163,91,0.10)' } },
  },
  radar: {
    name: { textStyle: { color: tokens.slate } },
    axisName: { color: tokens.slate },
    splitLine: { lineStyle: { color: 'rgba(79,208,192,0.22)' } },
    splitArea: { show: false },
    axisLine: { lineStyle: { color: 'rgba(79,208,192,0.22)' } },
  },
  geo: {
    itemStyle: { areaColor: tokens.land, borderColor: 'rgba(201,163,91,0.28)', borderWidth: 0.5 },
    emphasis: { itemStyle: { areaColor: '#1D3348' }, label: { show: false } },
  },
}

type EchartsLike = { registerTheme: (name: string, theme: object) => void }

export const registerNetraEchartsTheme = (echarts: EchartsLike): void => {
  echarts.registerTheme(NETRA_ECHARTS_THEME, netraEchartsTheme)
}

type LinearGradientCtor = new (
  x0: number,
  y0: number,
  x1: number,
  y1: number,
  stops: { offset: number; color: string }[],
) => unknown

export const areaGradient = (
  echarts: { graphic: { LinearGradient: LinearGradientCtor } },
  hex: string = tokens.teal,
  topAlpha = '52',
): unknown =>
  new echarts.graphic.LinearGradient(0, 0, 0, 1, [
    { offset: 0, color: hex + topAlpha },
    { offset: 1, color: hex + '00' },
  ])

export const cssVars: Record<string, string> = {
  '--void': tokens.abyss,
  '--panel': tokens.panelSolid,
  '--panel-2': tokens.ocean,
  '--panel-raised': tokens.elevated,
  '--line': tokens.line,
  '--line-bright': tokens.lineBright,
  '--ink': tokens.parchment,
  '--ink-dim': tokens.slate,
  '--scan': tokens.teal,
  '--iris': tokens.violet,
  '--amber': tokens.amber,
  '--rose': tokens.coral,
  '--good': tokens.teal,
  '--highlight': tokens.amber,
  '--init': tokens.teal,
  '--recv': tokens.amber,
  '--coast': tokens.coast,
  '--parchment': tokens.parchment,
  '--font-ui': tokens.fontUi,
  '--font-mono': tokens.fontMono,
}

export const applyNetraCssVars = (root: HTMLElement = document.documentElement): void => {
  for (const [key, value] of Object.entries(cssVars)) {
    root.style.setProperty(key, value)
  }
}
