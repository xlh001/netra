import { Pagination } from 'antd'
import type { StringKey } from '../i18n/strings'

type T = (key: StringKey, vars?: Record<string, string | number>) => string

export function DataPagination({
  page,
  pageSize,
  total,
  onPageChange,
  t,
  sequentialOnly = false,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number, pageSize: number) => void
  t: T
  sequentialOnly?: boolean
}) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  return (
    <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 12 }}>
      <Pagination
        current={page + 1}
        pageSize={pageSize}
        total={total}
        showSizeChanger={!sequentialOnly}
        pageSizeOptions={[10, 20, 50, 100]}
        showQuickJumper={!sequentialOnly}
        showTotal={(t_total) =>
          sequentialOnly
            ? `${t('tableTotal', { total: t_total })} · ${t('tablePageOfTotal', { page: page + 1, totalPages })}`
            : t('tableTotal', { total: t_total })
        }
        itemRender={sequentialOnly ? (_page, type, originalElement) => (type === 'prev' || type === 'next' ? originalElement : null) : undefined}
        onChange={(p, ps) => onPageChange(p - 1, ps)}
      />
    </div>
  )
}
