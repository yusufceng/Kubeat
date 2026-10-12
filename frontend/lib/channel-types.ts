import type { ChannelType } from "@/lib/api";

/** How each channel type is named and what its target is. */
export const channelTypes: Record<
  ChannelType,
  { label: string; targetLabel: string; placeholder: string; inputType: "url" | "text" }
> = {
  slack: {
    label: "Slack",
    targetLabel: "Incoming webhook URL",
    placeholder: "https://hooks.slack.com/services/...",
    inputType: "url",
  },
  teams: {
    label: "Microsoft Teams",
    targetLabel: "Workflow webhook URL",
    placeholder: "https://...",
    inputType: "url",
  },
  email: {
    label: "E-posta",
    targetLabel: "Alıcı adresleri (virgülle ayırın)",
    placeholder: "ops@ornek.com, nobetci@ornek.com",
    inputType: "text",
  },
  webhook: {
    label: "Webhook (JSON)",
    targetLabel: "Webhook URL",
    placeholder: "https://ornek.com/hook",
    inputType: "url",
  },
};
