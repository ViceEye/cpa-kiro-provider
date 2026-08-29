package provider

import (
	"sort"
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

type statEvent struct {
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
}

type credentialStat struct {
	Success     int64       `json:"success"`
	Failure     int64       `json:"failure"`
	LastRequest time.Time   `json:"last_request"`
	History     []statEvent `json:"history"`
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

type statSnapshot struct {
	Success     int64
	Failure     int64
	LastRequest string
	History     []bool
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
	return statSnapshot{Success: stat.Success, Failure: stat.Failure, LastRequest: last, History: history}, true
}
