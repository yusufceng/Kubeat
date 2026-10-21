import type { ChannelType } from "@/lib/api";

/** One input of a channel's form; every text is a message key, shown with t(). */
type FieldSpec = { label: string; placeholder: string; hint?: string };

/**
 * The kinds of event a channel can be limited to. slow and slow_resolved
 * are a monitor turning slow (API state "degraded") and fast again; they
 * open no incident and go at once, whatever the channel's delay.
 */
export type ChannelEvent = "down" | "up" | "reminder" | "cert" | "slow" | "slow_resolved";

/** The message key of each event's name; show it with t(). */
export const channelEventLabels: Record<ChannelEvent, string> = {
  down: "channelEvent.down",
  up: "channelEvent.up",
  reminder: "channelEvent.reminder",
  cert: "channelEvent.cert",
  slow: "channelEvent.slow",
  slow_resolved: "channelEvent.slow_resolved",
};

/**
 * How a channel type is named and what it needs to be told. Every text
 * here is a message key: show it with t().
 */
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
    label: "channelType.slack",
    summary: "channelType.slack.summary",
    target: {
      label: "channelType.slack.target.label",
      placeholder: "channelType.slack.target.placeholder",
      inputType: "url",
    },
    secret: null,
  },
  teams: {
    label: "channelType.teams",
    summary: "channelType.teams.summary",
    target: {
      label: "channelType.teams.target.label",
      placeholder: "channelType.teams.target.placeholder",
      inputType: "url",
    },
    secret: null,
  },
  telegram: {
    label: "channelType.telegram",
    summary: "channelType.telegram.summary",
    target: {
      label: "channelType.telegram.target.label",
      placeholder: "channelType.telegram.target.placeholder",
      inputType: "text",
      hint: "channelType.telegram.target.hint",
    },
    secret: {
      label: "channelType.telegram.secret.label",
      placeholder: "channelType.telegram.secret.placeholder",
      required: true,
    },
  },
  discord: {
    label: "channelType.discord",
    summary: "channelType.discord.summary",
    target: {
      label: "channelType.discord.target.label",
      placeholder: "channelType.discord.target.placeholder",
      inputType: "url",
    },
    secret: null,
  },
  mattermost: {
    label: "channelType.mattermost",
    summary: "channelType.mattermost.summary",
    target: {
      label: "channelType.mattermost.target.label",
      placeholder: "channelType.mattermost.target.placeholder",
      inputType: "url",
    },
    secret: null,
  },
  googlechat: {
    label: "channelType.googlechat",
    summary: "channelType.googlechat.summary",
    target: {
      label: "channelType.googlechat.target.label",
      placeholder: "channelType.googlechat.target.placeholder",
      inputType: "url",
    },
    secret: null,
  },
  pagerduty: {
    label: "channelType.pagerduty",
    summary: "channelType.pagerduty.summary",
    target: null,
    secret: {
      label: "channelType.pagerduty.secret.label",
      placeholder: "channelType.pagerduty.secret.placeholder",
      required: true,
    },
  },
  ntfy: {
    label: "channelType.ntfy",
    summary: "channelType.ntfy.summary",
    target: {
      label: "channelType.ntfy.target.label",
      placeholder: "channelType.ntfy.target.placeholder",
      inputType: "url",
    },
    secret: {
      label: "channelType.ntfy.secret.label",
      placeholder: "channelType.ntfy.secret.placeholder",
      required: false,
    },
  },
  gotify: {
    label: "channelType.gotify",
    summary: "channelType.gotify.summary",
    target: {
      label: "channelType.gotify.target.label",
      placeholder: "channelType.gotify.target.placeholder",
      inputType: "url",
    },
    secret: {
      label: "channelType.gotify.secret.label",
      placeholder: "channelType.gotify.secret.placeholder",
      required: true,
    },
  },
  email: {
    label: "channelType.email",
    summary: "channelType.email.summary",
    target: {
      label: "channelType.email.target.label",
      placeholder: "channelType.email.target.placeholder",
      inputType: "text",
    },
    secret: null,
  },
  webhook: {
    label: "channelType.webhook",
    summary: "channelType.webhook.summary",
    target: {
      label: "channelType.webhook.target.label",
      placeholder: "channelType.webhook.target.placeholder",
      inputType: "url",
    },
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
