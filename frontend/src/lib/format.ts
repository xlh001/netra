
export function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  if (n < 1000) return Math.round(n) + 'B'
  let i = 0
  let v = n
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return v.toFixed(2) + units[i]
}

export function formatBps(bps: number): string {
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let i = 0
  let v = bps
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return v.toFixed(2) + ' ' + units[i]
}

const WINDOW_SECONDS: Record<string, number> = { '15m': 900, '30m': 1800, '1h': 3600, '1d': 86400, '15d': 15 * 86400, '1mo': 30 * 86400 }

export function windowToSeconds(w: string): number {
  return WINDOW_SECONDS[w] ?? 900
}

export function rangeToSeconds(range: { kind: 'window'; window: string } | { kind: 'custom'; from: number; to: number }): number {
  return range.kind === 'custom' ? Math.max(1, range.to - range.from) : windowToSeconds(range.window)
}

export function protoColor(name: string): string {
  switch (name) {
    case 'tcp':
      return '#35e0ff'
    case 'udp':
      return '#9b8cff'
    case 'icmp':
      return '#ffb454'
    default:
      return '#ff5d7a'
  }
}

export interface ServiceTint {
  fg: string
  bg: string
  bd: string
}

export function serviceColor(svc: string): ServiceTint | null {
  switch (svc) {
    case 'https':
      return { fg: '#7fdcae', bg: 'rgba(127,220,174,.12)', bd: 'rgba(127,220,174,.35)' }
    case 'http':
      return { fg: '#ff9e6d', bg: 'rgba(255,158,109,.12)', bd: 'rgba(255,158,109,.35)' }
    case 'ssh':
      return { fg: '#c874ff', bg: 'rgba(200,116,255,.14)', bd: 'rgba(200,116,255,.4)' }
    case 'dns':
      return { fg: '#e8c869', bg: 'rgba(232,200,105,.12)', bd: 'rgba(232,200,105,.35)' }
    case 'rdp':
      return { fg: '#6ea8ff', bg: 'rgba(110,168,255,.12)', bd: 'rgba(110,168,255,.35)' }
    case 'mysql':
      return { fg: '#4fd1c5', bg: 'rgba(79,209,197,.12)', bd: 'rgba(79,209,197,.35)' }
    case 'smtp':
      return { fg: '#f472b6', bg: 'rgba(244,114,182,.12)', bd: 'rgba(244,114,182,.35)' }
    default:
      return null
  }
}

const CATEGORY_PALETTE = [
  '#35e0ff', '#9b8cff', '#ffb454', '#2ee6a8', '#ff5d7a',
  '#7fdcae', '#ff9e6d', '#c874ff', '#e8c869', '#6ea8ff',
  '#4fd1c5', '#f472b6',
]

export function categoryColor(category: string, index: number): string {
  if (category === '其他') return '#8a7355'
  return CATEGORY_PALETTE[index % CATEGORY_PALETTE.length]
}

export function readableTextColor(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return luminance > 0.6 ? '#0a0b0f' : '#f5f7fa'
}

export function formatRemaining(expiresAt: string): string {
  const ms = new Date(expiresAt).getTime() - Date.now()
  if (ms <= 0) return '已过期'
  const minutes = Math.floor(ms / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)
  const years = Math.floor(days / 365)
  if (years >= 1) return `${years} 年`
  if (days >= 1) return `${days} 天`
  if (hours >= 1) return `${hours} 小时 ${minutes % 60} 分`
  return `${Math.max(1, minutes)} 分钟`
}

export function flagIconSrc(iso2: string | undefined): string {
  if (!iso2 || iso2.length !== 2) return ''
  return '/vendor/flags/' + iso2.toLowerCase() + '.svg'
}

let displayNames: Intl.DisplayNames | undefined

export function countryName(code: string): string {
  if (!code) return code
  if (!displayNames) {
    displayNames = new Intl.DisplayNames(['zh-CN'], { type: 'region' })
  }
  try {
    return displayNames.of(code.toUpperCase()) ?? code
  } catch {
    return code
  }
}
