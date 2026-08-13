import { useCallback, useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Popconfirm, Select, Switch, Table, Tag, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useAuth } from '../auth/context'
import { useT } from '../i18n/context'
import { createUser, deleteUser, listUsers, updateUser } from '../api/client'
import type { Role, UserRecord } from '../api/types'

export function UserManagement() {
  const t = useT()
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<UserRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<UserRecord | 'new' | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setUsers((await listUsers()) ?? [])
    } catch (err) {
      message.error(t('fetchFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    refresh()
  }, [refresh])

  async function handleDelete(u: UserRecord) {
    try {
      await deleteUser(u.id)
      message.success(t('usersDeleteButton') + ' ' + u.username)
      refresh()
    } catch (err) {
      message.error(t('usersActionFailed') + (err instanceof Error ? err.message : String(err)))
    }
  }

  const columns: ColumnsType<UserRecord> = [
    { title: t('usersColUsername'), dataIndex: 'username' },
    { title: t('usersColRole'), dataIndex: 'role', render: (r: Role) => (r === 'admin' ? t('usersRoleAdmin') : t('usersRoleNormal')) },
    {
      title: t('usersColLongLived'),
      dataIndex: 'longLived',
      render: (v: boolean) => (v ? <Tag color="cyan">{t('usersLongLived')}</Tag> : null),
    },
    { title: t('usersColDescription'), dataIndex: 'description', ellipsis: true },
    { title: t('usersColCreated'), dataIndex: 'createdAt', render: (v: string) => new Date(v).toLocaleString() },
    {
      title: t('usersColActions'),
      key: 'actions',
      render: (_, u) => (
        <>
          <Button type="link" size="small" onClick={() => setEditing(u)}>
            {t('usersEditButton')}
          </Button>
          <Popconfirm
            title={t('usersDeleteConfirm', { username: u.username })}
            onConfirm={() => handleDelete(u)}
            disabled={u.username === currentUser?.username}
            okText={t('usersDeleteButton')}
            cancelText={t('usersCancel')}
            okButtonProps={{ danger: true }}
          >
            <Button type="link" size="small" danger disabled={u.username === currentUser?.username}>
              {t('usersDeleteButton')}
            </Button>
          </Popconfirm>
        </>
      ),
    },
  ]

  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('usersPageTitle')}</span>
        </h2>
        <Button type="primary" size="small" onClick={() => setEditing('new')}>
          {t('usersCreateButton')}
        </Button>
      </div>
      <div className="panel-body compact-table">
        <Table rowKey="id" columns={columns} dataSource={users} loading={loading} pagination={false} size="small" />
      </div>

      {editing && (
        <UserFormModal
          mode={editing}
          onDone={() => {
            setEditing(null)
            refresh()
          }}
          onCancel={() => setEditing(null)}
        />
      )}
    </div>
  )
}

interface UserFormValues {
  username: string
  password: string
  role: Role
  description: string
  longLived: boolean
}

function UserFormModal({ mode, onDone, onCancel }: { mode: UserRecord | 'new'; onDone: () => void; onCancel: () => void }) {
  const t = useT()
  const isNew = mode === 'new'
  const [form] = Form.useForm<UserFormValues>()
  const [submitting, setSubmitting] = useState(false)

  async function handleFinish(values: UserFormValues) {
    setSubmitting(true)
    try {
      if (isNew) {
        await createUser(values.username, values.password, values.role, values.description || '', values.longLived)
      } else {
        await updateUser(mode.id, values.role, values.password || '', values.description || '', values.longLived)
      }
      onDone()
    } catch (err) {
      message.error(t('usersActionFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('usersCreateTitle') : t('usersEditTitle')}
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
        initialValues={
          isNew
            ? { role: 'normal', longLived: false }
            : { username: mode.username, role: mode.role, description: mode.description, longLived: mode.longLived }
        }
        onFinish={handleFinish}
      >
        {isNew && (
          <Form.Item label={t('usersNewUsername')} name="username" rules={[{ required: true }]}>
            <Input autoFocus />
          </Form.Item>
        )}
        <Form.Item
          label={t('usersNewPassword')}
          name="password"
          rules={isNew ? [{ required: true, min: 8 }] : [{ min: 8 }]}
          extra={isNew ? undefined : t('usersLeavePasswordBlank')}
        >
          <Input.Password />
        </Form.Item>
        <Form.Item label={t('usersColRole')} name="role" rules={[{ required: true }]}>
          <Select
            options={[
              { value: 'normal', label: t('usersRoleNormal') },
              { value: 'admin', label: t('usersRoleAdmin') },
            ]}
          />
        </Form.Item>
        <Form.Item label={t('usersColDescription')} name="description">
          <Input placeholder={t('usersDescriptionPlaceholder')} />
        </Form.Item>
        <Form.Item label={t('usersLongLived')} name="longLived" valuePropName="checked" extra={t('usersLongLivedHint')}>
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}
