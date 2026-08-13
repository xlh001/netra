import { useCallback, useEffect, useRef, useState } from 'react'
import { Button, Divider, Form, Input, InputNumber, Modal, Popconfirm, Select, Switch, Table, Tabs, Tag, message } from 'antd'
import type { FormInstance } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { AlertOutlined, MessageOutlined } from '@ant-design/icons'
import { useT } from '../i18n/context'
import { useConfigContext } from '../config/context'
import {
  createIPTag,
  createMCPServer,
  createPortMapping,
  createWebhook,
  deleteIPTag,
  deleteMCPServer,
  deletePortMapping,
  deleteWebhook,
  getIPTagsPaged,
  getPortMappingsPaged,
  listMCPServers,
  listMCPServerTools,
  listWebhooks,
  testAI,
  testMCPServer,
  testWebhook,
  updateIPTag,
  updateMCPServer,
  updatePortMapping,
  updateWebhook,
} from '../api/client'
import type { AIProvider, ConfigDTO, IPTagKind, IPTagRecord, MCPAuthType, MCPConnStatus, MCPServerRecord, MCPServerTransport, MCPToolInfo, PortMappingRecord, WebhookChannel, WebhookRecord } from '../api/types'
import { tablePagination } from '../lib/antdTable'

const AI_PRESETS: Record<Exclude<AIProvider, ''>, { label: string; modelPlaceholderKey: string }> = {
  openai: { label: 'OpenAI', modelPlaceholderKey: 'aiModelPlaceholderGeneric' },
  deepseek: { label: 'DeepSeek', modelPlaceholderKey: 'aiModelPlaceholderGeneric' },
  qwen: { label: '通义千问', modelPlaceholderKey: 'aiModelPlaceholderGeneric' },
  moonshot: { label: 'Moonshot (Kimi)', modelPlaceholderKey: 'aiModelPlaceholderGeneric' },
  glm: { label: '智谱 GLM', modelPlaceholderKey: 'aiModelPlaceholderGeneric' },
  doubao: { label: '火山引擎豆包', modelPlaceholderKey: 'aiModelPlaceholderDoubao' },
  ollama: { label: 'Ollama (本地)', modelPlaceholderKey: 'aiModelPlaceholderOllama' },
  custom: { label: '', modelPlaceholderKey: 'aiModelPlaceholderCustom' },
}

const CRUD_ONLY_TABS = new Set(['channels', 'ipTags', 'portMappings', 'mcpServers'])

export function Settings() {
  const t = useT()
  const { config, loading, error, save } = useConfigContext()
  const [form] = Form.useForm<ConfigDTO>()
  const [activeTab, setActiveTab] = useState('general')

  useEffect(() => {
    if (config) form.setFieldsValue(config)
  }, [config, form])

  async function handleFinish(values: ConfigDTO) {
    try {
      const updated = await save(values)
      form.setFieldsValue(updated)
      message.success(t('settingsSaved'))
    } catch (err) {
      message.error(t('settingsSaveFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('settingsPageTitle')}</span>
        </h2>
      </div>
      <div className="panel-body settings-panel-body">
        {loading && !config && <div className="empty">{t('noData')}</div>}
        {error && (
          <div className="empty">
            {t('fetchFailed')}
            {error.message}
          </div>
        )}
        {config && (
          <Form form={form} layout="vertical" onFinish={handleFinish} style={{ paddingTop: 4 }}>
            <Tabs
              activeKey={activeTab}
              onChange={setActiveTab}
              items={[
                { key: 'general', label: t('settingsSectionGeneral'), forceRender: true, children: <GeneralTab t={t} /> },
                { key: 'threat', label: t('settingsSectionThreat'), forceRender: true, children: <ThreatTab t={t} /> },
                { key: 'capacity', label: t('settingsSectionCapacity'), forceRender: true, children: <CapacityTab t={t} /> },
                { key: 'ai', label: t('aiPageTitle'), forceRender: true, children: <AITab t={t} form={form} config={config} /> },
                { key: 'kafka', label: t('settingsSectionKafka'), forceRender: true, children: <KafkaTab t={t} form={form} /> },
                { key: 'channels', label: t('settingsSectionChannels'), forceRender: true, children: <ChannelsTab t={t} /> },
                { key: 'ipTags', label: t('settingsSectionIPTags'), forceRender: true, children: <AssetTagsTab t={t} /> },
                { key: 'portMappings', label: t('settingsSectionPortMappings'), forceRender: true, children: <PortMappingsTab t={t} /> },
                { key: 'mcpServers', label: t('settingsSectionMCP'), forceRender: true, children: <MCPServersTab t={t} /> },
              ]}
            />
            {!CRUD_ONLY_TABS.has(activeTab) && (
              <Form.Item style={{ marginTop: 8 }}>
                <Button type="primary" htmlType="submit">
                  {t('settingsSave')}
                </Button>
              </Form.Item>
            )}
          </Form>
        )}
      </div>
    </div>
  )
}

type T = ReturnType<typeof useT>

function GeneralTab({ t }: { t: T }) {
  return (
    <Form.Item label={t('settingsRefreshInterval')} name="refreshIntervalMs" rules={[{ required: true }]} extra={t('settingsRefreshIntervalHint')}>
      <InputNumber min={1} style={{ width: '100%' }} />
    </Form.Item>
  )
}

const BYTES_PER_GB = 1024 * 1024 * 1024

function VolumeThresholdInput({ value, onChange }: { value?: number; onChange?: (bytes: number) => void }) {
  const displayValue = value != null ? Math.round((value / BYTES_PER_GB) * 100) / 100 : undefined

  return (
    <InputNumber
      min={0.1}
      step={0.1}
      style={{ width: '100%' }}
      addonAfter="GB"
      value={displayValue}
      onChange={(n) => {
        if (n == null) return
        onChange?.(Math.round(n * BYTES_PER_GB))
      }}
    />
  )
}

function ThreatTab({ t }: { t: T }) {
  return (
    <>
      <Form.Item label={t('settingsPersistAlerts')} name="persistScanAlerts" valuePropName="checked">
        <Switch />
      </Form.Item>

      <Divider titlePlacement="left" style={{ marginTop: 0 }}>
        <span style={{ color: 'var(--amber)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <AlertOutlined />
          {t('settingsSectionThreatPeer')}
        </span>
      </Divider>
      <p className="settings-section-desc">{t('settingsSectionThreatPeerDesc')}</p>
      <Form.Item label={t('settingsAnomalyWindow')} name="anomalyWindowSec" rules={[{ required: true }]}>
        <InputNumber min={1} style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item label={t('settingsAnomalyPeerThreshold')} name="anomalyPeerThreshold" rules={[{ required: true }]}>
        <InputNumber min={1} style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item label={t('settingsAnomalyAvgPackets')} name="anomalyAvgPacketsThreshold" rules={[{ required: true }]}>
        <InputNumber min={0.1} step={0.1} style={{ width: '100%' }} />
      </Form.Item>

      <Divider titlePlacement="left">
        <span style={{ color: 'var(--rose)', display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <AlertOutlined />
          {t('settingsSectionThreatVolume')}
        </span>
      </Divider>
      <p className="settings-section-desc">{t('settingsSectionThreatVolumeDesc')}</p>
      <Form.Item label={t('settingsVolumeThreshold')} name="volumeThresholdBytes" rules={[{ required: true }]}>
        <VolumeThresholdInput />
      </Form.Item>
    </>
  )
}

function CapacityTab({ t }: { t: T }) {
  return (
    <>
      <Form.Item label={t('settingsDbFlowTopK')} name="dbFlowTopK" rules={[{ required: true }]} extra={t('settingsDbFlowTopKHint')}>
        <InputNumber min={1} style={{ width: '100%' }} />
      </Form.Item>
      <Form.Item label={t('settingsTopKPerBucket')} name="topKPerBucket" rules={[{ required: true }]} extra={t('settingsTopKPerBucketHint')}>
        <InputNumber min={1} style={{ width: '100%' }} />
      </Form.Item>
    </>
  )
}

function AITab({ t, form, config }: { t: T; form: FormInstance<ConfigDTO>; config: ConfigDTO }) {
  const [testing, setTesting] = useState(false)
  const [provider, setProvider] = useState<AIProvider>('')

  const aiEnabled = Form.useWatch('aiEnabled', form)

  const prevProviderRef = useRef<AIProvider>('')
  // The provider that actually has a real saved config right now, and its
  // baseURL/apiKey/model -- switching the preset dropdown back to THIS
  // provider should restore these real values, not blank fields out.
  // Without this, re-selecting the provider you're already configured for
  // (after having browsed through others) looked like your saved config
  // had vanished -- it hadn't, the dropdown was just overwriting the form
  // with an empty preset on every "different from the last selection"
  // change, with no way to tell "different, but it's the one you actually
  // saved" apart from "different, and genuinely never configured". A
  // refresh or leaving/re-entering the page always "fixed" it because
  // that re-reads the real saved config from the server -- the bug was
  // purely in-memory, this dropdown's own state, not the saved data
  // itself. Updates whenever a fresh config arrives (initial load, or
  // right after a successful save), so the restore point always tracks
  // whatever's actually persisted.
  //
  // Read from the `config` prop, not `form.getFieldValue(...)`, for the
  // initial capture -- this component's own mount-time effect fires
  // before Settings' effect that pushes `config` into the form via
  // `setFieldsValue` (child effects run before parent effects in the same
  // commit), so reading the form here on mount would race and capture
  // stale/empty values.
  const savedConfigRef = useRef<{ provider: AIProvider; baseURL: string; apiKey?: string; model: string }>({
    provider: '',
    baseURL: '',
    apiKey: '',
    model: '',
  })

  useEffect(() => {
    const initial = config.aiProvider ?? ''
    setProvider(initial)
    prevProviderRef.current = initial
    savedConfigRef.current = {
      provider: initial,
      baseURL: config.aiBaseURL ?? '',
      apiKey: config.aiApiKey ?? '',
      model: config.aiModel ?? '',
    }
  }, [config])

  function handleProviderChange(value: AIProvider) {
    const previous = prevProviderRef.current
    prevProviderRef.current = value
    setProvider(value)
    if (value === '' || value === previous) return

    if (value === savedConfigRef.current.provider) {
      form.setFieldsValue({
        aiBaseURL: savedConfigRef.current.baseURL,
        aiApiKey: savedConfigRef.current.apiKey,
        aiModel: savedConfigRef.current.model,
      })
      return
    }

    form.setFieldsValue({ aiBaseURL: '', aiModel: '', aiApiKey: '' })
  }

  async function handleTest() {
    const { aiBaseURL, aiApiKey, aiModel } = form.getFieldsValue()
    if (!aiBaseURL || !aiModel) {
      message.warning(t('aiTestNeedsFields'))
      return
    }
    setTesting(true)
    try {
      await testAI(aiBaseURL, aiApiKey ?? '', aiModel)
      message.success(t('aiTestSuccess'))
    } catch (err) {
      message.error(t('aiTestFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setTesting(false)
    }
  }

  const modelPlaceholder = provider ? t(AI_PRESETS[provider].modelPlaceholderKey as Parameters<typeof t>[0]) : t('aiModelPlaceholderGeneric')

  return (
    <>
      <Form.Item label={t('aiEnabled')} name="aiEnabled" valuePropName="checked">
        <Switch />
      </Form.Item>
      <Form.Item label={t('aiProviderLabel')} name="aiProvider">
        <Select<AIProvider>
          onChange={handleProviderChange}
          options={[
            { value: '', label: t('aiProviderSelectPlaceholder') },
            ...Object.entries(AI_PRESETS)
              .filter(([key]) => key !== 'custom')
              .map(([key, p]) => ({ value: key, label: p.label })),
            { value: 'custom', label: t('aiProviderCustom') },
          ]}
        />
      </Form.Item>
      <Form.Item
        label={t('aiBaseURL')}
        name="aiBaseURL"
        rules={aiEnabled ? [{ required: true, message: t('aiBaseURLRequired') }] : []}
        extra={t('aiBaseURLHint')}
      >
        <Input placeholder="https://api.example.com" />
      </Form.Item>
      <Form.Item label={t('aiApiKey')} name="aiApiKey" extra={t('aiApiKeyHint')}>
        <Input.Password />
      </Form.Item>
      <Form.Item label={t('aiModel')} name="aiModel" rules={aiEnabled ? [{ required: true, message: t('aiModelRequired') }] : []} extra={modelPlaceholder}>
        <Input placeholder={modelPlaceholder} />
      </Form.Item>
      <Form.Item>
        <Button onClick={handleTest} loading={testing}>
          {t('aiTestButton')}
        </Button>
      </Form.Item>
    </>
  )
}

function KafkaTab({ t, form }: { t: T; form: FormInstance<ConfigDTO> }) {
  const kafkaEnabled = Form.useWatch('kafkaEnabled', form)
  return (
    <>
      <Form.Item label={t('kafkaEnabled')} name="kafkaEnabled" valuePropName="checked" extra={t('kafkaEnabledHint')}>
        <Switch />
      </Form.Item>
      <Form.Item
        label={t('kafkaBrokers')}
        name="kafkaBrokers"
        rules={kafkaEnabled ? [{ required: true, message: t('kafkaBrokersRequired') }] : []}
        extra={t('kafkaBrokersHint')}
      >
        <Input placeholder="broker1:9092,broker2:9092" />
      </Form.Item>
      <Form.Item label={t('kafkaTopic')} name="kafkaTopic" rules={kafkaEnabled ? [{ required: true, message: t('kafkaTopicRequired') }] : []}>
        <Input placeholder="netra-flows" />
      </Form.Item>
      <Form.Item label={t('kafkaSaslUsername')} name="kafkaSaslUsername" extra={t('kafkaSaslHint')}>
        <Input />
      </Form.Item>
      <Form.Item label={t('kafkaSaslPassword')} name="kafkaSaslPassword">
        <Input.Password />
      </Form.Item>
      <Form.Item label={t('kafkaTls')} name="kafkaTls" valuePropName="checked">
        <Switch />
      </Form.Item>
    </>
  )
}

function ChannelsTab({ t }: { t: T }) {
  const [webhooks, setWebhooks] = useState<WebhookRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<{ channel: WebhookChannel; record: WebhookRecord | 'new' } | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setWebhooks((await listWebhooks()) ?? [])
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function handleDelete(w: WebhookRecord) {
    try {
      await deleteWebhook(w.id)
      message.success(t('settingsChannelDeleteButton'))
      refresh()
    } catch (err) {
      message.error(t('settingsChannelActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  async function handleToggleEnabled(w: WebhookRecord, enabled: boolean) {
    try {
      await updateWebhook(w.id, w.url, w.secret ?? '', enabled)
      refresh()
    } catch (err) {
      message.error(t('settingsChannelActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  return (
    <>
      <ChannelCard
        t={t}
        channel="wecom"
        icon={<MessageOutlined />}
        color="var(--good)"
        titleKey="settingsChannelWeCom"
        webhooks={webhooks}
        loading={loading}
        onAdd={() => setEditing({ channel: 'wecom', record: 'new' })}
        onEdit={(w) => setEditing({ channel: 'wecom', record: w })}
        onDelete={handleDelete}
        onToggleEnabled={handleToggleEnabled}
      />
      <ChannelCard
        t={t}
        channel="dingtalk"
        icon={<MessageOutlined />}
        color="var(--scan)"
        titleKey="settingsChannelDingTalk"
        webhooks={webhooks}
        loading={loading}
        showSecret
        onAdd={() => setEditing({ channel: 'dingtalk', record: 'new' })}
        onEdit={(w) => setEditing({ channel: 'dingtalk', record: w })}
        onDelete={handleDelete}
        onToggleEnabled={handleToggleEnabled}
      />
      <ChannelCard
        t={t}
        channel="feishu"
        icon={<MessageOutlined />}
        color="var(--iris)"
        titleKey="settingsChannelFeishu"
        webhooks={webhooks}
        loading={loading}
        onAdd={() => setEditing({ channel: 'feishu', record: 'new' })}
        onEdit={(w) => setEditing({ channel: 'feishu', record: w })}
        onDelete={handleDelete}
        onToggleEnabled={handleToggleEnabled}
      />

      {editing && (
        <WebhookFormModal
          t={t}
          channel={editing.channel}
          record={editing.record}
          showSecret={editing.channel === 'dingtalk'}
          onDone={() => {
            setEditing(null)
            refresh()
          }}
          onCancel={() => setEditing(null)}
        />
      )}
    </>
  )
}

function ChannelCard({
  t,
  channel,
  icon,
  color,
  titleKey,
  webhooks,
  loading,
  showSecret,
  onAdd,
  onEdit,
  onDelete,
  onToggleEnabled,
}: {
  t: T
  channel: WebhookChannel
  icon: React.ReactNode
  color: string
  titleKey: 'settingsChannelWeCom' | 'settingsChannelDingTalk' | 'settingsChannelFeishu'
  webhooks: WebhookRecord[]
  loading: boolean
  showSecret?: boolean
  onAdd: () => void
  onEdit: (w: WebhookRecord) => void
  onDelete: (w: WebhookRecord) => void
  onToggleEnabled: (w: WebhookRecord, enabled: boolean) => void
}) {
  const [testingID, setTestingID] = useState<number | null>(null)

  async function handleTest(w: WebhookRecord) {
    setTestingID(w.id)
    try {
      await testWebhook(w.channel, w.url, w.secret)
      message.success(t('settingsTestSuccess'))
    } catch (err) {
      message.error(t('settingsTestFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setTestingID(null)
    }
  }

  const rows = webhooks.filter((w) => w.channel === channel)

  const columns: ColumnsType<WebhookRecord> = [
    { title: t('settingsChannelColURL'), dataIndex: 'url', ellipsis: true },
    ...(showSecret
      ? [
          {
            title: t('settingsChannelColSecret'),
            key: 'secret',
            render: (_: unknown, w: WebhookRecord) => (w.secret ? t('settingsChannelSecretSet') : t('settingsChannelSecretUnset')),
          },
        ]
      : []),
    {
      title: t('settingsChannelColEnabled'),
      key: 'enabled',
      render: (_, w) => <Switch checked={w.enabled} size="small" onChange={(checked) => onToggleEnabled(w, checked)} />,
    },
    {
      title: t('settingsChannelColActions'),
      key: 'actions',
      render: (_, w) => (
        <>
          <Button type="link" size="small" loading={testingID === w.id} onClick={() => handleTest(w)}>
            {t('settingsTestButton')}
          </Button>
          <Button type="link" size="small" onClick={() => onEdit(w)}>
            {t('settingsChannelEditButton')}
          </Button>
          <Popconfirm
            title={t('settingsChannelDeleteConfirm')}
            onConfirm={() => onDelete(w)}
            okText={t('settingsChannelDeleteButton')}
            cancelText={t('usersCancel')}
            okButtonProps={{ danger: true }}
          >
            <Button type="link" size="small" danger>
              {t('settingsChannelDeleteButton')}
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ]

  return (
    <>
      <Divider titlePlacement="left" style={{ marginTop: 0 }}>
        <span style={{ color, display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          {icon}
          {t(titleKey)}
        </span>
      </Divider>
      <div className="compact-table">
        <Table
          rowKey="id"
          columns={columns}
          dataSource={rows}
          loading={loading}
          pagination={false}
          size="small"
          locale={{ emptyText: t('settingsChannelEmpty') }}
        />
      </div>
      <div style={{ marginTop: 8, marginBottom: 16 }}>
        <Button size="small" onClick={onAdd}>
          + {t('settingsChannelAddButton')}
        </Button>
      </div>
    </>
  )
}

interface WebhookFormValues {
  url: string
  secret?: string
  enabled: boolean
}

function WebhookFormModal({
  t,
  channel,
  record,
  showSecret,
  onDone,
  onCancel,
}: {
  t: T
  channel: WebhookChannel
  record: WebhookRecord | 'new'
  showSecret: boolean
  onDone: () => void
  onCancel: () => void
}) {
  const isNew = record === 'new'
  const [form] = Form.useForm<WebhookFormValues>()
  const [submitting, setSubmitting] = useState(false)

  async function handleFinish(values: WebhookFormValues) {
    setSubmitting(true)
    try {
      if (isNew) {
        await createWebhook(channel, values.url, values.secret ?? '', values.enabled)
      } else {
        await updateWebhook(record.id, values.url, values.secret ?? '', values.enabled)
      }
      onDone()
    } catch (err) {
      message.error(t('settingsChannelActionFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('settingsChannelModalCreateTitle') : t('settingsChannelModalEditTitle')}
      open
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      okText={t('usersSave')}
      cancelText={t('usersCancel')}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={isNew ? { enabled: true } : { url: record.url, secret: record.secret, enabled: record.enabled }}
        onFinish={handleFinish}
      >
        <Form.Item label={t('settingsChannelColURL')} name="url" rules={[{ required: true }]}>
          <Input autoFocus />
        </Form.Item>
        {showSecret && (
          <Form.Item label={t('settingsDingTalkSecret')} name="secret" extra={t('settingsDingTalkSecretHint')}>
            <Input.Password />
          </Form.Item>
        )}
        <Form.Item label={t('settingsChannelColEnabled')} name="enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}

function AssetTagsTab({ t }: { t: T }) {
  const [tags, setTags] = useState<IPTagRecord[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(10)
  const [q, setQ] = useState('')
  const [editing, setEditing] = useState<IPTagRecord | 'new' | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getIPTagsPaged(page, pageSize, q)
      setTags(res.tags)
      setTotal(res.total)
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoading(false)
    }
  }, [t, page, pageSize, q])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function handleDelete(tag: IPTagRecord) {
    try {
      await deleteIPTag(tag.id)
      message.success(t('ipTagsDeleteButton') + ' ' + tag.value)
      refresh()
    } catch (err) {
      message.error(t('ipTagsActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  const columns: ColumnsType<IPTagRecord> = [
    { title: t('ipTagsColKind'), dataIndex: 'kind', width: 90, render: (k: IPTagKind) => (k === 'cidr' ? t('ipTagsKindCIDR') : t('ipTagsKindIP')) },
    { title: t('ipTagsColValue'), dataIndex: 'value' },
    { title: t('ipTagsColLabel'), dataIndex: 'label' },
    {
      title: t('ipTagsColActions'),
      key: 'actions',
      width: 140,
      render: (_, tag) => (
        <>
          <Button type="link" size="small" onClick={() => setEditing(tag)}>
            {t('ipTagsEditButton')}
          </Button>
          <Popconfirm title={t('ipTagsDeleteConfirm', { value: tag.value })} onConfirm={() => handleDelete(tag)} okText={t('ipTagsDeleteButton')} cancelText={t('ipTagsCancel')} okButtonProps={{ danger: true }}>
            <Button type="link" size="small" danger>
              {t('ipTagsDeleteButton')}
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ]

  return (
    <>
      <p className="settings-section-desc">{t('ipTagsSectionDesc')}</p>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
        <Input.Search
          placeholder={t('ipTagsSearchPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Button type="primary" size="small" onClick={() => setEditing('new')}>
          {t('ipTagsCreateButton')}
        </Button>
      </div>
      <div className="compact-table">
        <Table
          rowKey="id"
          columns={columns}
          dataSource={tags}
          loading={loading}
          size="small"
          pagination={tablePagination(page, pageSize, total, (p, ps) => {
            setPage(p)
            setPageSize(ps)
          }, t)}
        />
      </div>

      {editing && (
        <IPTagFormModal
          t={t}
          mode={editing}
          onDone={() => {
            setEditing(null)
            refresh()
          }}
          onCancel={() => setEditing(null)}
        />
      )}
    </>
  )
}

interface IPTagFormValues {
  kind: IPTagKind
  value: string
  label: string
}

function IPTagFormModal({ t, mode, onDone, onCancel }: { t: T; mode: IPTagRecord | 'new'; onDone: () => void; onCancel: () => void }) {
  const isNew = mode === 'new'
  const [form] = Form.useForm<IPTagFormValues>()
  const [submitting, setSubmitting] = useState(false)

  async function handleFinish(values: IPTagFormValues) {
    setSubmitting(true)
    try {
      if (isNew) {
        await createIPTag(values.kind, values.value, values.label)
      } else {
        await updateIPTag(mode.id, values.label)
      }
      onDone()
    } catch (err) {
      message.error(t('ipTagsActionFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('ipTagsCreateTitle') : t('ipTagsEditTitle')}
      open
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      okText={t('ipTagsSave')}
      cancelText={t('ipTagsCancel')}
    >
      <Form form={form} layout="vertical" initialValues={isNew ? { kind: 'ip' } : { kind: mode.kind, value: mode.value, label: mode.label }} onFinish={handleFinish}>
        <Form.Item label={t('ipTagsColKind')} name="kind" rules={[{ required: true }]}>
          <Select
            disabled={!isNew}
            options={[
              { value: 'ip', label: t('ipTagsKindIP') },
              { value: 'cidr', label: t('ipTagsKindCIDR') },
            ]}
          />
        </Form.Item>
        <Form.Item label={t('ipTagsColValue')} name="value" rules={[{ required: true }]} extra={isNew ? t('ipTagsValueHint') : undefined}>
          <Input disabled={!isNew} placeholder={isNew ? '203.0.113.10 / 203.0.113.0/24' : undefined} />
        </Form.Item>
        <Form.Item label={t('ipTagsColLabel')} name="label" rules={[{ required: true }]}>
          <Input placeholder={t('ipTagsLabelPlaceholder')} autoFocus={!isNew} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

function PortMappingsTab({ t }: { t: T }) {
  const [mappings, setMappings] = useState<PortMappingRecord[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(10)
  const [q, setQ] = useState('')
  const [editing, setEditing] = useState<PortMappingRecord | 'new' | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const res = await getPortMappingsPaged(page, pageSize, q)
      setMappings(res.mappings)
      setTotal(res.total)
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoading(false)
    }
  }, [t, page, pageSize, q])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function handleDelete(m: PortMappingRecord) {
    try {
      await deletePortMapping(m.port)
      message.success(t('portMappingsDeleteButton') + ' ' + m.port)
      refresh()
    } catch (err) {
      message.error(t('portMappingsActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  const columns: ColumnsType<PortMappingRecord> = [
    { title: t('portMappingsColPort'), dataIndex: 'port', width: 100 },
    { title: t('portMappingsColService'), dataIndex: 'service' },
    {
      title: t('portMappingsColActions'),
      key: 'actions',
      width: 140,
      render: (_, m) => (
        <>
          <Button type="link" size="small" onClick={() => setEditing(m)}>
            {t('portMappingsEditButton')}
          </Button>
          <Popconfirm
            title={t('portMappingsDeleteConfirm', { port: m.port })}
            onConfirm={() => handleDelete(m)}
            okText={t('portMappingsDeleteButton')}
            cancelText={t('ipTagsCancel')}
            okButtonProps={{ danger: true }}
          >
            <Button type="link" size="small" danger>
              {t('portMappingsDeleteButton')}
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ]

  return (
    <>
      <p className="settings-section-desc">{t('portMappingsSectionDesc')}</p>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
        <Input.Search
          placeholder={t('portMappingsSearchPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Button type="primary" size="small" onClick={() => setEditing('new')}>
          {t('portMappingsCreateButton')}
        </Button>
      </div>
      <div className="compact-table">
        <Table
          rowKey="port"
          columns={columns}
          dataSource={mappings}
          loading={loading}
          size="small"
          pagination={tablePagination(page, pageSize, total, (p, ps) => {
            setPage(p)
            setPageSize(ps)
          }, t)}
        />
      </div>

      {editing && (
        <PortMappingFormModal
          t={t}
          mode={editing}
          onDone={() => {
            setEditing(null)
            refresh()
          }}
          onCancel={() => setEditing(null)}
        />
      )}
    </>
  )
}

interface PortMappingFormValues {
  port: number
  service: string
}

function PortMappingFormModal({ t, mode, onDone, onCancel }: { t: T; mode: PortMappingRecord | 'new'; onDone: () => void; onCancel: () => void }) {
  const isNew = mode === 'new'
  const [form] = Form.useForm<PortMappingFormValues>()
  const [submitting, setSubmitting] = useState(false)

  async function handleFinish(values: PortMappingFormValues) {
    setSubmitting(true)
    try {
      if (isNew) {
        await createPortMapping(values.port, values.service)
      } else {
        await updatePortMapping(mode.port, values.service)
      }
      onDone()
    } catch (err) {
      message.error(t('portMappingsActionFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('portMappingsCreateTitle') : t('portMappingsEditTitle')}
      open
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      okText={t('ipTagsSave')}
      cancelText={t('ipTagsCancel')}
    >
      <Form form={form} layout="vertical" initialValues={isNew ? {} : { port: mode.port, service: mode.service }} onFinish={handleFinish}>
        <Form.Item label={t('portMappingsColPort')} name="port" rules={[{ required: true }]}>
          <InputNumber disabled={!isNew} min={1} max={65535} style={{ width: '100%' }} autoFocus={isNew} />
        </Form.Item>
        <Form.Item label={t('portMappingsColService')} name="service" rules={[{ required: true }]}>
          <Input placeholder={t('portMappingsServicePlaceholder')} autoFocus={!isNew} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

function MCPServersTab({ t }: { t: T }) {
  const [servers, setServers] = useState<MCPServerRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<MCPServerRecord | 'new' | null>(null)
  const [viewingTools, setViewingTools] = useState<MCPServerRecord | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setServers((await listMCPServers()) ?? [])
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function handleDelete(s: MCPServerRecord) {
    try {
      await deleteMCPServer(s.id)
      message.success(t('mcpDeleteButton') + ' ' + s.name)
      refresh()
    } catch (err) {
      message.error(t('mcpActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  const statusColor: Record<MCPConnStatus, string> = { connected: 'success', error: 'error', disconnected: 'default' }

  const columns: ColumnsType<MCPServerRecord> = [
    { title: t('mcpColName'), dataIndex: 'name', width: 160, ellipsis: true },
    { title: t('mcpColTransport'), dataIndex: 'transport', width: 90, render: (v: MCPServerTransport) => v.toUpperCase() },
    { title: t('mcpColEnabled'), dataIndex: 'enabled', width: 80, render: (v: boolean) => (v ? t('mcpEnabled') : t('mcpDisabled')) },
    {
      title: t('mcpColConnStatus'),
      dataIndex: 'status',
      width: 110,
      render: (v: MCPConnStatus, s) => {
        const tag = <Tag color={statusColor[v]}>{t(v === 'connected' ? 'mcpStatusConnected' : v === 'error' ? 'mcpStatusError' : 'mcpStatusDisconnected')}</Tag>
        return v === 'error' && s.statusError ? <span title={s.statusError}>{tag}</span> : tag
      },
    },
    {
      title: t('mcpColActions'),
      key: 'actions',
      width: 200,
      render: (_, s) => (
        <>
          {s.status === 'connected' && (
            <Button type="link" size="small" onClick={() => setViewingTools(s)}>
              {t('mcpToolsButton')}
            </Button>
          )}
          <Button type="link" size="small" onClick={() => setEditing(s)}>
            {t('ipTagsEditButton')}
          </Button>
          <Popconfirm title={t('mcpDeleteConfirm', { name: s.name })} onConfirm={() => handleDelete(s)} okText={t('ipTagsDeleteButton')} cancelText={t('ipTagsCancel')} okButtonProps={{ danger: true }}>
            <Button type="link" size="small" danger>
              {t('ipTagsDeleteButton')}
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ]

  return (
    <>
      <p className="settings-section-desc">{t('mcpSectionDesc')}</p>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <Button type="primary" size="small" onClick={() => setEditing('new')}>
          {t('mcpCreateButton')}
        </Button>
      </div>
      <div className="compact-table">
        <Table rowKey="id" columns={columns} dataSource={servers} loading={loading} size="small" pagination={false} tableLayout="fixed" />
      </div>

      {editing && (
        <MCPServerFormModal
          t={t}
          mode={editing}
          onDone={() => {
            setEditing(null)
            refresh()
          }}
          onCancel={() => setEditing(null)}
        />
      )}

      {viewingTools && <MCPToolsModal t={t} server={viewingTools} onClose={() => setViewingTools(null)} />}
    </>
  )
}

function MCPToolsModal({ t, server, onClose }: { t: T; server: MCPServerRecord; onClose: () => void }) {
  const [tools, setTools] = useState<MCPToolInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    listMCPServerTools(server.id)
      .then((res) => {
        if (!cancelled) setTools(res.tools ?? [])
      })
      .catch((err) => {
        if (!cancelled) message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [server.id, t])

  const columns: ColumnsType<MCPToolInfo> = [
    { title: t('mcpToolsColName'), dataIndex: 'name', width: 200 },
    { title: t('mcpToolsColDescription'), dataIndex: 'description' },
  ]

  return (
    <Modal title={t('mcpToolsTitle', { name: server.name })} open onCancel={onClose} footer={null} width={640}>
      <Table
        rowKey="name"
        columns={columns}
        dataSource={tools}
        loading={loading}
        size="small"
        pagination={{ pageSize: 10, hideOnSinglePage: true, showTotal: (total) => t('tableTotal', { total }) }}
        locale={{ emptyText: t('noData') }}
      />
    </Modal>
  )
}

interface MCPServerFormValues {
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
}

function authFieldsOf(values: MCPServerFormValues) {
  return { authType: values.authType ?? 'none', authUsername: values.authUsername ?? '', authPassword: values.authPassword ?? '', authToken: values.authToken ?? '' }
}

function MCPServerFormModal({ t, mode, onDone, onCancel }: { t: T; mode: MCPServerRecord | 'new'; onDone: () => void; onCancel: () => void }) {
  const isNew = mode === 'new'
  const [form] = Form.useForm<MCPServerFormValues>()
  const [submitting, setSubmitting] = useState(false)
  const [testing, setTesting] = useState(false)
  const transport = Form.useWatch('transport', form) ?? (isNew ? 'http' : mode.transport)
  const authType = Form.useWatch('authType', form) ?? (isNew ? 'none' : mode.authType)

  async function handleFinish(values: MCPServerFormValues) {
    setSubmitting(true)
    try {
      const auth = authFieldsOf(values)
      if (isNew) {
        await createMCPServer(values.name, values.transport, values.endpoint ?? '', values.command ?? '', values.args ?? '', values.enabled, auth)
      } else {
        await updateMCPServer(mode.id, values.name, values.endpoint ?? '', values.command ?? '', values.args ?? '', values.enabled, auth)
      }
      onDone()
    } catch (err) {
      message.error(t('mcpActionFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setSubmitting(false)
    }
  }

  async function handleTest() {
    const values = form.getFieldsValue()
    setTesting(true)
    try {
      const res = await testMCPServer(values.transport, values.endpoint ?? '', values.command ?? '', values.args ?? '', authFieldsOf(values))
      message.success(t('mcpTestSuccess', { count: res.tools.length }) + (res.tools.length ? '：' + res.tools.join(', ') : ''))
    } catch (err) {
      message.error(t('mcpTestFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setTesting(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('mcpCreateTitle') : t('mcpEditTitle')}
      open
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      okText={t('ipTagsSave')}
      cancelText={t('ipTagsCancel')}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={
          isNew
            ? { transport: 'http', enabled: true, authType: 'none' }
            : {
                name: mode.name,
                transport: mode.transport,
                endpoint: mode.endpoint,
                command: mode.command,
                args: mode.args,
                enabled: mode.enabled,
                authType: mode.authType,
                authUsername: mode.authUsername,
                authPassword: mode.authPassword,
                authToken: mode.authToken,
              }
        }
        onFinish={handleFinish}
      >
        <Form.Item label={t('mcpColName')} name="name" rules={[{ required: true }]}>
          <Input placeholder={t('mcpNamePlaceholder')} autoFocus />
        </Form.Item>
        <Form.Item label={t('mcpColTransport')} name="transport" rules={[{ required: true }]}>
          <Select
            disabled={!isNew}
            options={[
              { value: 'http', label: 'HTTP' },
              { value: 'stdio', label: 'stdio' },
            ]}
          />
        </Form.Item>
        {transport === 'http' ? (
          <Form.Item label={t('mcpColEndpoint')} name="endpoint" rules={[{ required: true }]} extra={t('mcpEndpointHint')}>
            <Input placeholder="https://example.com/mcp" />
          </Form.Item>
        ) : (
          <>
            <Form.Item label={t('mcpColCommand')} name="command" rules={[{ required: true }]}>
              <Input placeholder="npx" />
            </Form.Item>
            <Form.Item label={t('mcpColArgs')} name="args" extra={t('mcpArgsHint')}>
              <Input placeholder='["-y", "some-mcp-server"]' />
            </Form.Item>
          </>
        )}
        {transport === 'http' && (
          <>
            <Form.Item label={t('mcpColAuthType')} name="authType">
              <Select
                options={[
                  { value: 'none', label: t('mcpAuthNone') },
                  { value: 'basic', label: t('mcpAuthBasic') },
                  { value: 'bearer', label: t('mcpAuthBearer') },
                ]}
              />
            </Form.Item>
            {authType === 'basic' && (
              <>
                <Form.Item label={t('mcpAuthUsername')} name="authUsername" rules={[{ required: true }]}>
                  <Input autoComplete="off" />
                </Form.Item>
                <Form.Item label={t('mcpAuthPassword')} name="authPassword" rules={[{ required: true }]}>
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
              </>
            )}
            {authType === 'bearer' && (
              <Form.Item label={t('mcpAuthToken')} name="authToken" rules={[{ required: true }]}>
                <Input.Password placeholder={t('mcpAuthTokenPlaceholder')} autoComplete="new-password" />
              </Form.Item>
            )}
          </>
        )}
        <Form.Item label={t('mcpColEnabled')} name="enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item>
          <Button onClick={handleTest} loading={testing}>
            {t('mcpTestButton')}
          </Button>
        </Form.Item>
      </Form>
    </Modal>
  )
}
