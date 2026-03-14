interface PaginationProps {
  currentPage: number
  totalPages: number
  /** All current search params to preserve when navigating between pages. */
  searchParams: Record<string, string>
}

/**
 * Server-compatible pagination using <a> links.
 * Preserves all existing search params (filters) when navigating.
 */
export default function Pagination({
  currentPage,
  totalPages,
  searchParams,
}: PaginationProps) {
  if (totalPages <= 1) return null

  function buildHref(page: number): string {
    const params = new URLSearchParams(searchParams)
    params.set("page", String(page))
    return `?${params.toString()}`
  }

  // Build page numbers: current +/- 2, with ellipsis for gaps
  const pages: (number | "ellipsis")[] = []
  const range = 2

  // Always show page 1
  pages.push(1)

  const rangeStart = Math.max(2, currentPage - range)
  const rangeEnd = Math.min(totalPages - 1, currentPage + range)

  if (rangeStart > 2) pages.push("ellipsis")
  for (let i = rangeStart; i <= rangeEnd; i++) pages.push(i)
  if (rangeEnd < totalPages - 1) pages.push("ellipsis")

  // Always show last page (if more than 1 page)
  if (totalPages > 1) pages.push(totalPages)

  const linkBase: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    minWidth: "32px",
    height: "32px",
    padding: "0 8px",
    border: "1px solid var(--border)",
    backgroundColor: "var(--bg)",
    color: "var(--muted)",
    textDecoration: "none",
    cursor: "pointer",
    transition: "background-color 0.15s, color 0.15s, border-color 0.15s",
  }

  const activeStyle: React.CSSProperties = {
    ...linkBase,
    backgroundColor: "var(--accent)",
    borderColor: "var(--accent)",
    color: "white",
  }

  const disabledStyle: React.CSSProperties = {
    ...linkBase,
    opacity: 0.4,
    pointerEvents: "none",
    cursor: "default",
  }

  return (
    <nav
      aria-label="Pagination"
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "4px",
        marginTop: "24px",
      }}
    >
      {/* Previous */}
      {currentPage <= 1 ? (
        <span className="font-outfit text-sm" style={disabledStyle}>
          Prev
        </span>
      ) : (
        <a
          href={buildHref(currentPage - 1)}
          className="font-outfit text-sm"
          style={linkBase}
        >
          Prev
        </a>
      )}

      {/* Page numbers */}
      {pages.map((p, i) => {
        if (p === "ellipsis") {
          return (
            <span
              key={`e${i}`}
              className="font-orbitron text-xs"
              style={{
                minWidth: "32px",
                textAlign: "center",
                color: "var(--dim)",
              }}
            >
              &hellip;
            </span>
          )
        }

        const isActive = p === currentPage
        return (
          <a
            key={p}
            href={buildHref(p)}
            className="font-orbitron text-xs"
            style={isActive ? activeStyle : linkBase}
            aria-current={isActive ? "page" : undefined}
          >
            {p}
          </a>
        )
      })}

      {/* Next */}
      {currentPage >= totalPages ? (
        <span className="font-outfit text-sm" style={disabledStyle}>
          Next
        </span>
      ) : (
        <a
          href={buildHref(currentPage + 1)}
          className="font-outfit text-sm"
          style={linkBase}
        >
          Next
        </a>
      )}
    </nav>
  )
}
