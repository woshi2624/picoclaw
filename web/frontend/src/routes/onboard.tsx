import { useState, useCallback, useEffect, useRef } from "react"
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import {
  IconCheck,
  IconLoader2,
  IconBrandOpenai,
  IconClockHour4,
} from "@tabler/icons-react"
import { Toaster, toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  getOnboardStatus,
  verifyModel,
  completeOnboard,
  type CompleteRequest,
} from "@/api/onboard"
import {
  loginOAuth,
  getOAuthFlow,
  pollOAuthFlow,
  type OAuthFlowState,
} from "@/api/oauth"

// Provider presets derived from the backend's model_list defaults.
interface ProviderPreset {
  label: string
  protocol: string
  model: string
  modelName: string
  apiBase: string
  authMode: "apikey" | "oauth"
}

const PROVIDERS: ProviderPreset[] = [
  {
    label: "DeepSeek",
    protocol: "deepseek",
    model: "deepseek/deepseek-chat",
    modelName: "deepseek",
    apiBase: "https://api.deepseek.com/v1",
    authMode: "apikey",
  },
  {
    label: "OpenAI",
    protocol: "openai",
    model: "openai/gpt-4o",
    modelName: "gpt-4o",
    apiBase: "https://api.openai.com/v1",
    authMode: "apikey",
  },
  {
    label: "OpenAI (Codex)",
    protocol: "openai-codex",
    model: "openai-codex/gpt-5.4",
    modelName: "gpt-5.4",
    apiBase: "",
    authMode: "oauth",
  },
  {
    label: "Anthropic (Claude)",
    protocol: "anthropic",
    model: "anthropic/claude-sonnet-4-20250514",
    modelName: "claude-sonnet",
    apiBase: "https://api.anthropic.com/v1",
    authMode: "apikey",
  },
  {
    label: "OpenRouter",
    protocol: "openrouter",
    model: "openrouter/google/gemini-2.5-flash",
    modelName: "openrouter",
    apiBase: "https://openrouter.ai/api/v1",
    authMode: "apikey",
  },
  {
    label: "Gemini",
    protocol: "gemini",
    model: "gemini/gemini-2.5-flash",
    modelName: "gemini-flash",
    apiBase: "https://generativelanguage.googleapis.com/v1beta",
    authMode: "apikey",
  },
  {
    label: "Qwen (Aliyun)",
    protocol: "qwen",
    model: "qwen/qwen-plus",
    modelName: "qwen-plus",
    apiBase: "https://dashscope.aliyuncs.com/compatible-mode/v1",
    authMode: "apikey",
  },
  {
    label: "Zhipu (GLM)",
    protocol: "zhipu",
    model: "zhipu/glm-4-flash-250414",
    modelName: "glm-4-flash",
    apiBase: "https://open.bigmodel.cn/api/paas/v4",
    authMode: "apikey",
  },
  {
    label: "Groq",
    protocol: "groq",
    model: "groq/llama-3.3-70b-versatile",
    modelName: "groq-llama",
    apiBase: "https://api.groq.com/openai/v1",
    authMode: "apikey",
  },
  {
    label: "Ollama (Local)",
    protocol: "ollama",
    model: "ollama/llama3",
    modelName: "ollama-llama3",
    apiBase: "http://localhost:11434/v1",
    authMode: "apikey",
  },
]

// Channel definitions with their required credential fields.
const CHANNELS = [
  {
    type: "telegram",
    label: "Telegram",
    fields: [{ key: "token", label: "token" }],
  },
  {
    type: "discord",
    label: "Discord",
    fields: [{ key: "token", label: "token" }],
  },
  {
    type: "qq",
    label: "QQ",
    fields: [
      { key: "app_id", label: "app_id" },
      { key: "app_secret", label: "app_secret" },
    ],
  },
  {
    type: "feishu",
    label: "Feishu",
    fields: [
      { key: "app_id", label: "app_id" },
      { key: "app_secret", label: "app_secret" },
    ],
  },
  {
    type: "dingtalk",
    label: "DingTalk",
    fields: [
      { key: "client_id", label: "client_id" },
      { key: "client_secret", label: "client_secret" },
    ],
  },
  {
    type: "slack",
    label: "Slack",
    fields: [
      { key: "bot_token", label: "bot_token" },
      { key: "app_token", label: "app_token" },
    ],
  },
] as const

type Step = "welcome" | "model" | "channel"

const STEPS: Step[] = ["welcome", "model", "channel"]

function OnboardPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  const [step, setStep] = useState<Step>("welcome")

  // Model form state
  const [providerIdx, setProviderIdx] = useState<number>(-1)
  const [modelId, setModelId] = useState("")
  const [apiBase, setApiBase] = useState("")
  const [apiKey, setApiKey] = useState("")
  const [verified, setVerified] = useState(false)
  const [verifying, setVerifying] = useState(false)

  // OAuth state (for Codex)
  const [oauthBusy, setOauthBusy] = useState(false)
  const [oauthFlowId, setOauthFlowId] = useState("")
  const [deviceCode, setDeviceCode] = useState<OAuthFlowState | null>(null)
  const oauthCancelRef = useRef(false)

  // Channel form state
  const [selectedChannel, setSelectedChannel] = useState<string>("")
  const [channelData, setChannelData] = useState<Record<string, string>>({})

  // Done state
  const [configPath, setConfigPath] = useState("")
  const [saving, setSaving] = useState(false)
  const [done, setDone] = useState(false)
  const [saveError, setSaveError] = useState("")

  const { data: status } = useQuery({
    queryKey: ["onboard-status"],
    queryFn: getOnboardStatus,
  })

  const selectedProvider = providerIdx >= 0 ? PROVIDERS[providerIdx] : undefined
  const isOAuthProvider = selectedProvider?.authMode === "oauth"

  const handleProviderChange = useCallback((value: string) => {
    const idx = parseInt(value, 10)
    const p = PROVIDERS[idx]
    if (!p) return
    setProviderIdx(idx)
    setModelId(p.model)
    setApiBase(p.apiBase)
    setVerified(false)
    setApiKey("")
    setOauthBusy(false)
    setOauthFlowId("")
    setDeviceCode(null)
  }, [])

  const handleVerify = useCallback(async () => {
    setVerifying(true)
    try {
      const result = await verifyModel({
        model: modelId,
        api_base: apiBase,
        api_key: apiKey,
      })
      if (result.valid) {
        setVerified(true)
        toast.success(t("onboard.model.verifySuccess"))
      } else {
        toast.error(`${t("onboard.model.verifyFailed")}: ${result.error}`)
      }
    } catch (err) {
      toast.error(
        `${t("onboard.model.verifyFailed")}: ${err instanceof Error ? err.message : String(err)}`,
      )
    } finally {
      setVerifying(false)
    }
  }, [modelId, apiBase, apiKey, t])

  // --- OAuth Browser Flow ---
  const handleOAuthBrowser = useCallback(async () => {
    setOauthBusy(true)
    oauthCancelRef.current = false

    const authTab = window.open("", "_blank")
    if (!authTab) {
      toast.error(t("onboard.codex.popupBlocked"))
      setOauthBusy(false)
      return
    }

    try {
      const resp = await loginOAuth({ provider: "openai-codex", method: "browser" })
      if (oauthCancelRef.current) {
        authTab.close()
        return
      }
      if (!resp.auth_url || !resp.flow_id) {
        throw new Error("Invalid browser login response")
      }
      authTab.location.href = resp.auth_url
      setOauthFlowId(resp.flow_id)
    } catch (err) {
      authTab.close()
      setOauthBusy(false)
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }, [t])

  // --- OAuth Device Code Flow ---
  const handleOAuthDeviceCode = useCallback(async () => {
    setOauthBusy(true)
    oauthCancelRef.current = false

    try {
      const resp = await loginOAuth({
        provider: "openai-codex",
        method: "device_code",
      })
      if (oauthCancelRef.current) return
      if (!resp.flow_id || !resp.user_code || !resp.verify_url) {
        throw new Error("Invalid device code response")
      }

      const flow: OAuthFlowState = {
        flow_id: resp.flow_id,
        provider: "openai",
        method: "device_code",
        status: "pending",
        user_code: resp.user_code,
        verify_url: resp.verify_url,
        interval: resp.interval,
        expires_at: resp.expires_at,
      }
      setDeviceCode(flow)
      setOauthFlowId(resp.flow_id)
    } catch (err) {
      setOauthBusy(false)
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }, [])

  // --- Poll OAuth flow until success/error ---
  useEffect(() => {
    if (!oauthFlowId || !oauthBusy) return

    let canceled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const isDeviceFlow = !!deviceCode

    const poll = async () => {
      try {
        const flow = isDeviceFlow
          ? await pollOAuthFlow(oauthFlowId)
          : await getOAuthFlow(oauthFlowId)

        if (canceled) return

        if (flow.status === "pending") {
          timer = setTimeout(poll, isDeviceFlow ? Math.max(1000, (flow.interval ?? 5) * 1000) : 2000)
          return
        }

        if (flow.status === "success") {
          setVerified(true)
          setOauthBusy(false)
          setDeviceCode(null)
          toast.success(t("onboard.codex.loginSuccess"))
        } else {
          setOauthBusy(false)
          setDeviceCode(null)
          toast.error(flow.error || t("onboard.codex.loginFailed"))
        }
      } catch {
        if (!canceled) {
          setOauthBusy(false)
          setDeviceCode(null)
        }
      }
    }

    void poll()

    // Also listen for browser OAuth postMessage callback
    const onMessage = (event: MessageEvent) => {
      const data = event.data as
        | { type?: string; flowId?: string; status?: string }
        | undefined
      if (
        data?.type === "picoclaw-oauth-result" &&
        data.flowId === oauthFlowId
      ) {
        // Trigger a status check
        void poll()
      }
    }
    window.addEventListener("message", onMessage)

    return () => {
      canceled = true
      if (timer) clearTimeout(timer)
      window.removeEventListener("message", onMessage)
    }
  }, [oauthFlowId, oauthBusy, deviceCode, t])

  const handleComplete = useCallback(async () => {
    setSaving(true)
    setSaveError("")
    try {
      const provider = providerIdx >= 0 ? PROVIDERS[providerIdx] : undefined
      const payload: CompleteRequest = {
        model_name: provider?.modelName ?? modelId,
        model: modelId,
        api_base: apiBase,
        api_key: apiKey,
      }
      if (provider?.authMode === "oauth") {
        payload.auth_method = "oauth"
      }
      if (selectedChannel) {
        payload.channel_type = selectedChannel
        payload.channel_data = channelData
      }
      const result = await completeOnboard(payload)
      if (result.success) {
        setConfigPath(result.config_path ?? "")
        setDone(true)
      } else {
        setSaveError(result.error ?? t("onboard.done.error"))
      }
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [providerIdx, modelId, apiBase, apiKey, selectedChannel, channelData, t])

  const currentStepIdx = STEPS.indexOf(step)

  // --- Step Indicator ---
  const StepIndicator = () => (
    <div className="mb-6 flex items-center justify-center gap-2">
      {STEPS.map((s, i) => {
        const isDone = i < currentStepIdx || done
        const isCurrent = s === step && !done
        return (
          <div key={s} className="flex items-center gap-2">
            {i > 0 && (
              <div
                className={`h-px w-8 ${i <= currentStepIdx || done ? "bg-primary" : "bg-border"}`}
              />
            )}
            <div className="flex items-center gap-1.5">
              <div
                className={`flex size-6 items-center justify-center rounded-full text-xs font-medium ${
                  isDone
                    ? "bg-primary text-primary-foreground"
                    : isCurrent
                      ? "border-2 border-primary text-primary"
                      : "border border-border text-muted-foreground"
                }`}
              >
                {isDone ? <IconCheck className="size-3.5" /> : i + 1}
              </div>
              <span
                className={`text-xs ${isCurrent ? "font-medium text-foreground" : "text-muted-foreground"}`}
              >
                {t(`onboard.steps.${s}`)}
              </span>
            </div>
          </div>
        )
      })}
    </div>
  )

  // --- Welcome Step ---
  if (step === "welcome" && !done) {
    return (
      <OnboardShell>
        <StepIndicator />
        <Card className="mx-auto w-full max-w-md">
          <CardHeader className="items-center text-center">
            <img
              src="/logo_with_text.png"
              alt="PicoClaw"
              className="mb-2 h-12"
            />
            <CardTitle>{t("onboard.welcome.title")}</CardTitle>
            <CardDescription>
              {t("onboard.welcome.description")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {status && (
              <div className="flex flex-col gap-1 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                <span>
                  {t("onboard.welcome.version")}: {status.version}
                </span>
                <span>
                  {t("onboard.welcome.system")}: {status.os}/{status.arch}
                </span>
              </div>
            )}
            <Button
              className="w-full"
              size="lg"
              onClick={() => setStep("model")}
            >
              {t("onboard.welcome.start")}
            </Button>
          </CardContent>
        </Card>
      </OnboardShell>
    )
  }

  // --- Model Step ---
  if (step === "model" && !done) {
    return (
      <OnboardShell>
        <StepIndicator />
        <Card className="mx-auto w-full max-w-md">
          <CardHeader>
            <CardTitle>{t("onboard.model.title")}</CardTitle>
            <CardDescription>
              {t("onboard.model.description")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1.5">
              <Label>{t("onboard.model.provider")}</Label>
              <Select
                value={providerIdx >= 0 ? String(providerIdx) : undefined}
                onValueChange={handleProviderChange}
              >
                <SelectTrigger className="w-full">
                  <SelectValue
                    placeholder={t("onboard.model.providerPlaceholder")}
                  />
                </SelectTrigger>
                <SelectContent>
                  {PROVIDERS.map((p, i) => (
                    <SelectItem key={p.protocol} value={String(i)}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {providerIdx >= 0 && !isOAuthProvider && (
              <>
                <div className="space-y-1.5">
                  <Label>{t("onboard.model.modelId")}</Label>
                  <Input
                    value={modelId}
                    onChange={(e) => {
                      setModelId(e.target.value)
                      setVerified(false)
                    }}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>{t("onboard.model.apiBase")}</Label>
                  <Input
                    value={apiBase}
                    onChange={(e) => {
                      setApiBase(e.target.value)
                      setVerified(false)
                    }}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>{t("onboard.model.apiKey")}</Label>
                  <Input
                    type="password"
                    placeholder={t("onboard.model.apiKeyPlaceholder")}
                    value={apiKey}
                    onChange={(e) => {
                      setApiKey(e.target.value)
                      setVerified(false)
                    }}
                  />
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleVerify}
                    disabled={!apiKey || verifying}
                  >
                    {verifying && (
                      <IconLoader2 className="mr-1 size-4 animate-spin" />
                    )}
                    {verifying
                      ? t("onboard.model.verifying")
                      : t("onboard.model.verify")}
                  </Button>
                  <Button
                    className="flex-1"
                    disabled={!verified}
                    onClick={() => setStep("channel")}
                  >
                    {t("onboard.model.next")}
                  </Button>
                </div>
              </>
            )}

            {/* OAuth (Codex) provider UI */}
            {providerIdx >= 0 && isOAuthProvider && (
              <>
                <div className="space-y-1.5">
                  <Label>{t("onboard.model.modelId")}</Label>
                  <Input
                    value={modelId}
                    onChange={(e) => {
                      setModelId(e.target.value)
                      setVerified(false)
                    }}
                  />
                </div>
                <p className="text-sm text-muted-foreground">
                  {t("onboard.codex.description")}
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    disabled={oauthBusy}
                    onClick={handleOAuthBrowser}
                  >
                    {oauthBusy && !deviceCode && (
                      <IconLoader2 className="mr-1 size-4 animate-spin" />
                    )}
                    <IconBrandOpenai className="size-4" />
                    {t("onboard.codex.browser")}
                  </Button>
                  <Button
                    variant="outline"
                    disabled={oauthBusy}
                    onClick={handleOAuthDeviceCode}
                  >
                    {oauthBusy && !!deviceCode && (
                      <IconLoader2 className="mr-1 size-4 animate-spin" />
                    )}
                    <IconClockHour4 className="size-4" />
                    {t("onboard.codex.deviceCode")}
                  </Button>
                </div>

                {/* Device code display */}
                {deviceCode && (
                  <div className="space-y-2 rounded-lg border border-border p-3">
                    <p className="text-sm text-muted-foreground">
                      {t("onboard.codex.deviceCodeHint")}
                    </p>
                    <div className="flex items-center gap-2">
                      <code className="rounded bg-muted px-2 py-1 text-lg font-bold tracking-widest">
                        {deviceCode.user_code}
                      </code>
                    </div>
                    {deviceCode.verify_url && (
                      <a
                        href={deviceCode.verify_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-sm text-primary underline underline-offset-4"
                      >
                        {t("onboard.codex.openVerify")}
                      </a>
                    )}
                  </div>
                )}

                {verified && (
                  <div className="flex items-center gap-2 text-sm text-primary">
                    <IconCheck className="size-4" />
                    {t("onboard.codex.loginSuccess")}
                  </div>
                )}

                <Button
                  className="w-full"
                  disabled={!verified}
                  onClick={() => setStep("channel")}
                >
                  {t("onboard.model.next")}
                </Button>
              </>
            )}
          </CardContent>
        </Card>
      </OnboardShell>
    )
  }

  // --- Channel Step ---
  if (step === "channel" && !done) {
    const selectedDef = CHANNELS.find((c) => c.type === selectedChannel)

    return (
      <OnboardShell>
        <StepIndicator />
        <Card className="mx-auto w-full max-w-lg">
          <CardHeader>
            <CardTitle>{t("onboard.channel.title")}</CardTitle>
            <CardDescription>
              {t("onboard.channel.description")}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-3 gap-2">
              {CHANNELS.map((ch) => (
                <button
                  key={ch.type}
                  type="button"
                  onClick={() => {
                    setSelectedChannel(
                      selectedChannel === ch.type ? "" : ch.type,
                    )
                    setChannelData({})
                  }}
                  className={`rounded-lg border px-3 py-2 text-sm transition-colors ${
                    selectedChannel === ch.type
                      ? "border-primary bg-primary/5 font-medium text-primary"
                      : "border-border hover:border-primary/40 hover:bg-muted/50"
                  }`}
                >
                  {ch.label}
                </button>
              ))}
            </div>

            {selectedDef && (
              <div className="space-y-3 rounded-lg border border-border p-3">
                {selectedDef.fields.map((f) => (
                  <div key={f.key} className="space-y-1.5">
                    <Label>
                      {t(`onboard.channel.fields.${f.label}`, {
                        defaultValue: f.label,
                      })}
                    </Label>
                    <Input
                      type="password"
                      value={channelData[f.key] ?? ""}
                      onChange={(e) =>
                        setChannelData((prev) => ({
                          ...prev,
                          [f.key]: e.target.value,
                        }))
                      }
                    />
                  </div>
                ))}
              </div>
            )}

            <div className="flex gap-2">
              <Button
                variant="outline"
                onClick={() => {
                  setSelectedChannel("")
                  setChannelData({})
                  handleComplete()
                }}
                disabled={saving}
              >
                {t("onboard.channel.skip")}
              </Button>
              <Button
                className="flex-1"
                disabled={saving || (!!selectedChannel && !channelData[selectedDef?.fields[0]?.key ?? ""])}
                onClick={handleComplete}
              >
                {saving && (
                  <IconLoader2 className="mr-1 size-4 animate-spin" />
                )}
                {t("onboard.channel.finish")}
              </Button>
            </div>
            {saveError && (
              <p className="text-sm text-destructive">{saveError}</p>
            )}
          </CardContent>
        </Card>
      </OnboardShell>
    )
  }

  // --- Done ---
  return (
    <OnboardShell>
      <StepIndicator />
      <Card className="mx-auto w-full max-w-md">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex size-12 items-center justify-center rounded-full bg-primary text-primary-foreground">
            <IconCheck className="size-6" />
          </div>
          <CardTitle>{t("onboard.done.title")}</CardTitle>
          <CardDescription>{t("onboard.done.description")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {configPath && (
            <code className="block rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground break-all">
              {configPath}
            </code>
          )}
          <Button
            className="w-full"
            size="lg"
            onClick={() => void navigate({ to: "/" })}
          >
            {t("onboard.done.enter")}
          </Button>
        </CardContent>
      </Card>
    </OnboardShell>
  )
}

function OnboardShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <div className="w-full max-w-lg">
        {children}
      </div>
      <Toaster position="bottom-center" />
    </div>
  )
}

export const Route = createFileRoute("/onboard")({
  component: OnboardPage,
})
