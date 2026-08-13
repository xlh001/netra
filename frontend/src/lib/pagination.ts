
export function pagesFor(total: number, maxPerPage: number): { pages: number; perPage: number } {
  const pages = Math.max(1, Math.ceil(total / maxPerPage))
  const perPage = pages > 0 ? Math.ceil(total / pages) : maxPerPage
  return { pages, perPage }
}

export const ROTATE_MS = 5500
