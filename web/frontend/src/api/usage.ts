// Usage API — token usage statistics

export interface UsageTotals {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  request_count: number
}

export interface DailyUsage {
  date: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  request_count: number
}

export interface ModelUsage {
  model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  request_count: number
}

export interface UsageSummary {
  totals: UsageTotals
  daily: DailyUsage[]
  by_model: ModelUsage[]
}

export async function getUsageSummary(days: number = 30): Promise<UsageSummary> {
  const res = await fetch(`/api/usage/summary?days=${days}`)
  if (!res.ok) {
    throw new Error(`Failed to fetch usage summary: ${res.status}`)
  }
  return res.json()
}
