import type { TablePaginationConfig } from 'antd/es/table'
import type { StringKey } from '../i18n/strings'

export function tablePagination(
  page: number,
  pageSize: number,
  total: number,
  onChange: (page: number, pageSize: number) => void,
  t: (key: StringKey, vars?: Record<string, string | number>) => string,
): TablePaginationConfig {
  return {
    current: page + 1,
    pageSize,
    total,
    showSizeChanger: true,
    pageSizeOptions: [10, 20, 50, 100],
    showQuickJumper: true,
    showTotal: (t_total) => t('tableTotal', { total: t_total }),
    onChange: (p, ps) => onChange(p - 1, ps),
  }
}
