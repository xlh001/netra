import { useEffect, useRef, useState } from 'react'
import { Button, Input, Popconfirm, Spin, Tag, message } from 'antd'
import { ClockCircleOutlined, DeleteOutlined, PlusOutlined, RobotOutlined, ThunderboltOutlined, ToolOutlined } from '@ant-design/icons'
import { useT } from '../i18n/context'
import { createChatSession, deleteChatSession, listChatMessages, listChatSessions, sendChatMessage } from '../api/client'
import type { ChatMessage, ChatSession } from '../api/types'
import { renderMarkdownLite } from '../lib/markdownLite'

const TOOL_LABELS: Record<string, string> = {
  get_traffic_report: '流量总览',
  get_timeseries: '流量趋势',
  get_threat_alerts: '威胁告警',
  get_flows: '流量明细',
}
function toolLabel(name: string): string {
  return TOOL_LABELS[name] ?? name
}

function MessageMeta({ t, m }: { t: ReturnType<typeof useT>; m: ChatMessage }) {
  const hasTools = !!m.toolCalls && m.toolCalls.length > 0
  const hasTokens = (m.promptTokens ?? 0) > 0 || (m.completionTokens ?? 0) > 0
  if (!hasTools && !m.model && !m.elapsedMs && !hasTokens) return null
  return (
    <div className="ai-chat-meta">
      {hasTools && m.toolCalls!.map((name) => (
        <Tag key={name} color="cyan" icon={<ToolOutlined />}>
          {toolLabel(name)}
        </Tag>
      ))}
      {m.model && (
        <Tag color="geekblue" icon={<RobotOutlined />}>
          {m.model}
        </Tag>
      )}
      {!!m.elapsedMs && (
        <Tag color="gold" icon={<ClockCircleOutlined />}>
          {(m.elapsedMs / 1000).toFixed(1)}s
        </Tag>
      )}
      {hasTokens && (
        <Tag color="purple" icon={<ThunderboltOutlined />}>
          {t('aiChatTokens', { prompt: m.promptTokens ?? 0, completion: m.completionTokens ?? 0 })}
        </Tag>
      )}
    </div>
  )
}

export function Chat({ aiEnabled }: { aiEnabled: boolean }) {
  const t = useT()
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [activeId, setActiveId] = useState<number | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loadingSessions, setLoadingSessions] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [streamingText, setStreamingText] = useState('')
  const [streamingTool, setStreamingTool] = useState<string | null>(null)
  const [elapsedSec, setElapsedSec] = useState(0)
  const messagesEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!streaming) return
    setElapsedSec(0)
    const id = window.setInterval(() => setElapsedSec((s) => s + 1), 1000)
    return () => window.clearInterval(id)
  }, [streaming])

  const skipNextHistoryFetchRef = useRef(false)

  async function refreshSessions() {
    setLoadingSessions(true)
    try {
      setSessions(await listChatSessions())
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoadingSessions(false)
    }
  }

  useEffect(() => {
    if (aiEnabled) refreshSessions()
  }, [aiEnabled])

  useEffect(() => {
    if (activeId == null) {
      setMessages([])
      return
    }
    if (skipNextHistoryFetchRef.current) {
      skipNextHistoryFetchRef.current = false
      return
    }
    // activeId can change again (e.g. the user deletes the session they're
    // viewing) before this fetch resolves -- without the cancelled guard,
    // a stale response landing after that (success or 404, doesn't matter)
    // would still call setMessages/show an error for a session that's no
    // longer relevant.
    let cancelled = false
    setLoadingMessages(true)
    listChatMessages(activeId)
      .then((msgs) => {
        if (!cancelled) setMessages(msgs)
      })
      .catch((err) => {
        if (!cancelled) message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
      })
      .finally(() => {
        if (!cancelled) setLoadingMessages(false)
      })
    return () => {
      cancelled = true
    }
  }, [activeId, t])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'auto' })
  }, [messages, streamingText])

  async function handleNewSession() {
    try {
      const session = await createChatSession()
      setSessions((prev) => [session, ...prev])
      setActiveId(session.id)
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  async function handleDeleteSession(id: number) {
    try {
      await deleteChatSession(id)
      setSessions((prev) => prev.filter((s) => s.id !== id))
      if (activeId === id) setActiveId(null)
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  async function handleSend() {
    const content = input.trim()
    if (!content || streaming) return

    let sessionId = activeId
    if (sessionId == null) {
      try {
        const session = await createChatSession()
        setSessions((prev) => [session, ...prev])
        sessionId = session.id
        skipNextHistoryFetchRef.current = true
        setActiveId(sessionId)
      } catch (err) {
        message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
        return
      }
    }

    setInput('')
    setMessages((prev) => [...prev, { id: -1, role: 'user', content, createdAt: new Date().toISOString() }])
    setStreaming(true)
    setStreamingText('')
    setStreamingTool(null)

    let accumulated = ''
    const toolsUsed: string[] = []
    await sendChatMessage(sessionId, content, {
      onToken: (text) => {
        accumulated += text
        setStreamingTool(null)
        setStreamingText(accumulated)
      },
      onToolCall: (name) => {
        toolsUsed.push(name)
        setStreamingTool(name)
      },
      onDone: (info) => {
        setStreaming(false)
        setStreamingText('')
        setStreamingTool(null)
        setMessages((prev) => [
          ...prev,
          {
            id: -1,
            role: 'assistant',
            content: accumulated,
            toolCalls: toolsUsed,
            createdAt: new Date().toISOString(),
            model: info.model,
            elapsedMs: info.elapsedMs,
            promptTokens: info.promptTokens,
            completionTokens: info.completionTokens,
          },
        ])
        refreshSessions()
      },
      onError: (msg) => {
        setStreaming(false)
        setStreamingTool(null)
        message.error(t('aiChatSendFailed') + msg)
      },
    })
  }

  function handleInputKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  if (!aiEnabled) {
    return (
      <div className="ai-chat-disabled">
        <p>{t('aiChatDisabledTitle')}</p>
        <p className="settings-section-desc">{t('aiChatDisabledDesc')}</p>
      </div>
    )
  }

  return (
    <div className="ai-chat">
      <div className="ai-chat-sidebar">
        <Button icon={<PlusOutlined />} onClick={handleNewSession} block size="small">
          {t('aiChatNewSession')}
        </Button>
        {loadingSessions ? (
          <Spin size="small" style={{ marginTop: 12 }} />
        ) : sessions.length === 0 ? (
          <div className="empty">{t('aiChatEmptySessions')}</div>
        ) : (
          sessions.map((s) => (
            <div key={s.id} className={'ai-chat-session' + (s.id === activeId ? ' active' : '')} onClick={() => setActiveId(s.id)}>
              <span className="ai-chat-session-title">{s.title || t('aiChatNewSession')}</span>
              <Popconfirm title={t('aiChatDeleteConfirm')} onConfirm={() => handleDeleteSession(s.id)} okText={t('usersDeleteButton')} cancelText={t('usersCancel')}>
                <DeleteOutlined onClick={(e) => e.stopPropagation()} />
              </Popconfirm>
            </div>
          ))
        )}
      </div>
      <div className="ai-chat-main">
        <div className="ai-chat-messages">
          {loadingMessages && <Spin size="small" />}
          {messages.map((m, i) => (
            <div key={i} className={'ai-chat-bubble ' + m.role}>
              {renderMarkdownLite(m.content)}
              {m.role === 'assistant' && <MessageMeta t={t} m={m} />}
            </div>
          ))}
          {streaming && (
            <div className="ai-chat-bubble assistant">
              {streamingTool ? (
                <div className="ai-chat-tool-indicator">
                  {t('aiChatUsingTool', { name: toolLabel(streamingTool) })}
                  <span className="ai-chat-elapsed">{t('aiChatElapsed', { sec: elapsedSec })}</span>
                </div>
              ) : null}
              {streamingText
                ? renderMarkdownLite(streamingText)
                : !streamingTool && (
                    <div className="ai-chat-tool-indicator">
                      {t('aiChatThinking')}
                      <span className="ai-chat-elapsed">{t('aiChatElapsed', { sec: elapsedSec })}</span>
                    </div>
                  )}
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>
        <div className="ai-chat-input-row">
          <Input.TextArea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleInputKeyDown}
            placeholder={t('aiChatInputPlaceholder')}
            autoSize={{ minRows: 1, maxRows: 4 }}
            disabled={streaming}
          />
          <Button type="primary" onClick={handleSend} loading={streaming} disabled={!input.trim()}>
            {t('aiChatSend')}
          </Button>
        </div>
      </div>
    </div>
  )
}
