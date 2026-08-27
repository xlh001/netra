
export interface ProtoStat {
  proto: string
  packets: number
  bytes: number
}

export interface FlowStat {
  srcIP: string
  srcPort: number
  srcLabel?: string
  srcCountry?: string
  dstIP: string
  dstPort: number
  dstLabel?: string
  dstCountry?: string
  proto: string
  service?: string
  dpi?: boolean
  domain?: string
  packets: number
  bytes: number
}

export interface IPStat {
  ip: string
  label?: string
  country?: string
  org?: string
  packets: number
  bytes: number
}

export interface PortStat {
  port: number
  proto: string
  service?: string
  packets: number
  bytes: number
}

export interface DomainStat {
  domain: string
  packets: number
  bytes: number
}

export interface ServiceStat {
  service: string
  packets: number
  bytes: number
}

export interface CategoryStat {
  category: string
  packets: number
  bytes: number
  services: ServiceStat[]
}

export type AlertKind = 'scan' | 'ddos' | 'volume'

export interface ThreatAlert {
  kind: AlertKind
  ip: string
  distinctPeers?: number
  volumeBytes?: number
}

export interface Report {
  window: string
  generatedAt: string
  activeFlowsNow: number
  totalPackets: number
  totalBytes: number
  readFailures?: number
  possibleTicks: number
  scanAlerts?: ThreatAlert[]
  protocols: ProtoStat[]
  topFlows: FlowStat[]
  topIPs: IPStat[]
  topPorts: PortStat[]
  topDomains: DomainStat[]
}

export interface TimeseriesPoint {
  time: string
  bytes: Record<string, number>
}

export interface Timeseries {
  window: string
  points: TimeseriesPoint[]
}

export interface FlowRatePoint {
  time: string
  count: number
  perSec: number
}

export interface FlowRate {
  points: FlowRatePoint[]
}

export interface GeoPoint {
  ip: string
  lat: number
  lng: number
  country: string
  org?: string
  packets: number
  bytes: number
}

export interface GeoReport {
  enabled: boolean
  window: string
  points: GeoPoint[]
  excludedPrivate: number
  excludedUnresolved: number
}

export interface TopologyNode {
  ip: string
  label?: string
  bytes: number
  packets: number
}

export interface TopologyEdge {
  src: string
  dst: string
  bytes: number
  packets: number
}

export interface Topology {
  window: string
  nodes: TopologyNode[]
  edges: TopologyEdge[]
}

export interface FlowsPagedResponse {
  total: number
  page: number
  flows: FlowStat[]
}

export interface IPsPagedResponse {
  total: number
  page: number
  ips: IPStat[]
}

export interface PortsPagedResponse {
  total: number
  page: number
  ports: PortStat[]
}

export interface ServiceCategoriesResponse {
  categories: CategoryStat[]
}

export interface DomainsPagedResponse {
  total: number
  page: number
  domains: DomainStat[]
}

export interface ThreatAlertRecord {
  time: string
  kind: AlertKind
  ip: string
  label?: string
  distinctPeers?: number
  volumeBytes?: number
}

export interface ThreatAlertsPagedResponse {
  total: number
  page: number
  alerts: ThreatAlertRecord[]
}

export interface ConfigDTO {
  refreshIntervalMs: number
  persistScanAlerts: boolean
  dbFlowTopK: number
  topKPerBucket: number

  anomalyEnabled: boolean
  anomalyWindowSec: number
  anomalyPeerThreshold: number
  anomalyAvgPacketsThreshold: number

  volumeThresholdBytes: number

  aiEnabled: boolean
  aiProvider: AIProvider
  aiBaseURL: string
  aiApiKey?: string
  aiModel: string

  kafkaEnabled: boolean
  kafkaBrokers: string
  kafkaTopic: string
  kafkaSaslUsername?: string
  kafkaSaslPassword?: string
  kafkaTls: boolean
  kafkaFlowTopK: number
}

export type AIProvider = 'openai' | 'deepseek' | 'qwen' | 'moonshot' | 'glm' | 'doubao' | 'ollama' | 'custom' | ''

export interface ChatSession {
  id: number
  title: string
  createdAt: string
  updatedAt: string
}

export interface ChatMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  toolCalls?: string[]
  createdAt: string

  model?: string
  elapsedMs?: number
  promptTokens?: number
  completionTokens?: number
}

export type WebhookChannel = 'wecom' | 'dingtalk' | 'feishu'

export interface WebhookRecord {
  id: number
  channel: WebhookChannel
  url: string
  secret?: string
  enabled: boolean
}

export type IPTagKind = 'ip' | 'cidr'

export interface IPTagRecord {
  id: number
  kind: IPTagKind
  value: string
  label: string
}

export interface IPTagsPagedResponse {
  total: number
  page: number
  tags: IPTagRecord[]
}

export type MCPServerTransport = 'http' | 'stdio'
export type MCPAuthType = 'none' | 'basic' | 'bearer'
export type MCPConnStatus = 'connected' | 'error' | 'disconnected'

export interface MCPServerRecord {
  id: number
  name: string
  transport: MCPServerTransport
  endpoint?: string
  command?: string
  args?: string
  enabled: boolean
  authType: MCPAuthType
  authUsername?: string
  authPassword?: string
  authToken?: string
  status: MCPConnStatus
  statusError?: string
}

export interface MCPToolInfo {
  name: string
  description: string
}

export type Window = '15m' | '30m' | '1h' | '1d'

export type FlowExplorerWindow = Window | '15d' | '1mo'

export type TimeRange = { kind: 'window'; window: FlowExplorerWindow } | { kind: 'custom'; from: number; to: number }

export type Role = 'admin' | 'normal'

export interface AuthUser {
  username: string
  role: Role
  expiresAt: string
}

export interface MonitorSnapshot {
  numCPU: number
  cpuPercent: number
  loadAvg1: number
  loadAvg5: number
  loadAvg15: number
  memTotalBytes: number
  memUsedBytes: number
  memUsedPercent: number
  diskTotalBytes: number
  diskUsedBytes: number
  diskUsedPercent: number

  goroutines: number
  heapAllocBytes: number
  heapSysBytes: number
  numGC: number
  processUptimeSec: number

  persistenceEnabled: boolean
  dbFileSizeBytes?: number
  dbWalSizeBytes?: number
  tsStoreSizeBytes?: number
  dbOpenConns?: number
  dbInUseConns?: number
  dbIdleConns?: number
  dbWaitCount?: number

  readFailuresRecent: number
  activeFlows: number

  kafkaEnabled: boolean
  kafkaQueueBytes: number
  kafkaDroppedTicksTotal: number
  kafkaWriteErrorsTotal: number

  threatAlertsScanTotal: number
  threatAlertsDdosTotal: number
  threatAlertsVolumeTotal: number

  mcpServersConnected: number
  mcpServersTotal: number

  xdpGenericMode: boolean
  ifaces: IfaceStatus[]

}

export interface IfaceStatus {
  name: string
  promiscEnabledByNetra: boolean
  carrierUp: boolean
  speedMbps?: number
  rxPPS: number
  rxBPS: number
}

export interface UserRecord {
  id: number
  username: string
  role: Role
  createdAt: string
  description?: string
  longLived: boolean
}
