import { Pagination, Tooltip } from 'antd'
import { InfoCircleOutlined } from '@ant-design/icons'
import type { StringKey } from '../i18n/strings'

type T = (key: StringKey, vars?: Record<string, string | number>) => string

export function DataPagination({
  page,
  pageSize,
  total,
  onPageChange,
  t,
  capRows,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number, pageSize: number) => void
  t: T
  capRows?: number
}) {
  const capped = capRows != null && total > capRows
  const effectiveTotal = capped ? capRows! : total
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8, marginTop: 12 }}>
      {capped && (
        <Tooltip title={t('tableTopRankedHint', { cap: capRows! })}>
          <InfoCircleOutlined style={{ color: 'var(--amber)', cursor: 'help' }} />
        </Tooltip>
      )}
      <Pagination
        current={page + 1}
        pageSize={pageSize}
        total={effectiveTotal}
        showSizeChanger
        pageSizeOptions={[10, 20, 50, 100]}
        showQuickJumper={false}
        showTotal={() => (capped ? t('tableTopRanked', { cap: capRows! }) : t('tableTotal', { total }))}
        onChange={(p, ps) => onPageChange(p - 1, ps)}
      />
    </div>
  )
}
