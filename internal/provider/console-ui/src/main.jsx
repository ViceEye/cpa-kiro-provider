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
import qwenIcon from './assets/qwen.svg';
import vertexIcon from './assets/vertex.svg';

const API = '/v0/management/plugins/kiro-provider';
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
  return { statusCode, body };
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

function fmtNum(n) {
  const v = Number(n);
  if (!Number.isFinite(v)) return '0';
  return v % 1 === 0 ? v.toLocaleString('en-US') : v.toFixed(2);
}

// Map CPA recent_requests buckets onto right-aligned discrete cells, 1:1 with
// the official panel: each bucket is one solid cell — success-only=green,
// failure-only=red, mixed=amber, empty=idle. Left-padded with idle cells.
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
    return { state, success: b.success, failure: b.failed, startTime, endTime: startTime + STATUS_CELL_MS };
  });
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
            <div className={`statusBlock statusBlock-${c.state}`} />
            {active === i && (
              <div className={`statusTooltip${posClass(i)}`}>
                <span className="tooltipTime">{clock(c.startTime)} – {clock(c.endTime)}</span>
                {c.success + c.failure > 0 ? (
                  <span className="tooltipStats">
                    <span className="tooltipSuccess">成功 {c.success}</span>
                    <span className="tooltipFailure">失败 {c.failure}</span>
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
      label: f.label || f.account || f.email || rawId,
      email: f.email || f.account || '',
      disabled: !!f.disabled,
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

function Card({ file, mgmtKey, onChanged, refreshAll }) {
  const providerKey = normalizeKey(file.provider || file.type || 'kiro');
  const color = typeColor(providerKey);
  const label = typeLabel(providerKey);
  const icon = iconFor(providerKey);
  const rawAccount = file.email || file.label || file.name || 'Kiro account';
  const account = providerKey === 'cline' && normalizeKey(rawAccount) === 'kiro' ? 'Cline' : rawAccount;
  const fileName = file.name || '';
  const disabled = !!file.disabled;
  const isKiro = providerKey === 'kiro';
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
  const [selected, setSelected] = useState(false);
  const [models, setModels] = useState(null);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [statusUpdating, setStatusUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [actionError, setActionError] = useState('');

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

  const refreshQuota = useCallback(async () => {
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
              : '该凭证由管理中心管理，Kiro 插件不查询其额度',
          }
      );
    } catch (e) {
      setQuota({ error: e.message });
    } finally {
      setQuotaLoading(false);
    }
  }, [mgmtKey, file.auth_index, file.name, isKiro]);

  const showModels = useCallback(async () => {
    setModelsLoading(true);
    setActionError('');
    try {
      const data = await request(`/auth-files/models?name=${encodeURIComponent(file.name)}`, mgmtKey, { base: MGMT });
      setModels(Array.isArray(data.models) ? data.models : []);
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
  // card's quota at once (Kiro -> /quotaRequest, Codex -> live usage).
  useEffect(() => {
    if (!refreshAll) return;
    if (isKiro && file.auth_index) refreshQuota();
    else if (isCodex && file.auth_index) refreshCodex();
  }, [refreshAll]);

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
            <span className={`stateBadge ${disabled ? 'stateDisabled' : 'stateActive'}`}>
              <span className="stateDot" aria-hidden="true" />
              {disabled ? '停用' : '启用'}
            </span>
          </div>
          <span className="account" title={account}>{account}</span>
        </div>
      </header>

      {fileName && <p className="fileName" title={fileName}>{fileName}</p>}

      {file.duplicate_count > 1 && (
        <p className="dupNote">⚠ 该凭证在 CPA 中注册了 {file.duplicate_count} 次（同一文件），已合并显示。建议在认证文件里删除多余条目。</p>
      )}

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
          <button
            className="btn iconButton"
            onClick={isKiro ? refreshQuota : isCodex ? refreshCodex : onChanged}
            title="刷新额度"
            disabled={quotaLoading || codexLoading}
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
  return (
    <span className={`oauthBrandIcon ${item.className}`}>
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
      <span className="oauthEntryBrand"><OAuthBrandIcon provider="kiro" size={20} /></span>
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
  const [key, setKey] = useState(() => sessionStorage.getItem('kiro-management-key') || '');
  const [draft, setDraft] = useState(key);
  const [files, setFiles] = useState([]);
  const [filter, setFilter] = useState('all');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshAllTick, setRefreshAllTick] = useState(0);
  const [refreshingAll, setRefreshingAll] = useState(false);
  const [oauthPageOpen, setOauthPageOpen] = useState(false);

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
      <h1 className="pageTitle">Kiro Console</h1>

      {!keyValid && (
        <section className="panel keyPanel">
          <label htmlFor="mkey">Management Key</label>
          <input
            id="mkey"
            type="password"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="输入 CPA Management Key"
          />
          <button
            className="btn btnPrimary"
            onClick={() => {
              sessionStorage.setItem('kiro-management-key', draft.trim());
              setKey(draft.trim());
            }}
          >
            保存
          </button>
        </section>
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
        <button className="btn btnPrimary" onClick={load} disabled={loading} title="重新拉取凭证列表">
          {loading ? '刷新中…' : '刷新列表'}
        </button>
      </section>

      <div className="tabs">
        <div className="tabList">
          {[
            ['all', '全部'],
            ['kiro', 'Kiro'],
            ['codex', 'Codex'],
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
          <Card key={`${f.provider}:${f.auth_index || f.name}`} file={f} mgmtKey={key} onChanged={load} refreshAll={refreshAllTick} />
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
