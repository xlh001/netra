import type { TablePaginationConfig } from 'antd/es/table'
import type { StringKey } from '../i18n/strings'

export function tablePagination(
  page: number,
  pageSize: number,
  total: number,
  onChange: (page: number, pageSize: number) => void,
  t: (key: StringKey, vars?: Record<string, string | number>) => string,
  // Jumping straight to a distant page (page-number buttons, quick-jumper,
  // "«"/"»" first/last) forces the backend to sort near the requested
  // offset's worth of rows -- fine for low-cardinality dimensions (IPs,
  // ports, domains: at most a few thousand groups), but for the raw
  // five-tuple Flows view a single click on "last page" can mean sorting
  // millions of rows (confirmed in production: offset near 12.5M turned a
  // sub-second query into 83s). Sequential-only next/prev sidesteps that
  // entirely by keeping the offset small and predictable one step at a
  // time -- pass true from the one caller that actually needs it (Flows).
  sequentialOnly = false,
): TablePaginationConfig {
  const config: TablePaginationConfig = {
    current: page + 1,
    pageSize,
    total,
    showSizeChanger: true,
    pageSizeOptions: [10, 20, 50, 100],
    showQuickJumper: !sequentialOnly,
    showTotal: (t_total) => t('tableTotal', { total: t_total }),
    onChange: (p, ps) => onChange(p - 1, ps),
  }
  if (sequentialOnly) {
    const totalPages = Math.max(1, Math.ceil(total / pageSize))
    config.showTotal = (t_total) => `${t('tableTotal', { total: t_total })} · 第 ${page + 1} / ${totalPages} 页`
    config.itemRender = (_page, type, originalElement) => {
      if (type === 'prev' || type === 'next') return originalElement
      return null
    }
  }
  return config
}
