// API client for onboarding wizard.

export interface OnboardStatus {
  needs_onboard: boolean
  version: string
  go_version: string
  os: string
  arch: string
}

export interface VerifyResult {
  valid: boolean
  error: string
}

export interface CompleteResult {
  success: boolean
  config_path?: string
  error?: string
}

export interface CompleteRequest {
  model_name: string
  model: string
  api_base: string
  api_key: string
  auth_method?: string
  channel_type?: string
  channel_data?: Record<string, string>
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(path, options)
  if (!res.ok) {
    let message = `API error: ${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (typeof body.error === "string" && body.error.trim() !== "") {
        message = body.error
      }
    } catch {
      // keep fallback
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

export async function getOnboardStatus(): Promise<OnboardStatus> {
  return request<OnboardStatus>("/api/onboard/status")
}

export async function verifyModel(payload: {
  model: string
  api_base: string
  api_key: string
}): Promise<VerifyResult> {
  return request<VerifyResult>("/api/onboard/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  })
}

export async function completeOnboard(
  payload: CompleteRequest,
): Promise<CompleteResult> {
  return request<CompleteResult>("/api/onboard/complete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  })
}
