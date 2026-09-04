import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';
import antigravityIcon from './assets/antigravity.svg';
import claudeIcon from './assets/claude.svg';
import clineIcon from './assets/cline.svg';
import codexIcon from './assets/codex.svg';
import geminiIcon from './assets/gemini.svg';
import grokIcon from './assets/grok.svg';
import kimiIcon from './assets/kimi-light.svg';
import nexusIcon from './assets/nexus.svg';
import qwenIcon from './assets/qwen.svg';
import vertexIcon from './assets/vertex.svg';

const API = '/v0/management/plugins/cpa-provider-nexus';
const NEXUS_PLUGIN_ID = 'cpa-provider-nexus';
// CPA native management API (same origin, same Bearer). auth-files is the
// single source of truth for per-credential success/failed counts and
// per-provider quota signals across every provider, not just Kiro.
const MGMT = '/v0/management';

const ICONS = {
  codex: codexIcon,
  claude: claudeIcon,
  gemini: geminiIcon,
  aistudio: geminiIcon,
  antigravity: antigravityIcon,
  xai: grokIcon,
  grok: grokIcon,
  cline: clineIcon,
  kimi: kimiIcon,
  qwen: qwenIcon,
  vertex: vertexIcon,
  kiro: 'https://kiro.dev/favicon.ico',
};

const TYPE_COLORS = {
  qwen: { bg: '#ede5fd', text: '#5530c7' },
  gemini: { bg: '#e3f2fd', text: '#1565c0' },
  aistudio: { bg: '#f0f2f5', text: '#2f343c' },
  claude: { bg: '#fbece4', text: '#c05621' },
  codex: { bg: '#eae7ff', text: '#3538d4' },
  cline: { bg: '#dff3ee', text: '#246b5c' },
  kimi: { bg: '#dce8ff', text: '#0560cf' },
  antigravity: { bg: '#e0f7fa', text: '#006064' },
  xai: { bg: '#f3f4f6', text: '#111827', border: '1px solid #d1d5db' },
  grok: { bg: '#f3f4f6', text: '#111827', border: '1px solid #d1d5db' },
  vertex: { bg: '#e4edfd', text: '#2b5fbc' },
  kiro: { bg: '#eee7ff', text: '#6a4bd4' },
  unknown: { bg: '#f0f0f0', text: '#666666', border: '1px dashed #999999' },
};

const normalizeKey = (v) => String(v || '').trim().toLowerCase();
const typeColor = (key) => TYPE_COLORS[key] || TYPE_COLORS.unknown;
const iconFor = (key) => ICONS[key] || null;
const typeLabel = (key) => (key ? key.charAt(0).toUpperCase() + key.slice(1) : 'Unknown');
const esc = (v) => String(v ?? '');

function ModelClusterIcon({ size = 14 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="5" width="6" height="6" rx="1.5" />
      <rect x="15" y="5" width="6" height="6" rx="1.5" />
      <rect x="9" y="13" width="6" height="6" rx="1.5" />
      <path d="M9 8h6" />
      <path d="M12 11v2" />
      <path d="M7.5 11v2" />
      <path d="M16.5 11v2" />
    </svg>
  );
}

function OAuthLoginIcon({ size = 22 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M2 21a8 8 0 0 1 13.292-6" />
      <circle cx="10" cy="8" r="5" />
      <path d="m16 19 2 2 4-4" />
    </svg>
  );
}

function RefreshIcon({ size = 15 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8" />
      <path d="M21 3v5h-5" />
    </svg>
  );
}

function BackIcon({ size = 16 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M19 12H5" />
      <path d="m12 19-7-7 7-7" />
    </svg>
  );
}

function DownloadIcon({ size = 15 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 15V3" />
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <path d="m7 10 5 5 5-5" />
    </svg>
  );
}

function TrashIcon({ size = 15 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 6h18" />
      <path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6" />
      <path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2" />
      <line x1="10" x2="10" y1="11" y2="17" />
      <line x1="14" x2="14" y1="11" y2="17" />
    </svg>
  );
}

function syncCPAFrameBackground() {
  if (window.parent === window || !window.frameElement) return;
  try {
    const parentRoot = window.parent.document.documentElement;
    const parentStyles = window.parent.getComputedStyle(parentRoot);
    const background =
      parentStyles.getPropertyValue('--bg-secondary').trim() ||
      window.parent.getComputedStyle(window.parent.document.body).backgroundColor;
    if (!background) return;

    window.frameElement.style.background = background;
    let hostElement = window.frameElement.parentElement;
    while (hostElement) {
      hostElement.style.background = background;
      if (hostElement.tagName === 'MAIN') break;
      hostElement = hostElement.parentElement;
    }
  } catch {
    // Cross-origin embedding: the iframe shell remains host-controlled.
  }
}

function syncCPATheme() {
  const root = document.documentElement;
  const systemTheme = window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'white';
  let theme = systemTheme;
  try {
    if (window.parent !== window) {
      const hostTheme = window.parent.document.documentElement.getAttribute('data-theme');
      theme = hostTheme === 'dark' ? 'dark' : 'white';
    }
  } catch {
    // Cross-origin embedding: keep the system theme fallback.
  }
  syncCPAFrameBackground();
  root.setAttribute('data-cpa-theme', theme);
  return theme;
}

async function request(path, key, options = {}) {
  const base = options.base ?? API;
  const response = await fetch(base + path, {
    ...options,
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': 'application/json',
      ...(options.headers || {}),
    },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw Error(data.message || data.error || `HTTP ${response.status}`);
  return data;
}

async function fetchAuthFileModels(name, key) {
  const data = await request(`/auth-files/models?name=${encodeURIComponent(name)}`, key, { base: MGMT });
  return Array.isArray(data.models) ? data.models : [];
}

async function downloadAuthFile(name, key) {
  const response = await fetch(
    `${MGMT}/auth-files/download?name=${encodeURIComponent(name)}`,
    { headers: { Authorization: `Bearer ${key}` } }
  );
  if (!response.ok) throw Error(`HTTP ${response.status}`);
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

async function downloadAuthFileText(name, key) {
  const response = await fetch(
    `${MGMT}/auth-files/download?name=${encodeURIComponent(name)}`,
    { headers: { Authorization: `Bearer ${key}` } }
  );
  if (!response.ok) throw Error(`HTTP ${response.status}`);
  return response.text();
}

// Some management-panel paths HTML-escape query separators. OAuth URLs must
// reach Kiro with literal ampersands or its query parameters are corrupted.
function cleanOAuthURL(value) {
  return String(value || '')
    .replace(/&amp;/gi, '&')
    .replace(/&#38;|&#x26;/gi, '&');
}

// Codex usage is fetched live the same way the official panel does: proxy a
// real Codex API call through CPA's /v0/management/api-call using the
// credential's auth_index. CPA substitutes $TOKEN$ with the live access token.
const CODEX_USAGE_URL = 'https://chatgpt.com/backend-api/wham/usage';
const CODEX_RESET_CREDITS_URL = 'https://chatgpt.com/backend-api/wham/rate-limit-reset-credits';
const CODEX_HEADERS = {
  Authorization: 'Bearer $TOKEN$',
  'Content-Type': 'application/json',
  'User-Agent': 'codex-tui/0.149.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.149.1)',
};

// Antigravity quota protocol mirrors the CPA management panel: subscription
// comes from loadCodeAssist, while grouped 5-hour/weekly buckets come from
// retrieveUserQuotaSummary.
const ANTIGRAVITY_QUOTA_URLS = [
  'https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary',
  'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary',
  'https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary',
];
const ANTIGRAVITY_LOAD_CODE_ASSIST_URL = 'https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist';
const ANTIGRAVITY_USER_AGENT = 'antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)';
const ANTIGRAVITY_HEADERS = {
  Authorization: 'Bearer $TOKEN$',
  'Content-Type': 'application/json',
  'User-Agent': ANTIGRAVITY_USER_AGENT,
};
const ANTIGRAVITY_LOAD_BODY = JSON.stringify({ metadata: { ideType: 'ANTIGRAVITY' } });

// apiCall proxies an upstream HTTP call through CPA and normalizes the body to
// parsed JSON when possible (mirrors management-center apiCallApi).
async function apiCall(mgmtKey, payload) {
  const res = await request('/api-call', mgmtKey, {
    base: MGMT,
    method: 'POST',
    body: JSON.stringify(payload),
  });
  const statusCode = Number(res?.status_code ?? 0);
  let body = res?.body;
  if (typeof body === 'string') {
    const trimmed = body.trim();
    if (trimmed) {
      try {
        body = JSON.parse(trimmed);
      } catch {
        /* keep string */
      }
    }
  }
  return { statusCode, body, header: res?.header || {} };
}

function codexAccountId(file) {
  const t = file?.id_token || {};
  return (
    t['chatgpt_account_id'] ||
    t['chatgpt-account-id'] ||
    file?.chatgpt_account_id ||
    file?.account_id ||
    ''
  );
}

function codexRequestHeader(file) {
  const h = { ...CODEX_HEADERS };
  const acc = codexAccountId(file);
  if (acc) h['Chatgpt-Account-Id'] = acc;
  return h;
}

const asNum = (v) => {
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
};

// buildCodexWindows: 1:1 port of the management-center window classifier. It
// turns rate_limit / code_review_rate_limit / additional_rate_limits into a
// flat list of labelled windows (5h / 周 / 月, plus per-feature variants),
// classifying each by limit_window_seconds so labels stay correct.
function buildCodexWindows(payload) {
  const FIVE_HOUR = 18000;
  const WEEK = 604800;
  const MIN_MONTH = 28 * 24 * 3600;
  const MAX_MONTH = 31 * 24 * 3600;
  const windows = [];

  const winSeconds = (w) => (w ? asNum(w.limit_window_seconds ?? w.limitWindowSeconds) : null);
  const isMonthly = (w) => {
    const s = winSeconds(w);
    return s !== null && s >= MIN_MONTH && s <= MAX_MONTH;
  };
  const resetAtOf = (w) => {
    const at = asNum(w.reset_at ?? w.resetAt);
    if (at !== null) return at;
    const after = asNum(w.reset_after_seconds ?? w.resetAfterSeconds);
    return after !== null ? Math.floor(Date.now() / 1000) + after : null;
  };

  const add = (id, label, w, limitReached, allowed) => {
    if (!w) return;
    const usedRaw = asNum(w.used_percent ?? w.usedPercent);
    const reached = Boolean(limitReached) || allowed === false;
    const used = usedRaw ?? (reached ? 100 : null);
    windows.push({ id, label, usedPercent: used, resetAt: resetAtOf(w), windowSeconds: winSeconds(w) });
  };

  const pick = (info) => {
    const primary = info?.primary_window ?? info?.primaryWindow ?? null;
    const secondary = info?.secondary_window ?? info?.secondaryWindow ?? null;
    let five = null;
    let long = null;
    for (const w of [primary, secondary]) {
      if (!w) continue;
      const s = winSeconds(w);
      if (s === FIVE_HOUR && !five) five = w;
      else if ((s === WEEK || isMonthly(w)) && !long) long = w;
    }
    if (!five) five = primary && primary !== long ? primary : null;
    if (!long) long = secondary && secondary !== five ? secondary : null;
    return { five, long };
  };

  const secLabel = (w, weekly, monthly) => (isMonthly(w) ? monthly : weekly);

  const rl = payload.rate_limit ?? payload.rateLimit ?? null;
  const rlReached = rl?.limit_reached ?? rl?.limitReached;
  const rlAllowed = rl?.allowed;
  const rlW = pick(rl);
  add('five-hour', '5 小时限额', rlW.five, rlReached, rlAllowed);
  add(secLabel(rlW.long, 'weekly', 'monthly'), secLabel(rlW.long, '周限额', '月限额'), rlW.long, rlReached, rlAllowed);

  const cr = payload.code_review_rate_limit ?? payload.codeReviewRateLimit ?? null;
  const crReached = cr?.limit_reached ?? cr?.limitReached;
  const crAllowed = cr?.allowed;
  const crW = pick(cr);
  add('cr-five-hour', '代码审查 5 小时限额', crW.five, crReached, crAllowed);
  add('cr-' + secLabel(crW.long, 'weekly', 'monthly'), secLabel(crW.long, '代码审查周限额', '代码审查月度限额'), crW.long, crReached, crAllowed);

  const additional = payload.additional_rate_limits ?? payload.additionalRateLimits ?? [];
  if (Array.isArray(additional)) {
    additional.forEach((item, i) => {
      const info = item?.rate_limit ?? item?.rateLimit ?? null;
      if (!info) return;
      const name =
        item?.limit_name ?? item?.limitName ?? item?.metered_feature ?? item?.meteredFeature ?? `附加 ${i + 1}`;
      const reached = info.limit_reached ?? info.limitReached;
      const allowed = info.allowed;
      const w = pick(info);
      add(`add-${i}-five`, `${name} 5 小时限额`, w.five, reached, allowed);
      add(`add-${i}-long`, isMonthly(w.long) ? `${name} 月度限额` : `${name} 周限额`, w.long, reached, allowed);
    });
  }

  return windows;
}

function normalizeResetCredits(payload) {
  if (!payload || typeof payload !== 'object') {
    return { availableCount: null, credits: [] };
  }
  const availableCount = asNum(payload.available_count ?? payload.availableCount);
  let credits = payload.credits ?? payload.reset_credits ?? payload.resetCredits ?? [];
  if (!Array.isArray(credits)) credits = [];
  const norm = credits
    .map((c) => ({ expiresAt: c?.expires_at ?? c?.expiresAt ?? '' }))
    .filter((c) => c.expiresAt);
  return { availableCount: availableCount ?? (norm.length || null), credits: norm };
}

// fetchCodexQuota gathers the live usage payload plus reset-credit detail for a
// single Codex credential, returning the shape CodexQuotaSection renders.
async function fetchCodexQuota(mgmtKey, file) {
  if (!file.auth_index) throw Error('该凭证缺少运行时 auth_index');
  const header = codexRequestHeader(file);
  const usage = await apiCall(mgmtKey, {
    authIndex: file.auth_index,
    method: 'GET',
    url: CODEX_USAGE_URL,
    header,
  });
  if (usage.statusCode < 200 || usage.statusCode >= 300) {
    throw Error(`用量接口返回 ${usage.statusCode || '错误'}`);
  }
  const payload = usage.body && typeof usage.body === 'object' ? usage.body : {};

  let resetCredits = normalizeResetCredits(payload.rate_limit_reset_credits ?? payload.rateLimitResetCredits);
  try {
    const rc = await apiCall(mgmtKey, {
      authIndex: file.auth_index,
      method: 'GET',
      url: CODEX_RESET_CREDITS_URL,
      header: { ...header, Accept: 'application/json', 'OpenAI-Beta': 'codex-1', Originator: 'Codex Desktop' },
    });
    if (rc.statusCode >= 200 && rc.statusCode < 300 && rc.body && typeof rc.body === 'object') {
      const detail = normalizeResetCredits(rc.body);
      if (detail.credits.length || detail.availableCount != null) resetCredits = detail;
    }
  } catch {
    /* reset-credit detail is best-effort */
  }

  return {
    plan: payload.plan_type ?? payload.planType ?? '',
    activeUntil: file.id_token?.chatgpt_subscription_active_until || '',
    resetCreditsAvailable: resetCredits.availableCount,
    resetCreditExpiries: resetCredits.credits,
    windows: buildCodexWindows(payload),
  };
}

function normalizeAntigravityString(value) {
  return typeof value === 'string' ? value.trim() : '';
}

function normalizeAntigravityProjectId(value) {
  if (typeof value === 'string') return value.trim();
  if (value && typeof value === 'object' && typeof value.id === 'string') return value.id.trim();
  return '';
}

async function resolveAntigravityProjectId(mgmtKey, file) {
  const direct = normalizeAntigravityProjectId(file.project_id || file.projectId);
  if (direct) return direct;

  const metadata = file.metadata && typeof file.metadata === 'object' ? file.metadata : null;
  const metadataProject = normalizeAntigravityProjectId(metadata?.project_id || metadata?.projectId);
  if (metadataProject) return metadataProject;

  const attributes = file.attributes && typeof file.attributes === 'object' ? file.attributes : null;
  const attributesProject = normalizeAntigravityProjectId(
    attributes?.project_id || attributes?.projectId || attributes?.gemini_virtual_project
  );
  if (attributesProject) return attributesProject;

  try {
    const text = await downloadAuthFileText(file.name, mgmtKey);
    const parsed = JSON.parse(text);
    const topLevel = normalizeAntigravityProjectId(parsed?.project_id || parsed?.projectId);
    if (topLevel) return topLevel;
    const installed = parsed?.installed && typeof parsed.installed === 'object' ? parsed.installed : null;
    const installedProject = normalizeAntigravityProjectId(installed?.project_id || installed?.projectId);
    if (installedProject) return installedProject;
    const web = parsed?.web && typeof parsed.web === 'object' ? parsed.web : null;
    return normalizeAntigravityProjectId(web?.project_id || web?.projectId);
  } catch {
    return '';
  }
}

function parseAntigravityPayload(value) {
  if (typeof value === 'string') {
    try {
      return JSON.parse(value.trim());
    } catch {
      return null;
    }
  }
  if (value && typeof value === 'object') {
    if (value.body && typeof value.body === 'object') return value.body;
    return value;
  }
  return null;
}

function buildAntigravityQuotaGroups(payload) {
  const groups = Array.isArray(payload?.groups) ? payload.groups : [];
  return groups.reduce((result, group, groupIndex) => {
    const label = normalizeAntigravityString(group.displayName || group.display_name) || `Quota Group ${groupIndex + 1}`;
    const groupId = label.toLowerCase().replace(/[^a-z0-9]+/g, '-') || `quota-group-${groupIndex + 1}`;
    const buckets = (Array.isArray(group.buckets) ? group.buckets : []).reduce((items, bucket, bucketIndex) => {
      const remainingFraction = asNum(bucket.remainingFraction ?? bucket.remaining_fraction);
      if (remainingFraction == null) return items;
      const window = normalizeAntigravityString(bucket.window);
      const id = normalizeAntigravityString(bucket.bucketId || bucket.bucket_id) || `${groupId}-${window || `bucket-${bucketIndex + 1}`}`;
      items.push({
        id,
        label: normalizeAntigravityString(bucket.displayName || bucket.display_name) || id,
        window,
        remainingFraction: Math.max(0, Math.min(1, remainingFraction)),
        resetTime: normalizeAntigravityString(bucket.resetTime || bucket.reset_time),
        description: normalizeAntigravityString(bucket.description),
      });
      return items;
    }, []);
    if (buckets.length) result.push({
      id: groupId,
      label,
      description: normalizeAntigravityString(group.description),
      buckets: buckets.sort((a, b) => {
        const rank = (value) => {
          const key = value.trim().toLowerCase();
          if (key === '5h' || key === 'five-hour' || key === 'five_hour') return 0;
          if (key === 'weekly' || key === 'week') return 1;
          return 2;
        };
        return rank(a.window) - rank(b.window);
      }),
    });
    return result;
  }, []);
}

function antigravitySubscription(value) {
  const current = value?.currentTier || value?.current_tier;
  const paid = value?.paidTier || value?.paid_tier;
  const tier = paid?.id ? paid : current;
  if (!tier || typeof tier !== 'object') return null;
  const tierId = normalizeAntigravityString(tier.id);
  const plans = {
    'free-tier': 'Free',
    'g1-pro-tier': 'Pro',
    'g1-ultra-tier': 'Ultra',
    'g1-ultra-lite-tier': 'Ultra Lite',
  };
  return { plan: plans[tierId] || normalizeAntigravityString(tier.name) || tierId || 'Unknown', tierId };
}

function antigravityServerTimeOffset(header) {
  const entry = Object.entries(header || {}).find(([key]) => key.toLowerCase() === 'date');
  const raw = Array.isArray(entry?.[1]) ? entry[1][0] : entry?.[1];
  const serverTime = raw ? new Date(raw).getTime() : NaN;
  return Number.isNaN(serverTime) ? 0 : serverTime - Date.now();
}

function formatAntigravityDuration(deltaMs) {
  const totalMinutes = Math.max(1, Math.ceil(deltaMs / 60000));
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days) return `${days} 天 ${hours} 小时`;
  if (hours) return `${hours} 小时 ${minutes} 分钟`;
  if (minutes) return `${minutes} 分钟`;
  return '不到 1 分钟';
}

function formatAntigravityResetLabel(resetTime, nowMs) {
  if (!resetTime) return '—';
  const resetMs = new Date(resetTime).getTime();
  if (Number.isNaN(resetMs)) return '—';
  const deltaMs = resetMs - nowMs;
  return deltaMs <= 0 ? '额度可用' : `额度可用 ${formatAntigravityDuration(deltaMs)} 后刷新`;
}

function translateAntigravityGroupLabel(label) {
  const key = label.trim().toLowerCase().replace(/\s+/g, ' ');
  if (key === 'gemini models') return 'GEMINI 模型';
  if (key === 'claude and gpt models') return 'CLAUDE 和 GPT 模型';
  return label;
}

function translateAntigravityBucketLabel(label) {
  const key = label.trim().toLowerCase().replace(/\s+/g, ' ');
  if (key === '5 hour limit' || key === '5-hour limit' || key === 'five hour limit') return 'Five Hour Limit Remaining';
  if (key === 'weekly limit') return 'Weekly Limit Remaining';
  if (key === 'daily limit') return 'Daily Limit Remaining';
  if (key === 'monthly limit') return 'Monthly Limit Remaining';
  return label;
}

function translateAntigravityDescription(value) {
  const match = String(value || '').match(/^models within this group:\s*(.+)$/i);
  return match ? `此分组包含: ${match[1].trim()}` : value;
}

async function fetchAntigravityQuota(mgmtKey, file) {
  if (!file.auth_index) throw Error('该凭证缺少运行时 auth_index');
  const projectId = await resolveAntigravityProjectId(mgmtKey, file);
  if (!projectId) throw Error('该凭证缺少 Antigravity project_id');

  let subscription = null;
  try {
    const loaded = await apiCall(mgmtKey, {
      authIndex: file.auth_index,
      method: 'POST',
      url: ANTIGRAVITY_LOAD_CODE_ASSIST_URL,
      header: ANTIGRAVITY_HEADERS,
      data: ANTIGRAVITY_LOAD_BODY,
    });
    if (loaded.statusCode >= 200 && loaded.statusCode < 300) subscription = antigravitySubscription(loaded.body);
  } catch {
    /* Quota data remains useful when subscription lookup fails. */
  }

  let lastError = '';
  let lastStatus = 0;
  for (const url of ANTIGRAVITY_QUOTA_URLS) {
    const result = await apiCall(mgmtKey, {
      authIndex: file.auth_index,
      method: 'POST',
      url,
      header: ANTIGRAVITY_HEADERS,
      data: JSON.stringify({ project: projectId }),
    });
    if (result.statusCode < 200 || result.statusCode >= 300) {
      lastStatus = result.statusCode;
      lastError = result.body?.error?.message || result.body?.message || `HTTP ${result.statusCode}`;
      continue;
    }
    const payload = parseAntigravityPayload(result.body ?? result.bodyText);
    const groups = buildAntigravityQuotaGroups(payload);
    if (!groups.length) {
      lastError = 'Antigravity 返回了空的额度分组';
      continue;
    }
    return { groups, subscription, serverTimeOffsetMs: antigravityServerTimeOffset(result.header) };
  }
  throw Error(lastError || `额度接口返回 ${lastStatus || '错误'}`);
}

function fmtNum(n) {
  const v = Number(n);
  if (!Number.isFinite(v)) return '0';
  return v % 1 === 0 ? v.toLocaleString('en-US') : v.toFixed(2);
}

// Map CPA recent_requests buckets onto right-aligned cells, 1:1 with the
// official panel. Each bucket keeps its success rate so the status bar uses
// the same red -> amber -> green interpolation as CPA.
const STATUS_CELLS = 20;
const STATUS_CELL_MS = 10 * 60 * 1000;

function pad2(n) {
  return n.toString().padStart(2, '0');
}

function clock(ms) {
  const d = new Date(ms);
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

// One detail per right-aligned cell: state + counts + time window, so each
// block can show the same hover tooltip the official panel does.
function buildStatusCells(recent) {
  const buckets = Array.isArray(recent) ? recent : [];
  const tail = buckets.slice(-STATUS_CELLS);
  const pad = STATUS_CELLS - tail.length;
  const stats = [
    ...Array.from({ length: pad }, () => ({ success: 0, failed: 0 })),
    ...tail.map((b) => ({
      success: Number(b?.success) || 0,
      failed: Number(b?.failed ?? b?.failure) || 0,
    })),
  ];
  const windowStart = Date.now() - STATUS_CELLS * STATUS_CELL_MS;
  return stats.map((b, i) => {
    const total = b.success + b.failed;
    const state = total === 0 ? 'idle' : b.failed === 0 ? 'success' : b.success === 0 ? 'failure' : 'mixed';
    const startTime = windowStart + i * STATUS_CELL_MS;
    return {
      state,
      rate: total > 0 ? b.success / total : -1,
      success: b.success,
      failure: b.failed,
      startTime,
      endTime: startTime + STATUS_CELL_MS,
    };
  });
}

function rateToColor(rate) {
  const t = Math.max(0, Math.min(1, rate));
  const segment = t < 0.5 ? 0 : 1;
  const localT = segment === 0 ? t * 2 : (t - 0.5) * 2;
  const stops = [
    [239, 68, 68],
    [250, 204, 21],
    [34, 197, 94],
  ];
  const from = stops[segment];
  const to = stops[segment + 1];
  const rgb = from.map((value, index) => Math.round(value + (to[index] - value) * localT));
  return `rgb(${rgb.join(', ')})`;
}

function StatusBar({ success, failure, recent }) {
  const total = (success || 0) + (failure || 0);
  const hasData = total > 0;
  const successRate = hasData ? (success / total) * 100 : 0;
  const cells = buildStatusCells(recent);
  const [active, setActive] = useState(null);
  const rateClass = !hasData
    ? ''
    : successRate >= 90
      ? 'statusRateHigh'
      : successRate >= 50
        ? 'statusRateMedium'
        : 'statusRateLow';
  const rounded = successRate.toFixed(1);
  const rateText = hasData ? `${rounded.endsWith('.0') ? rounded.slice(0, -2) : rounded}%` : '--';
  const posClass = (i) => (i <= 2 ? ' statusTooltipLeft' : i >= STATUS_CELLS - 3 ? ' statusTooltipRight' : '');
  return (
    <div className="statusBar">
      <div className="statusBlocks">
        {cells.map((c, i) => (
          <div
            className={`statusBlockWrapper${active === i ? ' statusBlockActive' : ''}`}
            key={i}
            onMouseEnter={() => setActive(i)}
            onMouseLeave={() => setActive(null)}
          >
            <div
              className={`statusBlock statusBlock-${c.state}`}
              style={c.rate >= 0 ? { backgroundColor: rateToColor(c.rate) } : undefined}
            />
            {active === i && (
              <div className={`statusTooltip${posClass(i)}`}>
                <span className="tooltipTime">{clock(c.startTime)} – {clock(c.endTime)}</span>
                {c.success + c.failure > 0 ? (
                  <span className="tooltipStats">
                    <span className="tooltipSuccess">成功 {c.success}</span>
                    <span className="tooltipFailure">失败 {c.failure}</span>
                    <span>({(c.rate * 100).toFixed(1)}%)</span>
                  </span>
                ) : (
                  <span className="tooltipStats">无请求</span>
                )}
              </div>
            )}
          </div>
        ))}
      </div>
      <span className={`statusRate ${rateClass}`}>{rateText}</span>
    </div>
  );
}

function QuotaSection({ account, brandIcon }) {
  if (account?.error) {
    return <div className="quotaError">额度获取失败：{account.error}</div>;
  }
  const usage = account?.usage || [];
  return (
    <div className="quotaBox">
      <div className="quotaHead">
        <span className="quotaTitle">
          {brandIcon && <img src={brandIcon} alt="" className="quotaBrandIcon" />}
          {esc(account?.subscription || '订阅')}
        </span>
        {account?.next_reset && <span className="quotaReset">重置于 {formatDate(account.next_reset)}</span>}
      </div>
      {usage.length === 0 && <div className="quotaEmpty">无用量数据</div>}
      {usage.map((u, i) => {
        const pct = Number(u.usage_percent) || 0;
        const barPct = Math.max(0, Math.min(100, pct));
        const over = pct > 100;
        const barClass = over || pct >= 90 ? 'barLow' : pct >= 50 ? 'barMed' : 'barHigh';
        const overages = Number(u.current_overages) || 0;
        const charges = Number(u.overage_charges) || 0;
        const currency = esc(u.currency || 'USD');
        return (
          <div className="quotaItem" key={i}>
            <div className="quotaItemHead">
              <span>{esc(u.display_name || u.resource_type)}</span>
              <span className="quotaItemNums">
                {fmtNum(u.current_usage)} / {fmtNum(u.usage_limit)} {esc(u.unit || '')}
              </span>
            </div>
            <div className="quotaBar">
              <i className={barClass} style={{ width: `${barPct}%` }} />
            </div>
            <div className="quotaItemFoot">
              <span>{over ? '已超额' : `剩余 ${fmtNum(u.remaining)}`}</span>
              <span className={over ? 'overPct' : ''}>{pct.toFixed(1)}%</span>
            </div>
            {over && (
              <div className="overageRow">
                <span>超额 {fmtNum(overages)} {esc(u.unit || '')}</span>
                <span className="overageCharge">
                  约 {charges.toFixed(2)} {currency}
                  {Number(u.overage_rate) > 0 && ` · 单价 ${Number(u.overage_rate)} ${currency}`}
                </span>
              </div>
            )}
            {!over && charges > 0 && (
              <div className="overageRow">
                <span>超额费用</span>
                <span className="overageCharge">约 {charges.toFixed(2)} {currency}</span>
              </div>
            )}
          </div>
        );
      })}
      {account?.overage_status && (
        <div className="quotaMeta">超额计费：{account.overage_status === 'ENABLED' ? '已开启' : esc(account.overage_status)}</div>
      )}
    </div>
  );
}

// Codex plan chips: 套餐 label reflects the tier (pro -> Pro 20x, plus -> Plus).
function codexPlanLabel(plan) {
  const p = normalizeKey(plan);
  if (!p) return '';
  if (p === 'pro') return 'Pro';
  if (p === 'plus') return 'Plus';
  if (p === 'team') return 'Team';
  if (p === 'free') return 'Free';
  if (p === 'business' || p === 'enterprise') return p.charAt(0).toUpperCase() + p.slice(1);
  return plan;
}

// CodexQuotaSection renders the live wham/usage payload the same way the
// official management panel does: a plan / 续期 / 主动重置次数 header, an
// optional reset-credit expiry list, then one water-line meter per usage
// window (5h / 周 / 月 and any per-feature windows).
function CodexQuotaSection({ quota, brandIcon }) {
  if (!quota) return null;
  const windows = quota.windows || [];
  const plan = codexPlanLabel(quota.plan);
  const resetCount = quota.resetCreditsAvailable;
  const resetExpiries = quota.resetCreditExpiries || [];
  const expiryRel = quota.activeUntil ? relTimeLabel(quota.activeUntil) : '';
  return (
    <div className="quotaBox">
      <div className="quotaHead">
        <span className="quotaTitle">
          {brandIcon && <img src={brandIcon} alt="" className="quotaBrandIcon" />}
          Codex
        </span>
      </div>
      {(plan || quota.activeUntil || resetCount != null) && (
        <div className="codexPlanRow">
          {plan && (
            <span className="codexChip">
              <span className="codexChipLabel">套餐</span>
              <span className="codexChipValue">{plan}</span>
            </span>
          )}
          {quota.activeUntil && (
            <span className="codexChip">
              <span className="codexChipLabel">续期时间</span>
              <span className="codexChipValue">{formatDate(quota.activeUntil)}</span>
              {expiryRel && <span className="codexChipRel">{expiryRel}</span>}
            </span>
          )}
        </div>
      )}
      {windows.length === 0 && <div className="quotaEmpty">暂无用量窗口</div>}
      {windows.map((w) => {
        const used = w.usedPercent == null ? null : Math.max(0, Math.min(100, w.usedPercent));
        // Show remaining rather than used: 100 - used, and drive the bar with it.
        const remaining = used == null ? null : 100 - used;
        const pct = remaining == null ? 0 : remaining;
        const barClass = pct <= 10 ? 'barLow' : pct <= 50 ? 'barMed' : 'barHigh';
        return (
          <div className="quotaItem" key={w.id}>
            <div className="quotaItemHead">
              <span>{w.label}</span>
              <span className="quotaItemNums">{remaining == null ? '--' : `剩余 ${pct.toFixed(0)}%`}</span>
            </div>
            <div className="quotaBar">
              <i className={barClass} style={{ width: `${pct}%` }} />
            </div>
            {w.resetAt ? (
              <div className="quotaItemFoot">
                <span>重置于 {resetLabel(w.resetAt)}</span>
                {relTimeLabel(w.resetAt) && <span>{relTimeLabel(w.resetAt)}</span>}
              </div>
            ) : null}
          </div>
        );
      })}
      {(resetCount != null || resetExpiries.length > 0) && (
        <div className="resetCreditFooter">
          <span>主动重置次数 {resetCount ?? '--'}</span>
          {resetExpiries.map((item, i) => (
            <span key={`${item.expiresAt}-${i}`}>过期于 {formatDate(item.expiresAt)}</span>
          ))}
        </div>
      )}
    </div>
  );
}

function ClockIcon({ size = 15 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </svg>
  );
}

function PlayIcon({ size = 14 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M8 5.5v13a1 1 0 0 0 1.53.85l9.5-6.5a1 1 0 0 0 0-1.7l-9.5-6.5A1 1 0 0 0 8 5.5Z" />
    </svg>
  );
}

function InfoIcon({ size = 14 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="10" />
      <path d="M12 16v-4" />
      <path d="M12 8h.01" />
    </svg>
  );
}

function formatCountdown(when) {
  if (!when) return '';
  const target = new Date(when).getTime();
  if (!Number.isFinite(target)) return '';
  let seconds = Math.ceil((target - Date.now()) / 1000);
  if (seconds <= 0) return '';
  const days = Math.floor(seconds / 86400);
  seconds %= 86400;
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  seconds %= 60;
  const parts = [];
  if (days) parts.push(`${days}天`);
  if (hours || days) parts.push(`${hours}小时`);
  if (minutes || hours || days) parts.push(`${minutes}分`);
  parts.push(`${seconds}秒`);
  return parts.join('');
}

function normalizeClineModelStates(value) {
  if (Array.isArray(value)) {
    return value
      .map((item) => ({ model: item?.model || item?.id || '', ...(item || {}) }))
      .filter((item) => item.model);
  }
  if (!value || typeof value !== 'object') return [];
  return Object.entries(value)
    .map(([model, state]) => ({ model, ...(state && typeof state === 'object' ? state : {}) }))
    .sort((a, b) => a.model.localeCompare(b.model));
}

function clineModelLabel(model) {
  return String(model || '').replace(/^nexus\//i, '');
}

function ClineQuotaSection({ account, brandIcon }) {
  const states = normalizeClineModelStates(account?.model_quotas);
  const [, setClock] = useState(0);
  const hasCountdown = states.some((state) => state.status === 'limited' && formatCountdown(state.reset_at));
  useEffect(() => {
    if (!hasCountdown) return undefined;
    const timer = setInterval(() => setClock((value) => value + 1), 1000);
    return () => clearInterval(timer);
  }, [hasCountdown]);

  if (account?.error) {
    return <div className="quotaError">额度获取失败：{account.error}</div>;
  }

  const balance = account?.balance ?? account?.quotas?.[0]?.remaining;
  const limited = states.filter((state) => state.status === 'limited');
  const available = states.filter((state) => state.status === 'available');
  const activeLimited = limited.filter((state) => !state.reset_at || formatCountdown(state.reset_at));
  const summary = activeLimited.length
    ? `已限制 ${activeLimited.length} 个模型`
    : available.length
      ? `最近确认 ${available.length} 个模型可用`
      : '待检测';
  const summaryClass = activeLimited.length ? 'clineStatusLimited' : available.length ? 'clineStatusAvailable' : 'clineStatusPending';

  return (
    <div className="quotaBox clineQuotaBox">
      <div className="quotaHead">
        <span className="quotaTitle">
          {brandIcon && <img src={brandIcon} alt="" className="quotaBrandIcon" />}
          {esc(account?.plan || 'Cline Free')}
        </span>
        {account?.email && <span className="quotaReset" title={account.email}>{account.email}</span>}
      </div>

      {balance != null && (
        <div className="clineCredits">
          <span>Credits</span>
          <strong>{fmtNum(balance)}</strong>
        </div>
      )}

      <div className="clineFreeStatus">
        <div className="clineFreeStatusHead">
          <span className="quotaItemHeadLabel">免费推理状态</span>
          <span className={`clineStatus ${summaryClass}`}>{summary}</span>
        </div>
        {!states.length ? (
          <div className="quotaEmpty">待检测：等待一次真实模型请求结果</div>
        ) : (
          <div className="clineModelList">
            {states.map((state) => {
              const countdown = state.status === 'limited' ? formatCountdown(state.reset_at) : '';
              const expired = state.status === 'limited' && state.reset_at && !countdown;
              let label = '待确认';
              let className = 'clineStatusPending';
              if (state.status === 'available') {
                label = '最近可用';
                className = 'clineStatusAvailable';
              } else if (state.status === 'limited') {
                label = countdown ? `限制中 · ${countdown}` : expired ? '等待真实请求确认' : '已达到上限';
                className = countdown ? 'clineStatusLimited' : 'clineStatusPending';
              }
              return (
                <div className="clineModelRow" key={state.model}>
                  <code>{clineModelLabel(state.model)}</code>
                  <span className={`clineStatus ${className}`}>{label}</span>
                </div>
              );
            })}
          </div>
        )}
      </div>
      <div className="quotaMeta">状态来自最近一次真实请求；刷新只查询账户余额，不会主动调用模型</div>
    </div>
  );
}

function AntigravityQuotaSection({ quota }) {
  if (!quota) return null;
  const groups = quota.groups || [];
  const serverTimeOffsetMs = Number(quota.serverTimeOffsetMs) || 0;
  const [, setClock] = useState(0);
  const hasResetTime = groups.some((group) => group.buckets?.some((bucket) => bucket.resetTime));
  useEffect(() => {
    if (!hasResetTime) return undefined;
    const timer = setInterval(() => setClock((value) => value + 1), 30000);
    return () => clearInterval(timer);
  }, [hasResetTime]);

  const nowMs = Date.now() + serverTimeOffsetMs;
  const plan = quota.subscription?.plan || '';
  const normalizedPlan = normalizeKey(plan).replace(/\s+/g, '-');
  const isPremiumPlan = normalizedPlan === 'ultra' || normalizedPlan === 'ultra-lite';
  const urgentResetId = groups
    .flatMap((group) => group.buckets || [])
    .filter((bucket) => {
      const resetMs = new Date(bucket.resetTime).getTime();
      return Number.isFinite(resetMs) && resetMs > nowMs && resetMs - nowMs < 60 * 60 * 1000;
    })
    .sort((a, b) => new Date(a.resetTime).getTime() - new Date(b.resetTime).getTime())[0]?.id || null;

  return (
    <div className="quotaSection antigravityQuotaSection">
      {plan && (
        <div className="codexPlan">
          <span className="codexPlanItem">
            <span className="codexPlanLabel">套餐</span>
            <span className={isPremiumPlan ? 'premiumPlanValue' : 'codexPlanValue'}>{esc(plan)}</span>
          </span>
        </div>
      )}
      {groups.length === 0 && <div className="quotaEmpty">暂无模型额度数据</div>}
      {groups.map((group) => (
        <div className="antigravityQuotaGroup" key={group.id}>
          <div className="antigravityQuotaGroupHeader">
            <span className="antigravityQuotaGroupTitle">{translateAntigravityGroupLabel(group.label)}</span>
            {group.description && (
              <span className="antigravityQuotaGroupDescription">
                {translateAntigravityDescription(group.description)}
              </span>
            )}
          </div>
          {(group.buckets || []).map((bucket) => {
            const fraction = Math.max(0, Math.min(1, Number(bucket.remainingFraction) || 0));
            const percent = fraction * 100;
            const percentLabel = fraction >= 1 ? '额度可用' : `剩余 ${Math.round(percent)}%`;
            const resetLabel = formatAntigravityResetLabel(bucket.resetTime, nowMs);
            const isUrgent = bucket.id === urgentResetId;
            const fillClass = percent >= 70 ? 'quotaBarFillHigh' : percent >= 30 ? 'quotaBarFillMedium' : 'quotaBarFillLow';
            return (
              <div className="quotaRow" key={bucket.id}>
                <div className="quotaRowHeader">
                  <span className="quotaModel" title={translateAntigravityDescription(bucket.description)}>
                    {translateAntigravityBucketLabel(bucket.label)}
                  </span>
                  <div className="quotaMeta">
                    <span className="quotaPercent">{percentLabel}</span>
                    <span className={`quotaReset${isUrgent ? ' quotaResetRelativeSoon' : ''}`}>
                      {resetLabel}
                    </span>
                  </div>
                </div>
                <div className="quotaBar">
                  <i className={`quotaBarFill ${fillClass}`} style={{ width: `${percent}%` }} />
                </div>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}

function AntigravityQuotaPlaceholder({ loading, error, hasAuthIndex, onRefresh }) {
  return (
    <div className="antigravityQuotaSection">
      {error ? (
        <div className="quotaError">额度获取失败：{error}</div>
      ) : (
        <button className="quotaMessage quotaMessageAction" onClick={onRefresh} disabled={!hasAuthIndex || loading}>
          {loading ? '刷新中…' : hasAuthIndex ? '点击此处刷新额度' : '该凭证缺少运行时 auth_index'}
        </button>
      )}
    </div>
  );
}

function formatDate(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return esc(iso);
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}/${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function formatFileSize(value) {
  const size = Number(value);
  if (!Number.isFinite(size) || size <= 0) return '-';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(2)} KB`;
  return `${(size / (1024 * 1024)).toFixed(2)} MB`;
}

function formatModified(value) {
  const normalized = typeof value === 'string' ? value.trim() : value;
  if (normalized === '' || normalized == null) return '';
  const raw = Number(normalized);
  const date = Number.isFinite(raw)
    ? new Date(raw < 1e12 ? raw * 1000 : raw)
    : new Date(normalized);
  if (Number.isNaN(date.getTime())) return '';
  const p = (n) => String(n).padStart(2, '0');
  return `${p(date.getDate())}/${p(date.getMonth() + 1)}/${date.getFullYear()}, ${p(date.getHours())}:${p(date.getMinutes())}:${p(date.getSeconds())}`;
}

const HEALTHY_AUTH_FILE_STATUS_MESSAGES = new Set(['ok', 'healthy', 'ready', 'success', 'available']);

function authFileStatusMessage(file) {
  const raw = file?.status_message ?? file?.statusMessage;
  if (typeof raw === 'string') return raw.trim();
  if (raw == null) return '';
  return String(raw).trim();
}

function hasAuthFileStatusWarning(file) {
  const status = normalizeKey(file?.status);
  if (file?.unavailable === true || status === 'error') return true;
  const message = authFileStatusMessage(file);
  return Boolean(message) && !HEALTHY_AUTH_FILE_STATUS_MESSAGES.has(message.toLowerCase());
}

// Detect provider from a CPA auth-file id (e.g. "codex-...", "kiro-...").
function providerFromId(id) {
  const m = String(id || '').match(/^([a-z]+)-/i);
  return normalizeKey(m ? m[1] : '');
}

// normalizeAuthFiles maps CPA /auth-files records to the card shape and
// dedupes credentials that point at the same underlying file. Exact repeats
// of the same path + auth_index are one CPA registration viewed twice and are
// silently collapsed; different registrations still keep the warning.
function normalizeAuthFiles(files) {
  const byKey = new Map();
  for (const f of files) {
    const rawId = f.id || f.name || '';
    const provider = providerFromId(rawId) || normalizeKey(f.account_type);
    const dedupKey = `${provider}::${rawId.replace(/\.json$/i, '')}`;
    const mapped = {
      name: rawId,
      id: rawId,
      auth_index: f.auth_index || '',
      path: f.path || f.source || '',
      provider,
      type: provider,
      project_id: f.project_id || f.projectId || '',
      projectId: f.projectId || f.project_id || '',
      metadata: f.metadata || null,
      attributes: f.attributes || null,
      label: f.label || f.account || f.email || rawId,
      email: f.email || f.account || '',
      disabled: !!f.disabled,
      status: f.status || '',
      status_message: f.status_message ?? f.statusMessage ?? '',
      statusMessage: f.statusMessage ?? f.status_message ?? '',
      unavailable: f.unavailable === true || normalizeKey(f.unavailable) === 'true',
      success: Number(f.success) || 0,
      failed: Number(f.failed) || 0,
      recent_requests: Array.isArray(f.recent_requests) ? f.recent_requests : null,
      model_quotas: f.model_quotas || null,
      id_token: f.id_token || null,
      size: Number(f.size) || 0,
      modified: f.modified ?? f.modtime ?? f.updated_at ?? f.last_refresh ?? '',
      priority: Number.isSafeInteger(Number(f.priority)) ? Number(f.priority) : undefined,
      weight: Number.isSafeInteger(Number(f.weight)) ? Number(f.weight) : undefined,
      note: typeof f.note === 'string' ? f.note.trim() : '',
    };
    const existing = byKey.get(dedupKey);
    if (!existing) {
      byKey.set(dedupKey, mapped);
    } else {
      // Merge counts, prefer the entry that carries quota signals, and flag
      // that CPA has this credential registered more than once.
      existing.success = Math.max(existing.success, mapped.success);
      existing.failed = Math.max(existing.failed, mapped.failed);
      existing.recent_requests = existing.recent_requests || mapped.recent_requests;
      existing.model_quotas = existing.model_quotas || mapped.model_quotas;
      existing.size = existing.size || mapped.size;
      existing.modified = existing.modified || mapped.modified;
      existing.priority ??= mapped.priority;
      existing.weight ??= mapped.weight;
      existing.note = existing.note || mapped.note;
      existing.status = existing.status || mapped.status;
      existing.status_message = existing.status_message || mapped.status_message;
      existing.statusMessage = existing.statusMessage || mapped.statusMessage;
      existing.unavailable = existing.unavailable || mapped.unavailable;
      const sameRegistration =
        String(existing.path || '').trim() === String(mapped.path || '').trim() &&
        String(existing.auth_index || '').trim() === String(mapped.auth_index || '').trim();
      if (!sameRegistration) existing.duplicate_count = (existing.duplicate_count || 1) + 1;
    }
  }
  return Array.from(byKey.values());
}

// Relative label like "21天前" / "5天后" for an ISO/epoch time vs now.
function relTimeLabel(when) {
  if (!when) return '';
  const t = typeof when === 'number' ? when * 1000 : new Date(when).getTime();
  if (!Number.isFinite(t)) return '';
  const diffDays = Math.round((t - Date.now()) / 86400000);
  if (diffDays === 0) return '今天';
  return diffDays > 0 ? `${diffDays}天后` : `${-diffDays}天前`;
}

function resetLabel(epochSec) {
  if (!epochSec) return '';
  return formatDate(new Date(epochSec * 1000).toISOString());
}

function Card({ file, mgmtKey, onChanged, refreshAll, onOpenQuotaTrigger }) {
  const providerKey = normalizeKey(file.provider || file.type || 'kiro');
  const color = typeColor(providerKey);
  const label = typeLabel(providerKey);
  const icon = iconFor(providerKey);
  const rawAccount = file.email || file.label || file.name || 'Kiro account';
  const account = providerKey === 'cline' && normalizeKey(rawAccount) === 'kiro' ? 'Cline' : rawAccount;
  const fileName = file.name || '';
  const disabled = !!file.disabled;
  const rawStatusMessage = authFileStatusMessage(file);
  const statusWarning = hasAuthFileStatusWarning(file);
  const isKiro = providerKey === 'kiro';
  const isCline = providerKey === 'cline';
  const isAntigravity = providerKey === 'antigravity';
  const isCodex = providerKey === 'codex';
  const avatarStyle = { backgroundColor: color.bg, color: color.text, ...(color.border ? { border: color.border } : {}) };

  const [quota, setQuota] = useState(null);
  const [quotaLoading, setQuotaLoading] = useState(false);
  const [quotaOpen, setQuotaOpen] = useState(false);
  const [relogin, setRelogin] = useState('');
  const [reloginUrl, setReloginUrl] = useState('');
  const [reloginState, setReloginState] = useState('');
  const [reloginCallback, setReloginCallback] = useState('');
  const [reloginVerifyUrl, setReloginVerifyUrl] = useState('');
  const [reloginUserCode, setReloginUserCode] = useState('');
  const [codexQuota, setCodexQuota] = useState(null);
  const [codexLoading, setCodexLoading] = useState(false);
  const [codexError, setCodexError] = useState('');
  const [antigravityQuota, setAntigravityQuota] = useState(null);
  const [antigravityLoading, setAntigravityLoading] = useState(false);
  const [antigravityError, setAntigravityError] = useState('');
  const [selected, setSelected] = useState(false);
  const [models, setModels] = useState(null);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [statusUpdating, setStatusUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [actionError, setActionError] = useState('');
  const displayedClineQuota = quota || (isCline && file.model_quotas ? { model_quotas: file.model_quotas } : null);

  // 收起重新登录流程的三个步骤框（成功后调用，保留状态提示文案）。
  const closeReloginFlow = useCallback(() => {
    setReloginUrl('');
    setReloginState('');
    setReloginCallback('');
    setReloginVerifyUrl('');
    setReloginUserCode('');
  }, []);

  const refreshCodex = useCallback(async () => {
    if (!isCodex) return;
    setCodexLoading(true);
    setCodexError('');
    setCodexQuota(null);
    try {
      const data = await fetchCodexQuota(mgmtKey, file);
      setCodexQuota(data);
    } catch (e) {
      setCodexError(e.message || '额度获取失败');
    } finally {
      setCodexLoading(false);
    }
  }, [isCodex, mgmtKey, file]);

  const refreshAntigravity = useCallback(async () => {
    if (!isAntigravity) return;
    setAntigravityLoading(true);
    setAntigravityError('');
    setAntigravityQuota(null);
    try {
      const data = await fetchAntigravityQuota(mgmtKey, file);
      setAntigravityQuota(data);
    } catch (e) {
      setAntigravityError(e.message || '额度获取失败');
    } finally {
      setAntigravityLoading(false);
    }
  }, [file, isAntigravity, mgmtKey]);

  const refreshQuota = useCallback(async () => {
    if (!isKiro && !isCline) return;
    setQuotaLoading(true);
    setQuotaOpen(true);
    try {
      const data = await request('/quotaRequest', mgmtKey, { method: 'POST' });
      const match =
        (data.accounts || []).find(
          (a) => a.auth_index === file.auth_index || a.name === file.name
        ) || null;
      setQuota(
        match ||
          {
            error: isKiro
              ? '未返回该凭证的额度'
              : '未返回该凭证的 Cline 免费状态',
          }
      );
    } catch (e) {
      setQuota({ error: e.message });
    } finally {
      setQuotaLoading(false);
    }
  }, [mgmtKey, file.auth_index, file.name, isCline, isKiro]);

  const showModels = useCallback(async () => {
    setModelsLoading(true);
    setActionError('');
    try {
      setModels(await fetchAuthFileModels(file.name, mgmtKey));
    } catch (e) {
      setActionError(`模型获取失败：${e.message}`);
    } finally {
      setModelsLoading(false);
    }
  }, [file.name, mgmtKey]);

  const downloadFile = useCallback(async () => {
    setActionError('');
    try {
      await downloadAuthFile(file.name, mgmtKey);
    } catch (e) {
      setActionError(`下载失败：${e.message}`);
    }
  }, [file.name, mgmtKey]);

  const toggleStatus = useCallback(async () => {
    setStatusUpdating(true);
    setActionError('');
    try {
      await request('/auth-files/status', mgmtKey, {
        base: MGMT,
        method: 'PATCH',
        body: JSON.stringify({ name: file.name, disabled: !disabled }),
      });
      onChanged();
    } catch (e) {
      setActionError(`状态更新失败：${e.message}`);
    } finally {
      setStatusUpdating(false);
    }
  }, [disabled, file.name, mgmtKey, onChanged]);

  const deleteFile = useCallback(async () => {
    if (!window.confirm(`确定删除认证文件“${file.name}”吗？`)) return;
    setDeleting(true);
    setActionError('');
    try {
      await request('/auth-files', mgmtKey, {
        base: MGMT,
        method: 'DELETE',
        body: JSON.stringify({ names: [file.name] }),
      });
      onChanged();
    } catch (e) {
      setActionError(`删除失败：${e.message}`);
    } finally {
      setDeleting(false);
    }
  }, [file.name, mgmtKey, onChanged]);

  // Broadcast quota refresh: bump refreshAll from the toolbar to refresh every
  // card's quota at once (Kiro/Cline -> management quota, Codex/Antigravity -> live usage).
  useEffect(() => {
    if (!refreshAll) return;
    if ((isKiro || isCline) && file.auth_index) refreshQuota();
    else if (isAntigravity && file.auth_index) refreshAntigravity();
    else if (isCodex && file.auth_index) refreshCodex();
  }, [file.auth_index, isAntigravity, isCline, isCodex, isKiro, refreshAll, refreshAntigravity, refreshCodex, refreshQuota]);

  const startRelogin = useCallback(async () => {
    if (!file.auth_index) {
      setRelogin('该凭证无运行时 auth_index，无法重新登录');
      return;
    }
    setRelogin('启动中…');
    setReloginUrl('');
    setReloginState('');
    setReloginCallback('');
    setReloginVerifyUrl('');
    setReloginUserCode('');
    try {
      const started = await request('/oauth/relogin/start', mgmtKey, {
        method: 'POST',
        body: JSON.stringify({ auth_index: file.auth_index }),
      });
      if (!started.url || !started.state) throw Error('登录启动失败');
      const cleanURL = cleanOAuthURL(started.url);
      const browserFlow = /app\.kiro\.dev\/signin/i.test(cleanURL);
      setReloginUrl(cleanURL);
      setReloginState(started.state);
      setRelogin(browserFlow ? '打开登录链接，在浏览器完成登录后把跳转到的 localhost 回调 URL 粘贴到下方。' : '打开设备验证链接并完成授权，面板会自动等待登录完成。');
      window.open(cleanURL, '_blank', 'noopener');
      if (!browserFlow) {
        const poll = async () => {
          try {
            const s = await request(`/oauth/relogin/status?state=${encodeURIComponent(started.state)}`, mgmtKey);
            if (s.status === 'success') { setRelogin('重新登录成功'); closeReloginFlow(); onChanged(); }
            else if (s.status === 'error') setRelogin(`失败：${s.message || '未知错误'}`);
            else setTimeout(poll, 2000);
          } catch (e) { setRelogin(`失败：${e.message}`); }
        };
        poll();
      }
    } catch (e) {
      setRelogin(`失败：${e.message}`);
    }
  }, [mgmtKey, file.auth_index, onChanged, closeReloginFlow]);

  const submitReloginCallback = useCallback(async () => {
    const redirect = reloginCallback.trim();
    if (!redirect || !reloginState) return;
    setRelogin('提交回调 URL…');
    try {
      const r = await request('/oauth/callback', mgmtKey, { method: 'POST', body: JSON.stringify({ redirect_url: redirect }) });
      if (r.status === 'continue' && r.url) {
        const verify = cleanOAuthURL(r.url);
        setReloginVerifyUrl(verify); setReloginUserCode(r.user_code || '');
        setRelogin('组织登录：打开下方验证链接并确认设备代码，然后等待完成。');
        window.open(verify, '_blank', 'noopener');
      } else if (r.status === 'accepted') setRelogin('回调已接收，等待完成…');
      else throw Error(r.message || `意外响应：${r.status || '未知'}`);
      const poll = async () => {
        try {
          const s = await request(`/oauth/relogin/status?state=${encodeURIComponent(reloginState)}`, mgmtKey);
          if (s.status === 'success') { setRelogin('重新登录成功'); closeReloginFlow(); onChanged(); }
          else if (s.status === 'error') setRelogin(`失败：${s.message || '未知错误'}`);
          else setTimeout(poll, 2000);
        } catch (e) { setRelogin(`失败：${e.message}`); }
      };
      poll();
    } catch (e) { setRelogin(`回调提交失败：${e.message}`); }
  }, [mgmtKey, onChanged, reloginCallback, reloginState, closeReloginFlow]);

  return (
    <article className={`card ${disabled ? 'cardDisabled' : ''}`}>
      <header className="head">
        <input
          className="selection"
          type="checkbox"
          checked={selected}
          onChange={(event) => setSelected(event.target.checked)}
          aria-label="选择认证文件"
        />
        <div className="avatar" style={avatarStyle}>
          {icon ? <img src={icon} alt="" className="avatarImage" /> : <span className="avatarFallback">{label.slice(0, 1)}</span>}
        </div>
        <div className="identity">
          <div className="badgeRow">
            <span className="typeBadge" style={avatarStyle}>{label}</span>
            <span className={`stateBadge ${disabled ? 'stateDisabled' : statusWarning ? 'stateWarning' : 'stateActive'}`}>
              <span className="stateDot" aria-hidden="true" />
              {disabled ? '停用' : statusWarning ? '警告' : rawStatusMessage ? '健康' : '启用'}
            </span>
          </div>
          <span className="account" title={account}>{account}</span>
        </div>
      </header>

      {fileName && <p className="fileName" title={fileName}>{fileName}</p>}

      {file.duplicate_count > 1 && (
        <p className="dupNote">⚠ 该凭证在 CPA 中注册了 {file.duplicate_count} 次（同一文件），已合并显示。建议在认证文件里删除多余条目。</p>
      )}

      {rawStatusMessage && statusWarning && (
        <div className="warning" title={rawStatusMessage}>
          <InfoIcon size={14} />
          <span>{rawStatusMessage}</span>
        </div>
      )}

      <div className="health">
        <div className="healthHead">
          <span className="healthLabel">健康状态</span>
          <span className="healthCounts">
            <span className={`countOk ${(file.success || 0) > 0 ? 'countLive' : ''}`}>成功 {file.success || 0}</span>
            <span className={`countFail ${(file.failed || 0) > 0 ? 'countLive' : ''}`}>失败 {file.failed || 0}</span>
          </span>
        </div>
        <StatusBar success={file.success || 0} failure={file.failed || 0} recent={file.recent_requests} />
      </div>

      <div className="metaRow">
        <span>{formatFileSize(file.size)}</span>
        {formatModified(file.modified) && (
          <>
            <span className="metaDivider" aria-hidden="true">·</span>
            <span>{formatModified(file.modified)}</span>
          </>
        )}
        {file.priority !== undefined && (
          <>
            <span className="metaDivider" aria-hidden="true">·</span>
            <span className="metaPriority">优先级 {file.priority}</span>
          </>
        )}
        {file.weight !== undefined && (
          <>
            <span className="metaDivider" aria-hidden="true">·</span>
            <span className="metaWeight">权重 {file.weight}</span>
          </>
        )}
      </div>

      {isKiro ? (
        quota && !quota.error ? (
          <QuotaSection account={quota} brandIcon={icon} />
        ) : (
          <>
            <button className="quotaTrigger" onClick={refreshQuota} disabled={quotaLoading}>
              {quotaLoading ? '刷新中…' : '点击此处刷新额度'}
            </button>
            {quotaOpen && !quotaLoading && quota?.error && <QuotaSection account={quota} brandIcon={icon} />}
          </>
        )
      ) : isCline ? (
        <>
          {quotaOpen && <ClineQuotaSection account={displayedClineQuota} brandIcon={icon} />}
          {(!quota || quota.error) && (
            <button className="quotaTrigger" onClick={refreshQuota} disabled={quotaLoading}>
              {quotaLoading ? '查询中…' : '点击此处刷新额度'}
            </button>
          )}
        </>
      ) : isCodex ? (
        codexQuota ? (
          <CodexQuotaSection quota={codexQuota} brandIcon={icon} />
        ) : (
          <>
            <button className="quotaTrigger" onClick={refreshCodex} disabled={!file.auth_index || codexLoading}>
              {codexLoading ? '刷新中…' : file.auth_index ? '点击此处刷新额度' : '该凭证缺少运行时 auth_index'}
            </button>
            {codexError && !codexLoading && <QuotaSection account={{ error: codexError }} brandIcon={icon} />}
          </>
        )
      ) : isAntigravity ? (
        antigravityQuota ? (
          <AntigravityQuotaSection quota={antigravityQuota} />
        ) : (
          <AntigravityQuotaPlaceholder
            loading={antigravityLoading}
            error={antigravityError}
            hasAuthIndex={Boolean(file.auth_index)}
            onRefresh={refreshAntigravity}
          />
        )
      ) : (
        <div className="quotaMeta quotaEmpty">暂无额度信号，发起一次请求后由 CPA 采集</div>
      )}

      {models && (
        <div className="modelsPanel">
          <div className="modelsHead">
            <strong>支持的模型</strong>
            <button className="modelsClose" onClick={() => setModels(null)} aria-label="关闭模型列表">×</button>
          </div>
          {models.length ? (
            <div className="modelsList">
              {models.map((model) => (
                <div className="modelItem" key={model.id || model.model_id}>
                  <span>{model.display_name || model.id || model.model_id}</span>
                  {(model.display_name && model.id && model.display_name !== model.id) && <code>{model.id}</code>}
                </div>
              ))}
            </div>
          ) : <div className="quotaEmpty">该凭证暂无可用模型</div>}
        </div>
      )}

      <footer className="actions">
        <div className="actionsMain">
          <button className="btn modelButton" onClick={showModels} disabled={modelsLoading} title="查看支持的模型">
            {modelsLoading ? '加载中…' : <><ModelClusterIcon size={14} /><span>模型</span></>}
          </button>
          {(isCodex || isAntigravity) && (
            <button className="btn iconButton" onClick={() => onOpenQuotaTrigger?.(file.auth_index)} disabled={!file.auth_index} title="设置每日 Quota 唤醒" aria-label="设置每日 Quota 唤醒">
              <ClockIcon />
            </button>
          )}
          <button
            className="btn iconButton"
            onClick={isKiro || isCline ? refreshQuota : isAntigravity ? refreshAntigravity : isCodex ? refreshCodex : onChanged}
            title="刷新额度"
            disabled={quotaLoading || codexLoading || antigravityLoading}
          ><RefreshIcon /></button>
          <button className="btn iconButton" onClick={downloadFile} title="下载认证文件"><DownloadIcon /></button>
          <button className="btn iconButton dangerButton" onClick={deleteFile} title="删除认证文件" disabled={deleting}>
            {deleting ? '…' : <TrashIcon />}
          </button>
          {isKiro && (
            <button className="btn iconButton" onClick={startRelogin} title="重新登录"><RefreshIcon /></button>
          )}
        </div>
        <div className="toggleWrap">
          <span className="toggleLabel">启用</span>
          <button
            className={`toggle ${disabled ? '' : 'checked'}`}
            role="switch"
            aria-checked={!disabled}
            onClick={toggleStatus}
            disabled={statusUpdating}
            title={disabled ? '启用认证文件' : '停用认证文件'}
          >
            <span className="toggleThumb" />
          </button>
        </div>
      </footer>
      {(relogin || actionError) && <div className="reloginNote">{relogin || actionError}</div>}
      {reloginUrl && (
        <div className="oauthFlow reloginFlow">
          <div className="oauthStep">
            <span className="oauthStepLabel">{/app\.kiro\.dev\/signin/i.test(reloginUrl) ? '1. 登录链接' : '设备验证链接'}</span>
            <code className="oauthUrl">{reloginUrl}</code>
            <div className="oauthActions">
              <button className="btn" onClick={() => window.open(reloginUrl, '_blank', 'noopener')}>打开</button>
              <button className="btn" onClick={() => navigator.clipboard?.writeText(reloginUrl)}>复制</button>
            </div>
          </div>
          {/app\.kiro\.dev\/signin/i.test(reloginUrl) && (
            <div className="oauthStep">
              <span className="oauthStepLabel">2. 粘贴回调 URL</span>
              <textarea className="oauthInput" rows={2} value={reloginCallback} onChange={(e) => setReloginCallback(e.target.value)} placeholder="http://localhost:3128/signin/callback?..." />
              <div className="oauthActions"><button className="btn btnPrimary" onClick={submitReloginCallback} disabled={!reloginCallback.trim()}>提交回调 URL</button></div>
            </div>
          )}
          {reloginVerifyUrl && (
            <div className="oauthStep">
              <span className="oauthStepLabel">3. 组织验证链接{reloginUserCode ? `（设备代码 ${reloginUserCode}）` : ''}</span>
              <code className="oauthUrl">{reloginVerifyUrl}</code>
              <div className="oauthActions"><button className="btn" onClick={() => window.open(reloginVerifyUrl, '_blank', 'noopener')}>打开</button><button className="btn" onClick={() => navigator.clipboard?.writeText(reloginVerifyUrl)}>复制</button></div>
            </div>
          )}
        </div>
      )}
    </article>
  );
}

function quotaTriggerConfigShape(schedule) {
  return {
    id: schedule.id,
    provider: normalizeKey(schedule.provider),
    auth_index: schedule.auth_index,
    model: schedule.model,
    time: schedule.time,
    timezone: schedule.timezone,
    enabled: schedule.enabled !== false,
  };
}

function quotaTriggerAccountLabel(file) {
  return file?.email || file?.label || file?.name || file?.auth_index || '未命名凭证';
}

function quotaTriggerTimestamp(value) {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? String(value) : formatDate(value);
}

function quotaTriggerModelValue(model) {
  const raw = typeof model === 'string'
    ? model
    : model?.id ?? model?.model_id ?? model?.modelId ?? model?.name ?? '';
  return String(raw || '')
    .trim()
    .replace(/^(?:nexus|codex|antigravity)\//i, '');
}

function quotaTriggerModelLabel(model, value) {
  if (typeof model === 'string') return value;
  return String(model?.display_name ?? model?.displayName ?? model?.name ?? value).trim() || value;
}

function normalizeQuotaTriggerModels(models) {
  const seen = new Set();
  return (Array.isArray(models) ? models : [])
    .map((model) => {
      const value = quotaTriggerModelValue(model);
      return value ? { value, label: quotaTriggerModelLabel(model, value) } : null;
    })
    .filter((model) => {
      if (!model || seen.has(model.value)) return false;
      seen.add(model.value);
      return true;
    });
}

function QuotaTriggerPanel({ mgmtKey, files, targetAuthIndex, onClose, onChanged }) {
  const candidates = useMemo(
    () => files.filter((file) => ['codex', 'antigravity'].includes(normalizeKey(file.provider || file.type)) && file.auth_index),
    [files]
  );
  const browserTimezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', []);
  const [schedules, setSchedules] = useState([]);
  const [modelsByCredential, setModelsByCredential] = useState({});
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [runningId, setRunningId] = useState('');
  const [editingId, setEditingId] = useState('');
  const [formOpen, setFormOpen] = useState(true);
  const [formError, setFormError] = useState('');
  const [notice, setNotice] = useState('');
  const [form, setForm] = useState({
    id: '',
    provider: 'codex',
    auth_index: '',
    model: '',
    time: '08:30',
    timezone: browserTimezone,
    enabled: true,
  });

  const candidateFor = useCallback(
    (authIndex, allowFallback = true) => candidates.find((file) => file.auth_index === authIndex) || (allowFallback ? candidates[0] : null),
    [candidates]
  );

  const formFor = useCallback(
    (authIndex, current = {}) => {
      const candidate = candidateFor(authIndex);
      const provider = normalizeKey(candidate?.provider || candidate?.type || current.provider || 'codex');
      return {
        id: current.id || '',
        provider,
        auth_index: candidate?.auth_index || current.auth_index || '',
        model: quotaTriggerModelValue(current.model),
        time: current.time || '08:30',
        timezone: current.timezone || browserTimezone,
        enabled: current.enabled !== false,
      };
    },
    [browserTimezone, candidateFor]
  );

  const load = useCallback(async () => {
    setLoading(true);
    setFormError('');
    try {
      const data = await request('/quota-triggers', mgmtKey);
      setSchedules(Array.isArray(data.schedules) ? data.schedules : []);
    } catch (e) {
      setFormError(`计划读取失败：${e.message}`);
    } finally {
      setLoading(false);
    }
  }, [mgmtKey]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    const timer = window.setInterval(load, 30000);
    return () => window.clearInterval(timer);
  }, [load]);

  useEffect(() => {
    if (editingId) return;
    setForm((current) => formFor(targetAuthIndex || current.auth_index));
  }, [editingId, formFor, targetAuthIndex]);

  useEffect(() => {
    if (!targetAuthIndex || !schedules.length || editingId) return;
    const existing = schedules.find((schedule) => schedule.auth_index === targetAuthIndex);
    if (!existing) return;
    setEditingId(existing.id);
    setForm(formFor(existing.auth_index, existing));
    setFormOpen(true);
  }, [editingId, formFor, schedules, targetAuthIndex]);

  const persist = useCallback(
    async (nextSchedules) => {
      setSaving(true);
      setFormError('');
      try {
        await request(`/plugins/${NEXUS_PLUGIN_ID}/config`, mgmtKey, {
          base: MGMT,
          method: 'PATCH',
          body: JSON.stringify({ quota_triggers: nextSchedules.map(quotaTriggerConfigShape) }),
        });
        setSchedules(nextSchedules);
        setNotice('计划已保存，后台调度已更新');
        window.setTimeout(load, 300);
        onChanged?.();
        return true;
      } catch (e) {
        setFormError(`计划保存失败：${e.message}`);
        return false;
      } finally {
        setSaving(false);
      }
    },
    [load, mgmtKey, onChanged]
  );

  const saveForm = useCallback(
    async (event) => {
      event.preventDefault();
      const authIndex = form.auth_index.trim();
      if (!authIndex) {
        setFormError('请选择凭证');
        return;
      }
      const model = quotaTriggerModelValue(form.model);
      if (!model) {
        setFormError(modelsError || '请选择该凭证的可用模型');
        return;
      }
      if (!/^\d{2}:\d{2}$/.test(form.time)) {
        setFormError('时间格式无效');
        return;
      }
      if (!form.timezone.trim()) {
        setFormError('请填写时区');
        return;
      }
      try {
        new Intl.DateTimeFormat('en-US', { timeZone: form.timezone.trim() }).format();
      } catch {
        setFormError('时区无效，请使用例如 Asia/Shanghai 的时区名称');
        return;
      }
      const duplicate = schedules.find((schedule) => schedule.auth_index === authIndex && schedule.id !== editingId);
      if (duplicate) {
        setFormError('这个凭证已经有一个计划，请直接编辑现有计划');
        return;
      }
      const item = {
        ...form,
        id: editingId || `qt-${Date.now().toString(36)}`,
        auth_index: authIndex,
        model,
        timezone: form.timezone.trim(),
        enabled: form.enabled !== false,
      };
      const next = editingId
        ? schedules.map((schedule) => (schedule.id === editingId ? item : schedule))
        : [...schedules, item];
      if (await persist(next)) {
        setEditingId('');
        setFormOpen(false);
      }
    },
    [editingId, form, modelsError, persist, schedules]
  );

  const startNew = useCallback(
    (authIndex = targetAuthIndex) => {
      setEditingId('');
      setForm(formFor(authIndex || candidates[0]?.auth_index));
      setFormError('');
      setNotice('');
      setFormOpen(true);
    },
    [candidates, formFor, targetAuthIndex]
  );

  const editSchedule = useCallback(
    (schedule) => {
      setEditingId(schedule.id);
      setForm(formFor(schedule.auth_index, schedule));
      setFormError('');
      setNotice('');
      setFormOpen(true);
    },
    [formFor]
  );

  const removeSchedule = useCallback(
    async (schedule) => {
      if (!window.confirm(`删除“${quotaTriggerAccountLabel(candidateFor(schedule.auth_index, false))}”的每日触发计划吗？`)) return;
      await persist(schedules.filter((item) => item.id !== schedule.id));
      if (editingId === schedule.id) {
        setEditingId('');
        setFormOpen(false);
      }
    },
    [candidateFor, editingId, persist, schedules]
  );

  const toggleSchedule = useCallback(
    async (schedule) => {
      await persist(schedules.map((item) => (item.id === schedule.id ? { ...item, enabled: item.enabled === false } : item)));
    },
    [persist, schedules]
  );

  const runNow = useCallback(
    async (schedule) => {
      setRunningId(schedule.id);
      setFormError('');
      setNotice('');
      try {
        const result = await request('/quota-triggers/run', mgmtKey, {
          method: 'POST',
          body: JSON.stringify({ id: schedule.id }),
        });
        if (result.status === 'success') setNotice(`${quotaTriggerAccountLabel(candidateFor(schedule.auth_index, false))} 已完成一次唤醒请求`);
        else setFormError(result.message || '唤醒失败');
        await load();
        onChanged?.();
      } catch (e) {
        setFormError(`立即触发失败：${e.message}`);
      } finally {
        setRunningId('');
      }
    },
    [candidateFor, load, mgmtKey, onChanged]
  );

  const selectedCandidate = candidateFor(form.auth_index);
  const selectedCredentialKey = selectedCandidate?.name || selectedCandidate?.auth_index || '';
  const availableModels = selectedCredentialKey ? modelsByCredential[selectedCredentialKey] || [] : [];
  const modelOptions = useMemo(() => {
    const options = [...availableModels];
    const selectedModel = quotaTriggerModelValue(form.model);
    if (selectedModel && !options.some((model) => model.value === selectedModel)) {
      options.unshift({ value: selectedModel, label: `${selectedModel}（当前计划）` });
    }
    return options;
  }, [availableModels, form.model]);

  useEffect(() => {
    const candidate = selectedCandidate;
    const credentialKey = candidate?.name || candidate?.auth_index || '';
    if (!credentialKey || !candidate?.name) {
      setModelsLoading(false);
      setModelsError('');
      return undefined;
    }

    if (Object.prototype.hasOwnProperty.call(modelsByCredential, credentialKey)) {
      const options = modelsByCredential[credentialKey];
      setModelsLoading(false);
      setModelsError('');
      if (options.length) {
        setForm((current) => (
          (!current.auth_index || current.auth_index === candidate.auth_index) && !current.model
            ? { ...current, model: options[0].value }
            : current
        ));
      }
      return undefined;
    }

    let cancelled = false;
    setModelsLoading(true);
    setModelsError('');
    fetchAuthFileModels(candidate.name, mgmtKey)
      .then((models) => {
        if (cancelled) return;
        const options = normalizeQuotaTriggerModels(models);
        setModelsByCredential((current) => ({ ...current, [credentialKey]: options }));
        if (options.length) {
          setForm((current) => (
            (!current.auth_index || current.auth_index === candidate.auth_index) && !current.model
              ? { ...current, model: options[0].value }
              : current
          ));
        }
      })
      .catch((error) => {
        if (!cancelled) setModelsError(`可用模型获取失败：${error.message}`);
      })
      .finally(() => {
        if (!cancelled) setModelsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [mgmtKey, modelsByCredential, selectedCandidate?.auth_index, selectedCandidate?.name]);

  const busy = saving || Boolean(runningId);

  useEffect(() => {
    const handleKeyDown = (event) => {
      if (event.key === 'Escape' && !busy) onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [busy, onClose]);

  return (
    <div
      className="quotaTriggerModalBackdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onClose();
      }}
    >
      <section
        className="panel quotaTriggerPanel quotaTriggerModal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="quota-trigger-title"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="quotaTriggerHead">
          <div>
            <div className="quotaTriggerTitle"><ClockIcon /><h2 id="quota-trigger-title">Quota 唤醒计划</h2></div>
            <span className="muted">按账户和本地时间定时发起一次真实模型请求</span>
          </div>
          <button type="button" className="btn iconButton" onClick={onClose} disabled={busy} title="关闭计划弹窗" aria-label="关闭计划弹窗">×</button>
        </div>

        <div className="triggerNotice">
          <InfoIcon size={14} />
          <span>每次计划会消耗一次真实模型调用，用于启动 Codex 或 Antigravity 的额度窗口；不会因为刷新网页而触发。</span>
        </div>

        {(formError || notice) && <div className={formError ? 'errorBox triggerMessage' : 'triggerSuccess'}>{formError || notice}</div>}

        {formOpen ? (
          <form className="quotaTriggerForm" onSubmit={saveForm}>
            <div className="triggerFormGrid">
              <label className="triggerField">
                <span>指定账户</span>
                <select
                  value={form.auth_index}
                  onChange={(event) => {
                    const candidate = candidateFor(event.target.value);
                    const nextProvider = normalizeKey(candidate?.provider || candidate?.type || 'codex');
                    setForm((current) => ({
                      ...current,
                      provider: nextProvider,
                      auth_index: event.target.value,
                      model: '',
                    }));
                  }}
                  disabled={!candidates.length || saving}
                >
                  <option value="">选择 Codex 或 Antigravity 凭证</option>
                  {candidates.map((file) => <option key={file.auth_index} value={file.auth_index}>{quotaTriggerAccountLabel(file)}</option>)}
                </select>
              </label>
              <label className="triggerField">
                <span>每日触发时间</span>
                <input type="time" value={form.time} onChange={(event) => setForm((current) => ({ ...current, time: event.target.value }))} disabled={saving} />
              </label>
              <label className="triggerField">
                <span>计划时区</span>
                <input type="text" value={form.timezone} onChange={(event) => setForm((current) => ({ ...current, timezone: event.target.value }))} placeholder="例如 Asia/Shanghai" disabled={saving} />
              </label>
              <label className="triggerField">
                <span>唤醒模型</span>
                <select
                  value={form.model}
                  onChange={(event) => setForm((current) => ({ ...current, model: event.target.value }))}
                  disabled={saving || modelsLoading || !selectedCandidate || !modelOptions.length}
                >
                  <option value="">
                    {modelsLoading ? '读取可用模型…' : modelsError ? '可用模型读取失败' : modelOptions.length ? '选择可用模型' : '该凭证暂无可用模型'}
                  </option>
                  {modelOptions.map((model) => (
                    <option key={model.value} value={model.value}>
                      {model.label === model.value ? model.label : `${model.label} · ${model.value}`}
                    </option>
                  ))}
                </select>
                <small>{modelsError || (modelsLoading ? '正在从 CPA 获取可用模型' : modelOptions.length ? `${modelOptions.length} 个可用模型` : '该凭证没有返回可用模型')}</small>
              </label>
            </div>
            <div className="quotaTriggerFormActions">
              <span className="muted">{editingId ? '编辑现有计划' : '新建每日计划'}</span>
              <div>
                {editingId && <button type="button" className="btn" onClick={() => { setEditingId(''); setFormOpen(false); }}>取消编辑</button>}
                <button type="submit" className="btn btnPrimary" disabled={saving || modelsLoading || !candidates.length || !form.model.trim()}>{saving ? '保存中…' : '保存计划'}</button>
              </div>
            </div>
          </form>
        ) : (
          <button type="button" className="btn quotaTriggerNew" onClick={() => startNew()} disabled={!candidates.length}>新建唤醒计划</button>
        )}

        <div className="quotaTriggerList">
          <div className="quotaTriggerListHead"><strong>已配置计划</strong><span className="muted">{loading ? '读取中…' : `${schedules.length} 个`}</span></div>
          {!loading && !schedules.length && <div className="quotaEmpty">还没有计划。比如选择一个账户，设置每天 08:30。</div>}
          {schedules.map((schedule) => {
            const file = candidateFor(schedule.auth_index, false);
            const scheduleProvider = normalizeKey(schedule.provider);
            const isRunning = runningId === schedule.id || schedule.running;
            const status = schedule.last_status === 'success'
              ? `上次成功 ${quotaTriggerTimestamp(schedule.last_run_at)}`
              : schedule.last_status === 'error'
                ? `上次失败：${schedule.last_error || '未知错误'}`
                : schedule.last_status === 'running'
                  ? '正在执行…'
                  : '尚未执行';
            return (
              <div className={`quotaTriggerRow${schedule.enabled === false ? ' quotaTriggerRowDisabled' : ''}`} key={schedule.id}>
                <div className="quotaTriggerRowMain">
                  <div className="quotaTriggerRowTitle">
                    <span className={`triggerProviderTag triggerProvider${scheduleProvider}`}>{typeLabel(scheduleProvider)}</span>
                    <strong title={quotaTriggerAccountLabel(file)}>{quotaTriggerAccountLabel(file)}</strong>
                  </div>
                  <div className="quotaTriggerRowMeta">每天 {schedule.time} · {schedule.timezone} · {schedule.model}</div>
                  <div className={`quotaTriggerRowStatus ${schedule.last_status === 'error' ? 'quotaTriggerStatusError' : schedule.last_status === 'success' ? 'quotaTriggerStatusSuccess' : ''}`} title={schedule.last_error || status}>{status}</div>
                  {schedule.enabled !== false && schedule.next_run_at && <div className="quotaTriggerRowNext">下次：{quotaTriggerTimestamp(schedule.next_run_at)}</div>}
                </div>
                <div className="quotaTriggerRowActions">
                  <button type="button" className={`toggle ${schedule.enabled !== false ? 'checked' : ''}`} role="switch" aria-checked={schedule.enabled !== false} onClick={() => toggleSchedule(schedule)} disabled={saving || isRunning} title={schedule.enabled === false ? '启用计划' : '停用计划'}><span className="toggleThumb" /></button>
                  <button type="button" className="btn iconButton" onClick={() => runNow(schedule)} disabled={saving || isRunning} title="立即触发" aria-label="立即触发">{isRunning ? <RefreshIcon /> : <PlayIcon />}</button>
                  <button type="button" className="btn iconButton" onClick={() => editSchedule(schedule)} disabled={saving || isRunning} title="编辑计划" aria-label="编辑计划">✎</button>
                  <button type="button" className="btn iconButton dangerButton" onClick={() => removeSchedule(schedule)} disabled={saving || isRunning} title="删除计划" aria-label="删除计划"><TrashIcon /></button>
                </div>
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}

const OAUTH_PROVIDERS = {
  kiro: {
    label: 'Kiro',
    title: 'Kiro OAuth',
    description: '通过浏览器授权新增一个凭证',
    icon: ICONS.kiro,
    className: 'oauthProviderKiro',
  },
  cline: {
    label: 'Cline',
    title: 'Cline OAuth',
    description: '通过浏览器授权新增一个凭证',
    icon: clineIcon,
    className: 'oauthProviderCline',
  },
};

function OAuthBrandIcon({ provider, size = 22 }) {
  const [failed, setFailed] = useState(false);
  const item = OAUTH_PROVIDERS[provider];
  const useCardStyle = provider === 'cline';
  const color = typeColor(provider);
  const cardStyle = useCardStyle
    ? { backgroundColor: color.bg, color: color.text, ...(color.border ? { border: color.border } : {}) }
    : undefined;
  return (
    <span className={`oauthBrandIcon ${item.className} ${useCardStyle ? 'oauthBrandIconCard' : ''}`} style={cardStyle}>
      {failed ? (
        <span className="oauthBrandFallback">{item.label.slice(0, 1)}</span>
      ) : (
        <img src={item.icon} alt="" width={size} height={size} onError={() => setFailed(true)} />
      )}
    </span>
  );
}

function OAuthEntry({ onOpen }) {
  return (
    <button type="button" className="panel oauthEntry" onClick={onOpen}>
      <span className="oauthEntryBrand"><OAuthLoginIcon /></span>
      <span className="oauthEntryCopy">
        <strong>OAuth 登录</strong>
        <span className="muted">通过浏览器授权新增一个凭证</span>
      </span>
      <span className="oauthEntryArrow" aria-hidden="true">→</span>
    </button>
  );
}

function OAuthProviderPage({ mgmtKey, onChanged, onBack }) {
  const [provider, setProvider] = useState('');
  return (
    <section className="oauthPage">
      <div className="panel oauthPageHead">
        <button type="button" className="btn iconButton" onClick={provider ? () => setProvider('') : onBack} title="返回">
          <BackIcon />
        </button>
        <div>
          <h2>OAuth 登录</h2>
          <span className="muted">选择要添加的凭证类型</span>
        </div>
      </div>

      {provider ? (
        <OAuthPanel provider={provider} mgmtKey={mgmtKey} onChanged={onChanged} onBack={() => setProvider('')} />
      ) : (
        <div className="oauthProviderGrid">
          {Object.entries(OAUTH_PROVIDERS).map(([key, item]) => (
            <button type="button" className={`oauthProviderCard ${item.className}`} key={key} onClick={() => setProvider(key)}>
              <span className="oauthProviderIcon"><OAuthBrandIcon provider={key} /></span>
              <span className="oauthProviderCopy">
                <strong>{item.title}</strong>
                <span>{item.description}</span>
              </span>
              <span className="oauthProviderArrow" aria-hidden="true">→</span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function OAuthPanel({ provider, mgmtKey, onChanged, onBack }) {
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);
  const [signinUrl, setSigninUrl] = useState('');
  const [state, setState] = useState('');
  const [verifyUrl, setVerifyUrl] = useState('');
  const [userCode, setUserCode] = useState('');
  const [callback, setCallback] = useState('');

  const reset = () => {
    setSigninUrl('');
    setState('');
    setVerifyUrl('');
    setUserCode('');
    setCallback('');
  };

  // Poll the plugin login session until the credential lands or fails.
  const poll = useCallback(
    async (st) => {
      try {
        const s = await request(`/console/oauth/status?state=${encodeURIComponent(st)}`, mgmtKey);
        if (s.status === 'success') {
          setStatus('登录成功，已保存新凭证');
          setBusy(false);
          reset();
          onChanged();
        } else if (s.status === 'error') {
          setStatus(`失败：${s.message || '未知错误'}`);
          setBusy(false);
        } else {
          setTimeout(() => poll(st), 2500);
        }
      } catch (e) {
        setStatus(`失败：${e.message}`);
        setBusy(false);
      }
    },
    [mgmtKey, onChanged]
  );

  const providerInfo = OAUTH_PROVIDERS[provider];
  const isCline = provider === 'cline';

  // Step 1: ask the plugin for the provider sign-in URL + state.
  const startLogin = useCallback(async () => {
    setBusy(true);
    setStatus('启动中…');
    reset();
    try {
      const started = await request('/console/oauth/start', mgmtKey, {
        method: 'POST',
        body: JSON.stringify(isCline ? { login_mode: 'cline' } : {}),
      });
      if (!started.url || !started.state) throw Error('登录启动失败');
      const cleanURL = cleanOAuthURL(started.url);
      setSigninUrl(cleanURL);
      setState(started.state);
      const browserFlow = isCline || /app\.kiro\.dev\/signin/i.test(cleanURL);
      setStatus(
        browserFlow
          ? `打开 ${providerInfo.label} 登录链接，在浏览器完成登录后把跳转到的 localhost 回调 URL 粘贴到下方。`
          : '打开设备验证链接并完成授权，面板会自动等待登录完成。'
      );
      window.open(cleanURL, '_blank', 'noopener');
      // aws-device returns the AWS verification URL directly and never emits
      // a browser callback; poll immediately after the user approves it.
      if (!browserFlow) poll(started.state);
    } catch (e) {
      setStatus(`失败：${e.message}`);
      setBusy(false);
    }
  }, [isCline, mgmtKey, poll, providerInfo.label]);

  // Step 2: submit the pasted localhost callback URL. Org logins return a
  // device verification URL to open; personal logins carry the code directly.
  const submitCallback = useCallback(async () => {
    const redirect = callback.trim();
    if (!redirect) return;
    setStatus('提交回调 URL…');
    try {
      const r = await request(
        isCline ? `/console/oauth/status?state=${encodeURIComponent(state)}` : '/oauth/callback',
        mgmtKey,
        {
          method: 'POST',
          body: JSON.stringify(isCline ? { callback_url: redirect } : { redirect_url: redirect }),
        }
      );
      if (r.status === 'success') {
        setStatus('登录成功，已保存新凭证');
        setBusy(false);
        reset();
        onChanged();
      } else if (r.status === 'error') {
        setStatus(`失败：${r.message || '未知错误'}`);
        setBusy(false);
      } else if (r.status === 'continue' && r.url) {
        setVerifyUrl(r.url);
        setUserCode(r.user_code || '');
        setStatus('组织登录：打开下方验证链接并确认设备代码，然后等待完成。');
        window.open(r.url, '_blank', 'noopener');
        poll(r.state || state);
      } else if (r.status === 'accepted') {
        setStatus('回调已接收，等待完成…');
        poll(r.state || state);
      } else {
        setStatus(`意外响应：${r.status || '未知'}`);
      }
    } catch (e) {
      setStatus(`回调提交失败：${e.message}`);
    }
  }, [callback, isCline, mgmtKey, poll, state]);

  const copy = (text) => navigator.clipboard?.writeText(text);
  const browserFlow = isCline || /app\.kiro\.dev\/signin/i.test(signinUrl);

  return (
    <section className="panel oauthPanel">
      <div className="oauthFlowHead">
        <button type="button" className="btn iconButton" onClick={onBack} title="返回登录方式">
          <BackIcon />
        </button>
        <div>
          <div className="oauthProviderTitle"><OAuthBrandIcon provider={provider} size={18} /><h3>{providerInfo.title}</h3></div>
          <span className="muted">通过浏览器授权新增一个凭证</span>
        </div>
        <button className="btn btnPrimary" onClick={startLogin} disabled={busy && !signinUrl}>
          {busy && !signinUrl ? '进行中…' : signinUrl ? '重新开始' : '开始登录'}
        </button>
      </div>

      {signinUrl && (
        <div className="oauthFlow">
          <div className="oauthStep">
            <span className="oauthStepLabel">{browserFlow ? '1. 登录链接' : '设备验证链接'}</span>
            <code className="oauthUrl">{signinUrl}</code>
            <div className="oauthActions">
              <button className="btn" onClick={() => window.open(signinUrl, '_blank', 'noopener')}>打开</button>
              <button className="btn" onClick={() => copy(signinUrl)}>复制</button>
            </div>
          </div>

          {browserFlow && (
            <div className="oauthStep">
              <span className="oauthStepLabel">2. 粘贴回调 URL</span>
              <textarea
                className="oauthInput"
                rows={2}
                value={callback}
                onChange={(e) => setCallback(e.target.value)}
                placeholder={isCline ? 'http://localhost:3128/auth?...' : 'http://localhost:3128/signin/callback?...'}
              />
              <div className="oauthActions">
                <button className="btn btnPrimary" onClick={submitCallback} disabled={!callback.trim()}>提交回调 URL</button>
              </div>
            </div>
          )}

          {verifyUrl && (
            <div className="oauthStep">
              <span className="oauthStepLabel">3. 组织验证链接{userCode ? `（设备代码 ${userCode}）` : ''}</span>
              <code className="oauthUrl">{verifyUrl}</code>
              <div className="oauthActions">
                <button className="btn" onClick={() => window.open(verifyUrl, '_blank', 'noopener')}>打开</button>
                <button className="btn" onClick={() => copy(verifyUrl)}>复制</button>
              </div>
            </div>
          )}
        </div>
      )}

      {status && <span className="muted oauthStatus">{status}</span>}
    </section>
  );
}

function App() {
  const [key, setKey] = useState(() => sessionStorage.getItem('nexus-management-key') || '');
  const [draft, setDraft] = useState(key);
  const [files, setFiles] = useState([]);
  const [filter, setFilter] = useState('all');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshAllTick, setRefreshAllTick] = useState(0);
  const [refreshingAll, setRefreshingAll] = useState(false);
  const [oauthPageOpen, setOauthPageOpen] = useState(false);
  const [quotaTriggerOpen, setQuotaTriggerOpen] = useState(false);
  const [quotaTriggerTarget, setQuotaTriggerTarget] = useState('');

  useEffect(() => {
    syncCPATheme();
    const mediaQuery = window.matchMedia?.('(prefers-color-scheme: dark)');
    const mediaListener = () => {
      if (window.parent === window) syncCPATheme();
    };
    mediaQuery?.addEventListener('change', mediaListener);

    let observer;
    try {
      if (window.parent !== window) {
        observer = new MutationObserver(syncCPATheme);
        observer.observe(window.parent.document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
      }
    } catch {
      // Cross-origin embedding: observer is unavailable.
    }

    return () => {
      mediaQuery?.removeEventListener('change', mediaListener);
      observer?.disconnect();
    };
  }, []);

  const refreshAllQuotas = useCallback(() => {
    setRefreshAllTick((n) => n + 1);
    setRefreshingAll(true);
    setTimeout(() => setRefreshingAll(false), 900);
  }, []);

  const openQuotaTrigger = useCallback((authIndex = '') => {
    setQuotaTriggerTarget(authIndex);
    setQuotaTriggerOpen(true);
  }, []);

  const load = useCallback(async () => {
    if (!key) return;
    setLoading(true);
    setError('');
    try {
      // Prefer CPA native auth-files: it carries success/failed and quota
      // signals for every provider. Fall back to the plugin's Kiro-only
      // /credentials if the native endpoint is unavailable.
      let list = [];
      try {
        const af = await request('/auth-files', key, { base: MGMT });
        list = normalizeAuthFiles(af.files || []);
      } catch (native) {
        const data = await request('/credentials', key);
        list = data.credentials || [];
      }
      setFiles(list);
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [key]);

  useEffect(() => {
    load();
  }, [load]);

  const visible = useMemo(
    () =>
      files.filter((f) =>
        filter === 'all'
          ? true
          : filter === 'disabled'
            ? f.disabled
            : normalizeKey(f.provider || f.type) === filter
      ),
    [files, filter]
  );
  const enabled = useMemo(() => files.filter((f) => !f.disabled).length, [files]);
  // Hide the key box once the key works: set and last load had no auth error.
  const keyValid = Boolean(key) && !error;

  return (
    <div className="page">
      <div className="eyebrow">CLI Proxy API · Plugin Resource</div>
      <div className="pageTitleRow">
        <img src={nexusIcon} alt="" className="pageTitleIcon" />
        <h1 className="pageTitle">Nexus Console</h1>
      </div>

      {!keyValid && (
        <form
          className="panel keyPanel"
          onSubmit={(e) => {
            e.preventDefault();
            sessionStorage.setItem('nexus-management-key', draft.trim());
            setKey(draft.trim());
          }}
        >
          <label htmlFor="mkey">Management Key</label>
          <input
            id="mkey"
            type="password"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="输入 CPA Management Key"
          />
          <button type="submit" className="btn btnPrimary">
            保存
          </button>
        </form>
      )}

      {key && oauthPageOpen ? (
        <OAuthProviderPage mgmtKey={key} onChanged={load} onBack={() => setOauthPageOpen(false)} />
      ) : (
        <>
          {key && <OAuthEntry onOpen={() => setOauthPageOpen(true)} />}

      <section className="panel toolbar">
        <div>
          <h2>认证文件</h2>
          <span className="muted">{files.length} 个凭证 · {enabled} 个启用</span>
        </div>
        <div className="toolbarActions">
          <button className="btn" onClick={() => openQuotaTrigger()} disabled={!key} title="设置每日 Quota 唤醒计划">
            <ClockIcon />
            <span>Quota 计划</span>
          </button>
          <button className="btn btnPrimary" onClick={load} disabled={loading} title="重新拉取凭证列表">
            {loading ? '刷新中…' : '刷新列表'}
          </button>
        </div>
      </section>

      {key && quotaTriggerOpen && (
        <QuotaTriggerPanel
          mgmtKey={key}
          files={files}
          targetAuthIndex={quotaTriggerTarget}
          onClose={() => setQuotaTriggerOpen(false)}
          onChanged={load}
        />
      )}

      <div className="tabs">
        <div className="tabList">
          {[
            ['all', '全部'],
            ['kiro', 'Kiro'],
            ['codex', 'Codex'],
            ['antigravity', 'Antigravity'],
            ['disabled', '已停用'],
          ].map(([value, text]) => (
            <button
              key={value}
              className={`tab ${filter === value ? 'active' : ''}`}
              onClick={() => setFilter(value)}
            >
              {text}
            </button>
          ))}
        </div>
        <button
          className="refreshAllBtn"
          onClick={refreshAllQuotas}
          disabled={!key || refreshingAll}
          title="刷新所有凭证的额度"
        >
          <svg
            className={refreshingAll ? 'spinning' : undefined}
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
            <path d="M21 3v5h-5" />
            <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
            <path d="M3 21v-5h5" />
          </svg>
          刷新全部凭证
        </button>
      </div>

      {error && <div className="errorBox">{error}</div>}

      <section className="grid">
        {visible.map((f) => (
          <Card key={`${f.provider}:${f.auth_index || f.name}`} file={f} mgmtKey={key} onChanged={load} refreshAll={refreshAllTick} onOpenQuotaTrigger={openQuotaTrigger} />
        ))}
        {!loading && !visible.length && (
          <div className="empty">{key ? '没有匹配的认证文件' : '请输入 Management Key，然后点击保存'}</div>
        )}
      </section>
        </>
      )}
    </div>
  );
}

createRoot(document.getElementById('root')).render(<App />);
