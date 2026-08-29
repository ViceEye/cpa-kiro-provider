package provider

import "testing"

func TestRecordRequestTalliesAndSnapshots(t *testing.T) {
	requestStats.Lock()
	requestStats.byAuthID = make(map[string]*credentialStat)
	requestStats.Unlock()

	const id = "kiro-0123456789abcdef0123"
	recordRequest(id, true)
	recordRequest(id, true)
	recordRequest(id, false)
	recordRequest("", true) // ignored

	snap, ok := statFor(id)
	if !ok {
		t.Fatal("expected stats for credential")
	}
	if snap.Success != 2 || snap.Failure != 1 {
		t.Fatalf("counts: success=%d failure=%d", snap.Success, snap.Failure)
	}
	if len(snap.History) != 3 || snap.History[0] != true || snap.History[2] != false {
		t.Fatalf("history order wrong: %v", snap.History)
	}
	if snap.LastRequest == "" {
		t.Fatal("expected last_request timestamp")
	}
	if _, ok := statFor("kiro-none"); ok {
		t.Fatal("unknown credential should have no stats")
	}
}

func TestRecordRequestHistoryCap(t *testing.T) {
	requestStats.Lock()
	requestStats.byAuthID = make(map[string]*credentialStat)
	requestStats.Unlock()

	const id = "kiro-capfedcba98765432100f"
	for i := 0; i < statsHistoryLimit+10; i++ {
		recordRequest(id, true)
	}
	snap, _ := statFor(id)
	if len(snap.History) != statsHistoryLimit {
		t.Fatalf("history not capped: %d", len(snap.History))
	}
	if snap.Success != int64(statsHistoryLimit+10) {
		t.Fatalf("total success should keep counting: %d", snap.Success)
	}
}
