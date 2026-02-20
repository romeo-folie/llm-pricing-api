# Debug Deploy

Check all deployment platforms for failures and produce a consolidated status report.

## Steps

Run all three checks in parallel:

### 1. GitHub Actions

```bash
gh run list --limit 5 --json status,conclusion,name,headBranch,createdAt,databaseId
```

If any run has `conclusion` != `success`, fetch its failed logs:

```bash
gh run view <databaseId> --log-failed
```

### 2. Railway

Check build logs for both services using the Railway MCP tools:

- `get-logs` with `service="llm-pricing-api"`, `logType="build"`, `lines=50`
- `get-logs` with `service="llm-pricing-worker"`, `logType="build"`, `lines=50`

If builds succeeded, also check deploy logs:

- `get-logs` with `service="llm-pricing-api"`, `logType="deploy"`, `lines=30`
- `get-logs` with `service="llm-pricing-worker"`, `logType="deploy"`, `lines=30`

Look for: build errors, health check failures, panic/fatal messages, connection refused errors.

### 3. Vercel

```bash
vercel ls
```

If any deployment shows `● Error`, inspect it:

```bash
vercel inspect <deployment-url>
```

If the build output is empty (`Builds: . [0ms]`), try reproducing locally:

```bash
cd frontend && npm run build
```

## Output Format

Present results as a table:

| Platform | Service | Status | Details |
|----------|---------|--------|---------|
| GitHub Actions | CI | ... | ... |
| Railway | llm-pricing-api | ... | ... |
| Railway | llm-pricing-worker | ... | ... |
| Vercel | frontend | ... | ... |

For any failures, include:
- **Root cause** — what specifically failed
- **Fix** — concrete action to resolve it
- **Workaround** — if available, a temporary bypass

## Triage Priority

1. GitHub Actions (CI) — code-level issues
2. Railway — infrastructure/runtime issues
3. Vercel — frontend build issues

If all platforms are green, report: "All deployments healthy."
