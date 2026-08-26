import type {
  AlertKind,
  AuthUser,
  ChatMessage,
  ChatSession,
  ConfigDTO,
  DomainsPagedResponse,
  FlowRate,
  FlowsPagedResponse,
  GeoReport,
  IPsPagedResponse,
  IPTagKind,
  IPTagRecord,
  IPTagsPagedResponse,
  MCPAuthType,
  MCPServerRecord,
  MCPServerTransport,
  MCPToolInfo,
  MonitorSnapshot,
  PortMappingRecord,
  PortMappingsPagedResponse,
  PortsPagedResponse,
  Report,
  Role,
  ServiceCategoriesResponse,
  ThreatAlertsPagedResponse,
  TimeRange,
  Timeseries,
  Topology,
  UserRecord,
  WebhookChannel,
  WebhookRecord,
  Window,
} from './types'
import { windowToSeconds } from '../lib/format'

export const TOPN = 10

type UnauthorizedListener = () => void
let unauthorizedListeners: UnauthorizedListener[] = []
export function onUnauthorized(fn: UnauthorizedListener): () => void {
  unauthorizedListeners.push(fn)
  return () => {
    unauthorizedListeners = unauthorizedListeners.filter((f) => f !== fn)
  }
}
function notifyUnauthorized() {
  unauthorizedListeners.forEach((fn) => fn())
}

async function getJSON<T>(path: string, params?: Record<string, string | number | boolean | undefined>): Promise<T> {
  const qs = params
    ? '?' + Object.entries(params)
        .filter(([, v]) => v !== undefined)
        .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
        .join('&')
    : ''
  const res = await fetch(path + qs)
  if (res.status === 401) {
    notifyUnauthorized()
  }
  if (!res.ok) {
    throw new Error(`${path} failed: ${res.status} ${await res.text()}`)
  }
  return res.json() as Promise<T>
}

async function sendJSON<T>(path: string, method: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    notifyUnauthorized()
  }
  if (!res.ok) {
    throw new Error(`${method} ${path} failed: ${res.status} ${await res.text()}`)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export function getReport(window: Window): Promise<Report> {
  return getJSON<Report>('/api/report', { window, topn: TOPN })
}

export function getTimeseries(window: Window): Promise<Timeseries> {
  return getJSON<Timeseries>('/api/timeseries', { window })
}

export function getTimeseriesRange(range: TimeRange): Promise<Timeseries> {
  return getJSON<Timeseries>('/api/timeseries', absoluteRangeParams(range))
}

export function getFlowRate(): Promise<FlowRate> {
  return getJSON<FlowRate>('/api/flowrate')
}

export function getGeo(window: Window): Promise<GeoReport> {
  return getJSON<GeoReport>('/api/geo', { window })
}

export function getTopology(window: Window): Promise<Topology> {
  return getJSON<Topology>('/api/topology', { window })
}

function rangeParams(range: TimeRange): Record<string, string | number> {
  return range.kind === 'custom' ? { from: range.from, to: range.to } : { window: range.window }
}

function absoluteRangeParams(range: TimeRange): Record<string, string | number> {
  if (range.kind === 'custom') return { from: range.from, to: range.to }
  const now = Math.floor(Date.now() / 1000)
  return { from: now - windowToSeconds(range.window), to: now }
}

export function getFlowsPaged(range: TimeRange, page: number, pageSize: number, ip?: string): Promise<FlowsPagedResponse> {
  return getJSON<FlowsPagedResponse>('/api/admin/flows', { ...rangeParams(range), page, pageSize, ip: ip || undefined })
}

export function getIPsPaged(range: TimeRange, page: number, pageSize: number, q?: string): Promise<IPsPagedResponse> {
  return getJSON<IPsPagedResponse>('/api/admin/ips', { ...rangeParams(range), page, pageSize, q: q || undefined })
}

export function getPortsPaged(range: TimeRange, page: number, pageSize: number, q?: string): Promise<PortsPagedResponse> {
  return getJSON<PortsPagedResponse>('/api/admin/ports', { ...rangeParams(range), page, pageSize, q: q || undefined })
}

export function getDomainsPaged(range: TimeRange, page: number, pageSize: number, q?: string): Promise<DomainsPagedResponse> {
  return getJSON<DomainsPagedResponse>('/api/admin/domains', { ...rangeParams(range), page, pageSize, q: q || undefined })
}

export function getServiceCategories(range: TimeRange): Promise<ServiceCategoriesResponse> {
  return getJSON<ServiceCategoriesResponse>('/api/admin/service-categories', absoluteRangeParams(range))
}

export function getServiceCategoriesWindow(window: Window): Promise<ServiceCategoriesResponse> {
  return getJSON<ServiceCategoriesResponse>('/api/admin/service-categories', { window })
}

export function getThreatAlertsPaged(page: number, pageSize: number, q?: string, kind?: AlertKind | ''): Promise<ThreatAlertsPagedResponse> {
  return getJSON<ThreatAlertsPagedResponse>('/api/admin/threat-alerts', { page, pageSize, q: q || undefined, kind: kind || undefined })
}

export function getConfig(): Promise<ConfigDTO> {
  return getJSON<ConfigDTO>('/api/config')
}

export function putConfig(dto: ConfigDTO): Promise<ConfigDTO> {
  return sendJSON<ConfigDTO>('/api/config', 'PUT', dto)
}

export function testAI(baseURL: string, apiKey: string, model: string): Promise<void> {
  return sendJSON<void>('/api/admin/ai/test', 'POST', { baseURL, apiKey, model })
}

export function testKafka(
  kafkaBrokers: string,
  kafkaTopic: string,
  kafkaSaslUsername: string,
  kafkaSaslPassword: string,
  kafkaTls: boolean
): Promise<{ partitions: number }> {
  return sendJSON<{ partitions: number }>('/api/admin/kafka/test', 'POST', { kafkaBrokers, kafkaTopic, kafkaSaslUsername, kafkaSaslPassword, kafkaTls })
}

export function listChatSessions(): Promise<ChatSession[]> {
  return getJSON<ChatSession[]>('/api/admin/ai/chat/sessions')
}

export function createChatSession(): Promise<ChatSession> {
  return sendJSON<ChatSession>('/api/admin/ai/chat/sessions', 'POST')
}

export function deleteChatSession(id: number): Promise<void> {
  return sendJSON<void>(`/api/admin/ai/chat/sessions/${id}`, 'DELETE')
}

export function listChatMessages(sessionId: number): Promise<ChatMessage[]> {
  return getJSON<ChatMessage[]>(`/api/admin/ai/chat/sessions/${sessionId}/messages`)
}

export interface ChatDoneInfo {
  model?: string
  elapsedMs?: number
  promptTokens?: number
  completionTokens?: number
}

export interface ChatStreamHandlers {
  onToken: (text: string) => void
  onToolCall: (name: string) => void
  onDone: (info: ChatDoneInfo) => void
  onError: (message: string) => void
}

export async function sendChatMessage(sessionId: number, content: string, handlers: ChatStreamHandlers): Promise<void> {
  const res = await fetch(`/api/admin/ai/chat/sessions/${sessionId}/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  })
  if (res.status === 401) {
    notifyUnauthorized()
  }
  if (!res.ok) {
    handlers.onError((await res.text()) || `request failed: ${res.status}`)
    return
  }
  if (!res.body) {
    handlers.onError('this browser does not support streaming responses')
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let boundary: number
    while ((boundary = buffer.indexOf('\n\n')) !== -1) {
      const rawEvent = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      let eventName = 'message'
      let data = ''
      for (const line of rawEvent.split('\n')) {
        if (line.startsWith('event:')) eventName = line.slice(6).trim()
        else if (line.startsWith('data:')) data += line.slice(5).trim()
      }
      if (!data) continue
      let payload: {
        text?: string
        name?: string
        message?: string
        model?: string
        elapsedMs?: number
        promptTokens?: number
        completionTokens?: number
      }
      try {
        payload = JSON.parse(data)
      } catch {
        continue
      }
      switch (eventName) {
        case 'token':
          handlers.onToken(payload.text ?? '')
          break
        case 'tool_call':
          handlers.onToolCall(payload.name ?? '')
          break
        case 'done':
          handlers.onDone({
            model: payload.model,
            elapsedMs: payload.elapsedMs,
            promptTokens: payload.promptTokens,
            completionTokens: payload.completionTokens,
          })
          break
        case 'error':
          handlers.onError(payload.message ?? 'unknown error')
          break
      }
    }
  }
}

export function testWebhook(channel: WebhookChannel, url: string, secret?: string): Promise<void> {
  return sendJSON<void>('/api/admin/webhooks/test', 'POST', { channel, url, secret: secret || undefined })
}

export function listWebhooks(): Promise<WebhookRecord[]> {
  return getJSON<WebhookRecord[]>('/api/admin/webhooks')
}

export function createWebhook(channel: WebhookChannel, url: string, secret: string, enabled: boolean): Promise<WebhookRecord> {
  return sendJSON<WebhookRecord>('/api/admin/webhooks', 'POST', { channel, url, secret, enabled })
}

export function updateWebhook(id: number, url: string, secret: string, enabled: boolean): Promise<WebhookRecord> {
  return sendJSON<WebhookRecord>(`/api/admin/webhooks/${id}`, 'PUT', { url, secret, enabled })
}

export function deleteWebhook(id: number): Promise<void> {
  return sendJSON<void>(`/api/admin/webhooks/${id}`, 'DELETE')
}

export function getIPTagsPaged(page: number, pageSize: number, q?: string): Promise<IPTagsPagedResponse> {
  return getJSON<IPTagsPagedResponse>('/api/admin/ip-tags', { page, pageSize, q: q || undefined })
}

export function createIPTag(kind: IPTagKind, value: string, label: string): Promise<IPTagRecord> {
  return sendJSON<IPTagRecord>('/api/admin/ip-tags', 'POST', { kind, value, label })
}

export function updateIPTag(id: number, label: string): Promise<IPTagRecord> {
  return sendJSON<IPTagRecord>(`/api/admin/ip-tags/${id}`, 'PUT', { label })
}

export function deleteIPTag(id: number): Promise<void> {
  return sendJSON<void>(`/api/admin/ip-tags/${id}`, 'DELETE')
}

export function getPortMappingsPaged(page: number, pageSize: number, q?: string): Promise<PortMappingsPagedResponse> {
  return getJSON<PortMappingsPagedResponse>('/api/admin/port-mappings', { page, pageSize, q: q || undefined })
}

export function createPortMapping(port: number, service: string): Promise<PortMappingRecord> {
  return sendJSON<PortMappingRecord>('/api/admin/port-mappings', 'POST', { port, service })
}

export function updatePortMapping(port: number, service: string): Promise<PortMappingRecord> {
  return sendJSON<PortMappingRecord>(`/api/admin/port-mappings/${port}`, 'PUT', { service })
}

export function deletePortMapping(port: number): Promise<void> {
  return sendJSON<void>(`/api/admin/port-mappings/${port}`, 'DELETE')
}

export function listMCPServers(): Promise<MCPServerRecord[]> {
  return getJSON<MCPServerRecord[]>('/api/admin/mcp-servers')
}

export interface MCPAuthFields {
  authType: MCPAuthType
  authUsername: string
  authPassword: string
  authToken: string
}

export function createMCPServer(name: string, transport: MCPServerTransport, endpoint: string, command: string, args: string, enabled: boolean, auth: MCPAuthFields): Promise<MCPServerRecord> {
  return sendJSON<MCPServerRecord>('/api/admin/mcp-servers', 'POST', { name, transport, endpoint, command, args, enabled, ...auth })
}

export function updateMCPServer(id: number, name: string, endpoint: string, command: string, args: string, enabled: boolean, auth: MCPAuthFields): Promise<MCPServerRecord> {
  return sendJSON<MCPServerRecord>(`/api/admin/mcp-servers/${id}`, 'PUT', { name, endpoint, command, args, enabled, ...auth })
}

export function deleteMCPServer(id: number): Promise<void> {
  return sendJSON<void>(`/api/admin/mcp-servers/${id}`, 'DELETE')
}

export function listMCPServerTools(id: number): Promise<{ tools: MCPToolInfo[] }> {
  return getJSON<{ tools: MCPToolInfo[] }>(`/api/admin/mcp-servers/${id}/tools`)
}

export function testMCPServer(transport: MCPServerTransport, endpoint: string, command: string, args: string, auth: MCPAuthFields): Promise<{ tools: string[] }> {
  return sendJSON<{ tools: string[] }>('/api/admin/mcp-servers/test', 'POST', { transport, endpoint, command, args, ...auth })
}

export function getMonitorSnapshot(): Promise<MonitorSnapshot> {
  return getJSON<MonitorSnapshot>('/api/admin/monitor')
}

export async function login(username: string, password: string): Promise<AuthUser> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  if (!res.ok) {
    throw new Error(res.status === 401 ? 'invalid username or password' : `login failed: ${res.status}`)
  }
  return res.json() as Promise<AuthUser>
}

export async function logout(): Promise<void> {
  await fetch('/api/auth/logout', { method: 'POST' })
}

export async function getMe(): Promise<AuthUser | null> {
  const res = await fetch('/api/auth/me')
  if (!res.ok) return null
  return res.json() as Promise<AuthUser>
}

export function listUsers(): Promise<UserRecord[]> {
  return getJSON<UserRecord[]>('/api/admin/users')
}

export function createUser(username: string, password: string, role: Role, description: string, longLived: boolean): Promise<UserRecord> {
  return sendJSON<UserRecord>('/api/admin/users', 'POST', { username, password, role, description, longLived })
}

export function updateUser(id: number, role: Role, password: string, description: string, longLived: boolean): Promise<UserRecord> {
  return sendJSON<UserRecord>(`/api/admin/users/${id}`, 'PUT', { role, password, description, longLived })
}

export function deleteUser(id: number): Promise<void> {
  return sendJSON<void>(`/api/admin/users/${id}`, 'DELETE')
}
