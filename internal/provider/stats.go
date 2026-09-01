package provider

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rolling per-credential request statistics kept in memory. CPA gives the
// plugin no persistence RPC, and the management-center health bar is itself a
// recent-window view, so in-memory counters that reset on restart are the
// right tradeoff here.
// ponytail: in-memory only, add host-backed persistence if counts must survive restarts.

const statsHistoryLimit = 40

var (
	clineRetryAfterPattern = regexp.MustCompile(`(?i)try again in[[:space:]]+([0-9]+[[:space:]]*[dhms]([[:space:]]*[0-9]+[[:space:]]*[dhms])*)`)
	clineDurationPartPattern = regexp.MustCompile(`([0-9]+)[[:space:]]*([dhms])`)
)

type statEvent struct {
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
}

type credentialStat struct {
	Success     int64       `json:"success"`
	Failure     int64       `json:"failure"`
	LastRequest time.Time   `json:"last_request"`
	History     []statEvent `json:"history"`
	ClineModels map[string]clineModelState
}

type clineModelState struct {
	Status      string
	ResetAt     time.Time
	LastChecked time.Time
	Message     string
}

type clineModelSnapshot struct {
	Status      string `json:"status"`
	ResetAt     string `json:"reset_at,omitempty"`
	LastChecked string `json:"last_checked,omitempty"`
	Message     string `json:"message,omitempty"`
}

var requestStats = struct {
	sync.Mutex
	byAuthID map[string]*credentialStat
}{byAuthID: make(map[string]*credentialStat)}

// recordRequest tallies one completed chat request against a credential.
func recordRequest(authID string, success bool) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}
	now := time.Now().UTC()
	requestStats.Lock()
	defer requestStats.Unlock()
	stat := requestStats.byAuthID[authID]
	if stat == nil {
		stat = &credentialStat{}
		requestStats.byAuthID[authID] = stat
	}
	if success {
		stat.Success++
	} else {
		stat.Failure++
	}
	stat.LastRequest = now
	stat.History = append(stat.History, statEvent{At: now, Success: success})
	if len(stat.History) > statsHistoryLimit {
		stat.History = stat.History[len(stat.History)-statsHistoryLimit:]
	}
}

// recordClineRequest records the outcome of a real Cline model request. It
// deliberately ignores balance/management calls: those endpoints do not tell
// us whether a free inference window is available.
func recordClineRequest(authID, model string, success bool, message string) {
	recordRequest(authID, success)
	authID = strings.TrimSpace(authID)
	model = normalizeClineModel(model)
	if authID == "" || model == "" {
		return
	}

	now := time.Now().UTC()
	requestStats.Lock()
	defer requestStats.Unlock()
	stat := requestStats.byAuthID[authID]
	if stat == nil {
		stat = &credentialStat{}
		requestStats.byAuthID[authID] = stat
	}
	if stat.ClineModels == nil {
		stat.ClineModels = make(map[string]clineModelState)
	}
	state := stat.ClineModels[model]
	if success {
		state.Status = "available"
		state.ResetAt = time.Time{}
		state.LastChecked = now
		state.Message = ""
		stat.ClineModels[model] = state
		return
	}
	if !isClineInferenceLimit(message) {
		return
	}

	state.Status = "limited"
	state.LastChecked = now
	state.Message = "已达到免费推理上限"
	if delay, ok := parseClineRetryAfter(message); ok && delay > 0 {
		state.ResetAt = now.Add(delay)
	} else {
		state.ResetAt = time.Time{}
	}
	stat.ClineModels[model] = state
}

func normalizeClineModel(model string) string {
	model = strings.TrimSpace(model)
	model = strings.TrimPrefix(model, "nexus/")
	model = strings.TrimPrefix(model, "cline/")
	return model
}

func isClineInferenceLimit(message string) bool {
	message = strings.ToUpper(message)
	return strings.Contains(message, "INFERENCE_CAP_ERROR") || strings.Contains(message, "DAILY FREE LIMIT")
}

func parseClineRetryAfter(message string) (time.Duration, bool) {
	match := clineRetryAfterPattern.FindStringSubmatch(message)
	if len(match) < 2 {
		return 0, false
	}
	var total time.Duration
	for _, part := range clineDurationPartPattern.FindAllStringSubmatch(match[1], -1) {
		if len(part) < 3 {
			continue
		}
		value, err := strconv.ParseInt(part[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(part[2]) {
		case "d":
			total += time.Duration(value) * 24 * time.Hour
		case "h":
			total += time.Duration(value) * time.Hour
		case "m":
			total += time.Duration(value) * time.Minute
		case "s":
			total += time.Duration(value) * time.Second
		}
	}
	return total, true
}

type statSnapshot struct {
	Success     int64
	Failure     int64
	LastRequest string
	History     []bool
	ClineModels map[string]clineModelSnapshot
}

// statFor returns a copy of the stats for a credential, if any.
func statFor(authID string) (statSnapshot, bool) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return statSnapshot{}, false
	}
	requestStats.Lock()
	defer requestStats.Unlock()
	stat := requestStats.byAuthID[authID]
	if stat == nil {
		return statSnapshot{}, false
	}
	history := make([]bool, len(stat.History))
	events := append([]statEvent(nil), stat.History...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	for i, ev := range events {
		history[i] = ev.Success
	}
	last := ""
	if !stat.LastRequest.IsZero() {
		last = stat.LastRequest.Format(time.RFC3339)
	}
	models := make(map[string]clineModelSnapshot, len(stat.ClineModels))
	for model, state := range stat.ClineModels {
		snapshot := clineModelSnapshot{Status: state.Status, Message: state.Message}
		if !state.ResetAt.IsZero() {
			snapshot.ResetAt = state.ResetAt.Format(time.RFC3339)
		}
		if !state.LastChecked.IsZero() {
			snapshot.LastChecked = state.LastChecked.Format(time.RFC3339)
		}
		models[model] = snapshot
	}
	return statSnapshot{Success: stat.Success, Failure: stat.Failure, LastRequest: last, History: history, ClineModels: models}, true
}
