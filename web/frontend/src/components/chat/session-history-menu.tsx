import { IconHistory, IconTrash } from "@tabler/icons-react"
import dayjs from "dayjs"
import type { RefObject } from "react"
import { useTranslation } from "react-i18next"

import type { SessionSummary } from "@/api/sessions"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"

interface SessionHistoryMenuProps {
  sessions: SessionSummary[]
  activeSessionId: string
  hasMore: boolean
  observerRef: RefObject<HTMLDivElement | null>
  onOpenChange: (open: boolean) => void
  onSwitchSession: (sessionId: string) => void
  onDeleteSession: (sessionId: string) => void
}

const CHANNEL_LABELS: Record<string, string> = {
  pico: "Web",
  feishu: "飞书",
  dingtalk: "钉钉",
  wecom: "企业微信",
  wecom_aibot: "企业微信",
  wecom_app: "企业微信",
  telegram: "Telegram",
  line: "LINE",
  discord: "Discord",
  whatsapp: "WhatsApp",
  slack: "Slack",
}

function channelLabel(channel: string): string {
  return CHANNEL_LABELS[channel] ?? channel
}

export function SessionHistoryMenu({
  sessions,
  activeSessionId,
  hasMore,
  observerRef,
  onOpenChange,
  onSwitchSession,
  onDeleteSession,
}: SessionHistoryMenuProps) {
  const { t } = useTranslation()

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="h-9 gap-2">
          <IconHistory className="size-4" />
          <span className="hidden sm:inline">{t("chat.history")}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-72">
        <ScrollArea className="max-h-[300px]">
          {sessions.length === 0 ? (
            <DropdownMenuItem disabled>
              <span className="text-muted-foreground text-xs">
                {t("chat.noHistory")}
              </span>
            </DropdownMenuItem>
          ) : (
            sessions.map((session) => (
              <DropdownMenuItem
                key={session.id}
                className={`group relative my-0.5 flex flex-col items-start gap-0.5 pr-8 ${
                  session.id === activeSessionId ? "bg-accent" : ""
                }`}
                onClick={() => onSwitchSession(session.id)}
              >
                <div className="flex w-full items-center gap-1.5">
                  {session.channel && session.channel !== "pico" && (
                    <span className="bg-muted text-muted-foreground shrink-0 rounded px-1 py-0.5 text-[10px] font-medium leading-none">
                      {channelLabel(session.channel)}
                    </span>
                  )}
                  <span className="line-clamp-1 text-sm font-medium">
                    {session.preview}
                  </span>
                </div>
                <span className="text-muted-foreground text-xs">
                  {t("chat.messagesCount", {
                    count: session.message_count,
                  })}{" "}
                  · {dayjs(session.updated).fromNow()}
                </span>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("chat.deleteSession")}
                  className="text-muted-foreground hover:bg-destructive/10 hover:text-destructive absolute top-1/2 right-2 h-6 w-6 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100"
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onDeleteSession(session.id)
                  }}
                >
                  <IconTrash className="h-4 w-4" />
                </Button>
              </DropdownMenuItem>
            ))
          )}
          {hasMore && sessions.length > 0 && (
            <div ref={observerRef} className="py-2 text-center">
              <span className="text-muted-foreground animate-pulse text-xs">
                {t("chat.loadingMore")}
              </span>
            </div>
          )}
        </ScrollArea>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
