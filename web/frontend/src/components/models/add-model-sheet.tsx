import {
  IconBrandOpenai,
  IconCheck,
  IconClockHour4,
  IconLoader2,
} from "@tabler/icons-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { addModel, setDefaultModel } from "@/api/models"
import {
  getOAuthFlow,
  loginOAuth,
  pollOAuthFlow,
  type OAuthFlowState,
} from "@/api/oauth"
import { maskedSecretPlaceholder } from "@/components/secret-placeholder"
import {
  AdvancedSection,
  Field,
  KeyInput,
  SwitchCardField,
} from "@/components/shared-form"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

interface AddForm {
  modelName: string
  model: string
  apiBase: string
  apiKey: string
  proxy: string
  authMethod: string
  connectMode: string
  workspace: string
  rpm: string
  maxTokensField: string
  requestTimeout: string
  thinkingLevel: string
  protocol: string
}

const EMPTY_ADD_FORM: AddForm = {
  modelName: "",
  model: "",
  apiBase: "",
  apiKey: "",
  proxy: "",
  authMethod: "",
  connectMode: "",
  workspace: "",
  rpm: "",
  maxTokensField: "",
  requestTimeout: "",
  thinkingLevel: "",
  protocol: "openai",
}

const PROVIDER_DEFAULTS: Record<string, Partial<AddForm>> = {
  anthropic: {
    modelName: "Anthropic Claude",
    model: "anthropic/claude-opus-4-6",
    apiBase: "https://api.anthropic.com/v1",
  },
  gemini: {
    modelName: "Gemini",
    model: "gemini/gemini-1.5-pro",
    apiBase: "https://generativelanguage.googleapis.com/v1beta/openai/",
  },
  ollama: {
    modelName: "Ollama",
    model: "ollama/gpt-oss:20b",
    apiBase: "http://127.0.0.1:11434",
  },
  openai: {
    modelName: "OpenAI",
    model: "openai/gpt-5.4",
    apiBase: "https://api.openai.com/v1",
  },
  glm: {
    modelName: "GLM Models",
    model: "zai/glm-5",
    apiBase: "https://open.bigmodel.cn/api/paas/v4",
  },
  deepseek: {
    modelName: "DeepSeek",
    model: "deepseek/deepseek-chat",
    apiBase: "https://api.deepseek.com",
  },
  minimax: {
    modelName: "MiniMax",
    model: "minimax/MiniMax-M2.5",
    apiBase: "https://api.minimax.io/anthropic",
  },
  qwen: {
    modelName: "Qwen",
    model: "qwen-portal/coder-model",
    apiBase: "https://portal.qwen.ai/v1",
  },
  "openai-codex": {
    modelName: "OpenAI Codex",
    model: "openai-codex/gpt-5.4",
    apiBase: "",
  },
}

interface AddModelSheetProps {
  open: boolean
  onClose: () => void
  onSaved: () => void
  existingModelNames: string[]
}

export function AddModelSheet({
  open,
  onClose,
  onSaved,
  existingModelNames,
}: AddModelSheetProps) {
  const { t } = useTranslation()
  const [form, setForm] = useState<AddForm>(EMPTY_ADD_FORM)
  const [saving, setSaving] = useState(false)
  const [setAsDefault, setSetAsDefault] = useState(false)
  const [provider, setProvider] = useState("custom")
  const [fieldErrors, setFieldErrors] = useState<
    Partial<Record<keyof AddForm, string>>
  >({})
  const [serverError, setServerError] = useState("")
  const apiKeyPlaceholder = maskedSecretPlaceholder(
    form.apiKey,
    t("models.field.apiKeyPlaceholder"),
  )

  // OAuth state (for Codex)
  const [oauthBusy, setOauthBusy] = useState(false)
  const [oauthFlowId, setOauthFlowId] = useState("")
  const [oauthVerified, setOauthVerified] = useState(false)
  const [deviceCode, setDeviceCode] = useState<OAuthFlowState | null>(null)
  const oauthCancelRef = useRef(false)

  const isOAuthProvider = provider === "openai-codex"

  useEffect(() => {
    if (open) {
      setForm(EMPTY_ADD_FORM)
      setSetAsDefault(false)
      setProvider("custom")
      setFieldErrors({})
      setServerError("")
      setOauthBusy(false)
      setOauthFlowId("")
      setOauthVerified(false)
      setDeviceCode(null)
      oauthCancelRef.current = false
    }
  }, [open])

  const validate = (): boolean => {
    const errors: Partial<Record<keyof AddForm, string>> = {}
    const modelName = form.modelName.trim()
    if (!modelName) {
      errors.modelName = t("models.add.errorRequired")
    } else if (existingModelNames.some((name) => name.trim() === modelName)) {
      errors.modelName = t("models.add.errorDuplicateModelName")
    }
    if (!form.model.trim()) errors.model = t("models.add.errorRequired")
    if (isOAuthProvider && !oauthVerified) {
      setServerError(t("models.add.codexOAuthRequired", "Please authenticate via OAuth before saving."))
      setFieldErrors(errors)
      return false
    }
    setFieldErrors(errors)
    return Object.keys(errors).length === 0
  }

  const setField =
    (key: keyof AddForm) => (e: React.ChangeEvent<HTMLInputElement>) => {
      setForm((f) => ({ ...f, [key]: e.target.value }))
      if (fieldErrors[key]) {
        setFieldErrors((prev) => ({ ...prev, [key]: undefined }))
      }
    }

  const handleOAuthBrowser = async () => {
    setOauthBusy(true)
    oauthCancelRef.current = false
    const authTab = window.open("", "_blank")
    if (!authTab) {
      setOauthBusy(false)
      setServerError(t("credentials.errors.popupBlocked"))
      return
    }
    try {
      const resp = await loginOAuth({ provider: "openai-codex", method: "browser" })
      if (oauthCancelRef.current) { authTab.close(); return }
      if (!resp.auth_url || !resp.flow_id) throw new Error(t("credentials.errors.invalidBrowserResponse"))
      authTab.location.href = resp.auth_url
      setOauthFlowId(resp.flow_id)
    } catch (err) {
      authTab.close()
      setOauthBusy(false)
      setServerError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleOAuthDeviceCode = async () => {
    setOauthBusy(true)
    oauthCancelRef.current = false
    try {
      const resp = await loginOAuth({ provider: "openai-codex", method: "device_code" })
      if (oauthCancelRef.current) return
      if (!resp.flow_id || !resp.user_code || !resp.verify_url) throw new Error(t("credentials.errors.invalidDeviceResponse"))
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
      setServerError(err instanceof Error ? err.message : String(err))
    }
  }

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
          setOauthVerified(true)
          setOauthBusy(false)
          setDeviceCode(null)
          setServerError("")
        } else {
          setOauthBusy(false)
          setDeviceCode(null)
          setServerError(flow.error || t("credentials.errors.loginFailed"))
        }
      } catch {
        if (!canceled) { setOauthBusy(false); setDeviceCode(null) }
      }
    }

    void poll()

    const onMessage = (event: MessageEvent) => {
      const data = event.data as { type?: string; flowId?: string } | undefined
      if (data?.type === "picoclaw-oauth-result" && data.flowId === oauthFlowId) {
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

  const handleProviderChange = (val: string) => {
    setProvider(val)
    setOauthBusy(false)
    setOauthFlowId("")
    setOauthVerified(false)
    setDeviceCode(null)
    oauthCancelRef.current = true
    if (val !== "custom") {
      const defaults = PROVIDER_DEFAULTS[val]
      if (defaults) {
        setForm((f) => ({
          ...f,
          modelName: defaults.modelName || f.modelName,
          model: defaults.model || f.model,
          apiBase: defaults.apiBase || f.apiBase,
        }))
        setFieldErrors((prev) => ({
          ...prev,
          modelName: undefined,
          model: undefined,
        }))
      }
    } else {
      setForm((f) => ({
        ...f,
        modelName: "",
        model: "",
        apiBase: "",
        protocol: "openai",
      }))
    }
  }

  const handleSave = async () => {
    if (!validate()) return
    setSaving(true)
    setServerError("")
    try {
      const modelName = form.modelName.trim()
      let modelId = form.model.trim()
      
      // Auto-prepend protocol if no slash in modelId
      if (!modelId.includes("/")) {
        if (provider === "custom" && form.protocol) {
          modelId = `${form.protocol}/${modelId}`
        } else if (provider !== "custom") {
          // For built-in providers, use the provider name as protocol (e.g. anthropic/claude-3)
          modelId = `${provider}/${modelId}`
        }
      }
      
      let apiBase = form.apiBase.trim()
      
      // 智能容错：对于使用 OpenAI 格式协议的中转基地址，如果没有携带 /vX 后缀，则自动补全 /v1
      if (
        apiBase && 
        provider === "custom" && 
        (form.protocol === "openai" || form.protocol === "litellm" || form.protocol === "vllm" || form.protocol === "deepseek" || form.protocol === "qwen" || form.protocol === "minimax") &&
        !apiBase.match(/\/v\d+[\w-]*\/?$/) && 
        !apiBase.includes("googleapis.com")
      ) {
        apiBase = apiBase.replace(/\/+$/, "")
        apiBase = `${apiBase}/v1`
      }
      
      await addModel({
        model_name: modelName,
        model: modelId,
        api_base: apiBase || undefined,
        api_key: form.apiKey.trim() || undefined,
        proxy: form.proxy.trim() || undefined,
        auth_method: isOAuthProvider ? "oauth" : (form.authMethod.trim() || undefined),
        connect_mode: form.connectMode.trim() || undefined,
        workspace: form.workspace.trim() || undefined,
        rpm: form.rpm ? Number(form.rpm) : undefined,
        max_tokens_field: form.maxTokensField.trim() || undefined,
        request_timeout: form.requestTimeout
          ? Number(form.requestTimeout)
          : undefined,
        thinking_level: form.thinkingLevel.trim() || undefined,
      })
      if (setAsDefault) {
        await setDefaultModel(modelName)
      }
      onSaved()
      onClose()
    } catch (e) {
      setServerError(e instanceof Error ? e.message : t("models.add.saveError"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent
        side="right"
        className="flex flex-col gap-0 p-0 data-[side=right]:!w-full data-[side=right]:sm:!w-[560px] data-[side=right]:sm:!max-w-[560px]"
      >
        <SheetHeader className="border-b-muted border-b px-6 py-5">
          <SheetTitle className="text-base">{t("models.add.title")}</SheetTitle>
          <SheetDescription className="text-xs">
            {t("models.add.description")}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="space-y-5 px-6 py-5">
            <Field label={t("models.add.provider", "Provider")}>
              <Select value={provider} onValueChange={handleProviderChange}>
                <SelectTrigger>
                  <SelectValue placeholder="Select a provider" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="custom">Custom</SelectItem>
                  <SelectItem value="anthropic">Anthropic</SelectItem>
                  <SelectItem value="gemini">Gemini</SelectItem>
                  <SelectItem value="ollama">Ollama</SelectItem>
                  <SelectItem value="openai">OpenAI</SelectItem>
                  <SelectItem value="openai-codex">OpenAI Codex (OAuth)</SelectItem>
                  <SelectItem value="glm">GLM Models</SelectItem>
                  <SelectItem value="deepseek">DeepSeek</SelectItem>
                  <SelectItem value="minimax">MiniMax</SelectItem>
                  <SelectItem value="qwen">Qwen</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            {provider === "custom" && (
              <Field label={t("models.add.protocol", "Protocol (发送协议)")} hint="Select the API protocol used by this custom provider">
                <Select
                  value={form.protocol}
                  onValueChange={(val) => setForm((f) => ({ ...f, protocol: val }))}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select API Protocol" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="openai">OpenAI (Default)</SelectItem>
                    <SelectItem value="anthropic">Anthropic</SelectItem>
                    <SelectItem value="gemini">Gemini</SelectItem>
                    <SelectItem value="ollama">Ollama</SelectItem>
                    <SelectItem value="deepseek">DeepSeek</SelectItem>
                    <SelectItem value="minimax">MiniMax</SelectItem>
                    <SelectItem value="qwen">Qwen</SelectItem>
                    <SelectItem value="litellm">LiteLLM</SelectItem>
                    <SelectItem value="vllm">vLLM</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            )}

            <Field
              label={t("models.add.modelName")}
              hint={t("models.add.modelNameHint")}
            >
              <Input
                value={form.modelName}
                onChange={setField("modelName")}
                placeholder={t("models.add.modelNamePlaceholder")}
                aria-invalid={!!fieldErrors.modelName}
              />
              {fieldErrors.modelName && (
                <p className="text-destructive text-xs">
                  {fieldErrors.modelName}
                </p>
              )}
            </Field>

            <Field
              label={t("models.add.modelId")}
              hint={t("models.add.modelIdHint")}
            >
              <Input
                value={form.model}
                onChange={setField("model")}
                placeholder={t("models.add.modelIdPlaceholder")}
                className="font-mono text-sm"
                aria-invalid={!!fieldErrors.model}
              />
              {fieldErrors.model && (
                <p className="text-destructive text-xs">{fieldErrors.model}</p>
              )}
            </Field>

            {!isOAuthProvider && (
              <>
                <Field label={t("models.field.apiKey")}>
                  <KeyInput
                    value={form.apiKey}
                    onChange={(v) => setForm((f) => ({ ...f, apiKey: v }))}
                    placeholder={apiKeyPlaceholder}
                  />
                </Field>

                <Field label={t("models.field.apiBase")}>
                  <Input
                    value={form.apiBase}
                    onChange={setField("apiBase")}
                    placeholder="https://api.example.com/v1"
                  />
                </Field>
              </>
            )}

            {isOAuthProvider && (
              <div className="space-y-3">
                <p className="text-sm text-muted-foreground">
                  {t("models.add.codexOAuthDescription", "Sign in with your OpenAI account to use Codex. No API key required.")}
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={oauthBusy || oauthVerified}
                    onClick={handleOAuthBrowser}
                  >
                    {oauthBusy && !deviceCode && (
                      <IconLoader2 className="mr-1 size-4 animate-spin" />
                    )}
                    <IconBrandOpenai className="size-4" />
                    {t("credentials.actions.browser")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={oauthBusy || oauthVerified}
                    onClick={handleOAuthDeviceCode}
                  >
                    {oauthBusy && !!deviceCode && (
                      <IconLoader2 className="mr-1 size-4 animate-spin" />
                    )}
                    <IconClockHour4 className="size-4" />
                    {t("credentials.actions.deviceCode")}
                  </Button>
                </div>

                {deviceCode && (
                  <div className="space-y-2 rounded-lg border border-border p-3">
                    <p className="text-sm text-muted-foreground">
                      {t("credentials.device.description")}
                    </p>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">{t("credentials.device.code")}:</span>
                      <code className="rounded bg-muted px-2 py-1 text-sm font-bold tracking-widest">
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
                        {t("credentials.device.open")}
                      </a>
                    )}
                  </div>
                )}

                {oauthBusy && !deviceCode && (
                  <p className="text-sm text-muted-foreground">
                    {t("credentials.flow.pending")}
                  </p>
                )}

                {oauthVerified && (
                  <div className="flex items-center gap-2 text-sm text-primary">
                    <IconCheck className="size-4" />
                    {t("credentials.flow.success")}
                  </div>
                )}
              </div>
            )}

            <SwitchCardField
              label={t("models.defaultOnSave.label")}
              hint={t("models.defaultOnSave.description")}
              checked={setAsDefault}
              onCheckedChange={setSetAsDefault}
            />

            <AdvancedSection>
              <Field
                label={t("models.field.proxy")}
                hint={t("models.field.proxyHint")}
              >
                <Input
                  value={form.proxy}
                  onChange={setField("proxy")}
                  placeholder="http://127.0.0.1:7890"
                />
              </Field>

              <Field
                label={t("models.field.authMethod")}
                hint={t("models.field.authMethodHint")}
              >
                <Input
                  value={form.authMethod}
                  onChange={setField("authMethod")}
                  placeholder="oauth"
                />
              </Field>

              <Field
                label={t("models.field.connectMode")}
                hint={t("models.field.connectModeHint")}
              >
                <Input
                  value={form.connectMode}
                  onChange={setField("connectMode")}
                  placeholder="stdio"
                />
              </Field>

              <Field
                label={t("models.field.workspace")}
                hint={t("models.field.workspaceHint")}
              >
                <Input
                  value={form.workspace}
                  onChange={setField("workspace")}
                  placeholder="/path/to/workspace"
                />
              </Field>

              <Field
                label={t("models.field.requestTimeout")}
                hint={t("models.field.requestTimeoutHint")}
              >
                <Input
                  value={form.requestTimeout}
                  onChange={setField("requestTimeout")}
                  placeholder="60"
                  type="number"
                  min={0}
                />
              </Field>

              <Field
                label={t("models.field.rpm")}
                hint={t("models.field.rpmHint")}
              >
                <Input
                  value={form.rpm}
                  onChange={setField("rpm")}
                  placeholder="60"
                  type="number"
                  min={0}
                />
              </Field>

              <Field
                label={t("models.field.thinkingLevel")}
                hint={t("models.field.thinkingLevelHint")}
              >
                <Input
                  value={form.thinkingLevel}
                  onChange={setField("thinkingLevel")}
                  placeholder="off"
                />
              </Field>

              <Field
                label={t("models.field.maxTokensField")}
                hint={t("models.field.maxTokensFieldHint")}
              >
                <Input
                  value={form.maxTokensField}
                  onChange={setField("maxTokensField")}
                  placeholder="max_completion_tokens"
                />
              </Field>
            </AdvancedSection>

            {serverError && (
              <p className="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">
                {serverError}
              </p>
            )}
          </div>
        </div>

        <SheetFooter className="border-t-muted border-t px-6 py-4">
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving && <IconLoader2 className="size-4 animate-spin" />}
            {t("models.add.confirm")}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
