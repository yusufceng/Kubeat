import type { ChannelType } from "@/lib/api";

/** One input of a channel's form. */
type FieldSpec = { label: string; placeholder: string; hint?: string };

/** How a channel type is named and what it needs to be told. */
export type ChannelTypeInfo = {
  label: string;
  /** Where to get what the form asks for. */
  summary: string;
  /** Where the channel sends to; null for types that need only a key. */
  target: (FieldSpec & { inputType: "url" | "text" }) | null;
  /** The token or key beside the target; null for types without one. */
  secret: (FieldSpec & { required: boolean }) | null;
};

export const channelTypes: Record<ChannelType, ChannelTypeInfo> = {
  slack: {
    label: "Slack",
    summary: "Slack'te bir uygulamaya \"Incoming Webhooks\" ekleyip kanal için bir adres oluşturun.",
    target: { label: "Incoming webhook URL", placeholder: "https://hooks.slack.com/services/...", inputType: "url" },
    secret: null,
  },
  teams: {
    label: "Microsoft Teams",
    summary: "Kanalda \"Workflows\" ile \"Post to a channel when a webhook request is received\" akışını kurun.",
    target: { label: "Workflow webhook URL", placeholder: "https://...", inputType: "url" },
    secret: null,
  },
  telegram: {
    label: "Telegram",
    summary: "BotFather ile bir bot oluşturun, botu sohbete ekleyin ve sohbetin kimliğini girin.",
    target: {
      label: "Sohbet kimliği",
      placeholder: "-1001234567890",
      inputType: "text",
      hint: "Grup kimlikleri eksi ile başlar; herkese açık kanallar için @kanaladi.",
    },
    secret: { label: "Bot token", placeholder: "123456789:AA...", required: true },
  },
  discord: {
    label: "Discord",
    summary: "Kanal ayarlarında Entegrasyonlar > Webhook'lar altından bir webhook oluşturun.",
    target: { label: "Webhook URL", placeholder: "https://discord.com/api/webhooks/...", inputType: "url" },
    secret: null,
  },
  mattermost: {
    label: "Mattermost",
    summary: "Entegrasyonlar > Incoming Webhooks altından bir webhook oluşturun.",
    target: { label: "Incoming webhook URL", placeholder: "https://mattermost.ornek.com/hooks/...", inputType: "url" },
    secret: null,
  },
  googlechat: {
    label: "Google Chat",
    summary: "Alanın \"Uygulamalar ve entegrasyonlar\" bölümünden bir webhook ekleyin.",
    target: { label: "Webhook URL", placeholder: "https://chat.googleapis.com/v1/spaces/...", inputType: "url" },
    secret: null,
  },
  pagerduty: {
    label: "PagerDuty",
    summary:
      "Servise bir \"Events API v2\" entegrasyonu ekleyin. Kesinti bir alarm açar, düzelince aynı alarm kapanır.",
    target: null,
    secret: { label: "Integration key", placeholder: "32 karakterlik anahtar", required: true },
  },
  ntfy: {
    label: "ntfy",
    summary: "ntfy.sh ya da kendi ntfy sunucunuzdaki bir konuya yayınlar.",
    target: { label: "Konu adresi", placeholder: "https://ntfy.sh/kubeat-alarm", inputType: "url" },
    secret: { label: "Erişim token'ı (isteğe bağlı)", placeholder: "tk_...", required: false },
  },
  gotify: {
    label: "Gotify",
    summary: "Gotify sunucusunda bir uygulama oluşturup token'ını girin.",
    target: { label: "Sunucu adresi", placeholder: "https://gotify.ornek.com", inputType: "url" },
    secret: { label: "Uygulama token'ı", placeholder: "A1b2C3...", required: true },
  },
  email: {
    label: "E-posta",
    summary: "Backend'de tanımlı SMTP sunucusu üzerinden gönderilir.",
    target: {
      label: "Alıcı adresleri (virgülle ayırın)",
      placeholder: "ops@ornek.com, nobetci@ornek.com",
      inputType: "text",
    },
    secret: null,
  },
  webhook: {
    label: "Webhook (JSON)",
    summary: "Olayı yapılandırılmış JSON olarak herhangi bir adrese gönderir.",
    target: { label: "Webhook URL", placeholder: "https://ornek.com/hook", inputType: "url" },
    secret: null,
  },
};

/** The types in the order they are offered. */
export const channelTypeOrder: ChannelType[] = [
  "slack",
  "teams",
  "telegram",
  "discord",
  "mattermost",
  "googlechat",
  "pagerduty",
  "ntfy",
  "gotify",
  "email",
  "webhook",
];
