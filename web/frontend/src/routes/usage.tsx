import { createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { getUsageSummary, type UsageSummary } from "@/api/usage"
import { PageHeader } from "@/components/page-header"

export const Route = createFileRoute("/usage")({
  component: UsagePage,
})

type Period = 7 | 30 | 90

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M"
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K"
  return n.toString()
}

function UsagePage() {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<Period>(30)
  const [data, setData] = useState<UsageSummary | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let mounted = true
    setLoading(true)
    getUsageSummary(period)
      .then((res) => {
        if (mounted) setData(res)
      })
      .catch(() => {})
      .finally(() => {
        if (mounted) setLoading(false)
      })
    return () => {
      mounted = false
    }
  }, [period])

  // Compute "today" totals from the daily array
  const todayStr = new Date().toISOString().slice(0, 10)
  const todayData = data?.daily.find((d) => d.date === todayStr)
  const todayTokens = todayData?.total_tokens ?? 0

  // Compute 7-day totals
  const sevenDaysAgo = new Date()
  sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7)
  const sevenDayStr = sevenDaysAgo.toISOString().slice(0, 10)
  const last7Tokens =
    data?.daily
      .filter((d) => d.date >= sevenDayStr)
      .reduce((s, d) => s + d.total_tokens, 0) ?? 0

  const maxDaily = data
    ? Math.max(...data.daily.map((d) => d.total_tokens), 1)
    : 1

  const periods: Period[] = [7, 30, 90]

  return (
    <div className="flex h-full flex-col">
      <PageHeader title={t("navigation.usage")} />

      <div className="flex flex-1 flex-col overflow-auto p-4 sm:p-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">
              {t("navigation.usage")}
            </h1>
            <p className="text-muted-foreground mt-1 text-sm">
              {t("pages.usage.description")}
            </p>
          </div>
          <div className="flex gap-1 rounded-lg border p-1">
            {periods.map((p) => (
              <button
                key={p}
                onClick={() => setPeriod(p)}
                className={`rounded-md px-3 py-1 text-sm font-medium transition-colors ${
                  period === p
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted"
                }`}
              >
                {p}D
              </button>
            ))}
          </div>
        </div>

        {loading && !data ? (
          <div className="text-muted-foreground py-12 text-center text-sm">
            {t("labels.loading")}
          </div>
        ) : (
          <>
            {/* Summary cards */}
            <div className="mb-8 grid grid-cols-1 gap-4 sm:grid-cols-3">
              <div className="rounded-lg border p-4">
                <p className="text-muted-foreground text-xs font-medium uppercase">
                  {t("pages.usage.today")}
                </p>
                <p className="mt-1 text-2xl font-bold">
                  {formatNumber(todayTokens)}
                </p>
                <p className="text-muted-foreground text-xs">tokens</p>
              </div>
              <div className="rounded-lg border p-4">
                <p className="text-muted-foreground text-xs font-medium uppercase">
                  {t("pages.usage.last_7_days")}
                </p>
                <p className="mt-1 text-2xl font-bold">
                  {formatNumber(last7Tokens)}
                </p>
                <p className="text-muted-foreground text-xs">tokens</p>
              </div>
              <div className="rounded-lg border p-4">
                <p className="text-muted-foreground text-xs font-medium uppercase">
                  {t("pages.usage.period_total", { days: period })}
                </p>
                <p className="mt-1 text-2xl font-bold">
                  {formatNumber(data?.totals.total_tokens ?? 0)}
                </p>
                <p className="text-muted-foreground text-xs">
                  {data?.totals.request_count ?? 0} {t("pages.usage.requests")}
                </p>
              </div>
            </div>

            {/* Daily bar chart */}
            <div className="mb-8">
              <h2 className="mb-3 text-sm font-semibold">
                {t("pages.usage.daily_usage")}
              </h2>
              {data && data.daily.length > 0 ? (
                <div className="flex items-end gap-[2px] overflow-x-auto rounded-lg border p-4">
                  {data.daily.map((d) => {
                    const h = Math.max(
                      (d.total_tokens / maxDaily) * 160,
                      2,
                    )
                    return (
                      <div
                        key={d.date}
                        className="group relative flex min-w-[8px] flex-1 flex-col items-center"
                      >
                        <div className="absolute -top-8 left-1/2 z-10 hidden -translate-x-1/2 whitespace-nowrap rounded bg-black/80 px-2 py-1 text-xs text-white group-hover:block">
                          {d.date}: {formatNumber(d.total_tokens)}
                        </div>
                        <div
                          className="bg-primary/70 hover:bg-primary w-full rounded-t transition-colors"
                          style={{ height: `${h}px` }}
                        />
                      </div>
                    )
                  })}
                </div>
              ) : (
                <div className="text-muted-foreground rounded-lg border p-8 text-center text-sm">
                  {t("pages.usage.no_data")}
                </div>
              )}
            </div>

            {/* Model breakdown table */}
            <div>
              <h2 className="mb-3 text-sm font-semibold">
                {t("pages.usage.by_model")}
              </h2>
              {data && data.by_model.length > 0 ? (
                <div className="overflow-hidden rounded-lg border">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="bg-muted/50 border-b">
                        <th className="px-4 py-2 text-left font-medium">
                          {t("pages.usage.model")}
                        </th>
                        <th className="px-4 py-2 text-right font-medium">
                          {t("pages.usage.prompt")}
                        </th>
                        <th className="px-4 py-2 text-right font-medium">
                          {t("pages.usage.completion")}
                        </th>
                        <th className="px-4 py-2 text-right font-medium">
                          {t("pages.usage.total")}
                        </th>
                        <th className="px-4 py-2 text-right font-medium">
                          {t("pages.usage.requests")}
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.by_model.map((m) => (
                        <tr key={m.model} className="border-b last:border-0">
                          <td className="px-4 py-2 font-mono text-xs">
                            {m.model}
                          </td>
                          <td className="px-4 py-2 text-right">
                            {formatNumber(m.prompt_tokens)}
                          </td>
                          <td className="px-4 py-2 text-right">
                            {formatNumber(m.completion_tokens)}
                          </td>
                          <td className="px-4 py-2 text-right font-medium">
                            {formatNumber(m.total_tokens)}
                          </td>
                          <td className="px-4 py-2 text-right">
                            {m.request_count}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="text-muted-foreground rounded-lg border p-8 text-center text-sm">
                  {t("pages.usage.no_data")}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
