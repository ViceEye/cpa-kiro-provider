package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/ViceEye/cpa-provider-nexus/internal/quotaactivation"
)

const (
	quotaTriggerTickInterval = 15 * time.Second
	quotaTriggerPrompt       = "quota activation ping"
)

var quotaTriggerProviders = map[string]struct{}{
	"codex":       {},
	"antigravity": {},
}

// quotaTriggerSchedule is persisted below plugins.configs.cpa-provider-nexus.
// Runtime fields such as last_run_at intentionally stay out of this structure.
type quotaTriggerSchedule struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	AuthIndex string `json:"auth_index"`
	Model     string `json:"model"`
	Time      string `json:"time"`
	Timezone  string `json:"timezone"`
	Enabled   bool   `json:"enabled"`
}

type quotaTriggerRuntime struct {
	LastScheduledDate string
	LastRunAt         time.Time
	LastStatus        string
	LastError         string
	Running           bool
}

type quotaTriggerView struct {
	quotaTriggerSchedule
	NextRunAt  string `json:"next_run_at,omitempty"`
	LastRunAt  string `json:"last_run_at,omitempty"`
	LastStatus string `json:"last_status,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	Running    bool   `json:"running"`
}

var quotaTriggerManager = struct {
	sync.Mutex
	started   bool
	stop      chan struct{}
	done      chan struct{}
	schedules map[string]quotaTriggerSchedule
	runtime   map[string]*quotaTriggerRuntime
}{
	schedules: make(map[string]quotaTriggerSchedule),
	runtime:   make(map[string]*quotaTriggerRuntime),
}

func init() {
	startQuotaTriggerScheduler()
}

func startQuotaTriggerScheduler() {
	quotaTriggerManager.Lock()
	if quotaTriggerManager.started {
		quotaTriggerManager.Unlock()
		return
	}
	quotaTriggerManager.started = true
	stop := make(chan struct{})
	done := make(chan struct{})
	quotaTriggerManager.stop = stop
	quotaTriggerManager.done = done
	quotaTriggerManager.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(quotaTriggerTickInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				triggerDueQuotaSchedules(now)
			case <-stop:
				return
			}
		}
	}()
}

// Shutdown stops the background quota trigger loop before the host unloads the
// plugin instance. It is safe to call more than once.
func Shutdown() {
	quotaTriggerManager.Lock()
	stop := quotaTriggerManager.stop
	done := quotaTriggerManager.done
	quotaTriggerManager.stop = nil
	quotaTriggerManager.done = nil
	quotaTriggerManager.started = false
	quotaTriggerManager.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		log.Printf("nexus quota trigger: scheduler shutdown timed out")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		quotaTriggerManager.Lock()
		running := false
		for _, runtime := range quotaTriggerManager.runtime {
			if runtime != nil && runtime.Running {
				running = true
				break
			}
		}
		quotaTriggerManager.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func normalizeQuotaTriggerProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "google" || value == "gemini" || value == "antigravity" {
		return "antigravity"
	}
	return value
}

func normalizeQuotaTriggerModel(value string) string {
	model := strings.TrimSpace(value)
	model = strings.TrimPrefix(model, "nexus/")
	model = strings.TrimPrefix(model, "codex/")
	return strings.TrimPrefix(model, "antigravity/")
}

func normalizeQuotaTriggerSchedule(schedule quotaTriggerSchedule) (quotaTriggerSchedule, error) {
	schedule.Provider = normalizeQuotaTriggerProvider(schedule.Provider)
	if _, ok := quotaTriggerProviders[schedule.Provider]; !ok {
		return quotaTriggerSchedule{}, fmt.Errorf("不支持的提供方")
	}
	schedule.AuthIndex = strings.TrimSpace(schedule.AuthIndex)
	if schedule.AuthIndex == "" {
		return quotaTriggerSchedule{}, fmt.Errorf("缺少凭证 auth_index")
	}
	schedule.Time = strings.TrimSpace(schedule.Time)
	if _, err := time.Parse("15:04", schedule.Time); err != nil {
		return quotaTriggerSchedule{}, fmt.Errorf("时间必须是 HH:MM")
	}
	schedule.Timezone = strings.TrimSpace(schedule.Timezone)
	if schedule.Timezone == "" {
		schedule.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return quotaTriggerSchedule{}, fmt.Errorf("无效的时区")
	}
	schedule.Model = normalizeQuotaTriggerModel(schedule.Model)
	if schedule.Model == "" {
		return quotaTriggerSchedule{}, fmt.Errorf("缺少模型名称")
	}
	schedule.ID = strings.TrimSpace(schedule.ID)
	if schedule.ID == "" {
		schedule.ID = quotaTriggerID(schedule)
	}
	if len(schedule.ID) > 128 {
		schedule.ID = schedule.ID[:128]
	}
	return schedule, nil
}

func normalizeQuotaTriggerSchedules(input []quotaTriggerSchedule) []quotaTriggerSchedule {
	seen := make(map[string]struct{}, len(input))
	result := make([]quotaTriggerSchedule, 0, len(input))
	for _, raw := range input {
		schedule, errNormalize := normalizeQuotaTriggerSchedule(raw)
		if errNormalize != nil {
			log.Printf("nexus quota trigger: ignore invalid schedule: %v", errNormalize)
			continue
		}
		if _, exists := seen[schedule.ID]; exists {
			continue
		}
		seen[schedule.ID] = struct{}{}
		result = append(result, schedule)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func quotaTriggerID(schedule quotaTriggerSchedule) string {
	seed := strings.Join([]string{schedule.Provider, schedule.AuthIndex, schedule.Time, schedule.Timezone, schedule.Model}, "\x00")
	sum := sha256.Sum256([]byte(seed))
	return "qt-" + hex.EncodeToString(sum[:8])
}

// configureQuotaTriggerSchedules applies the persisted config without resetting
// runtime state for unchanged schedules. A newly loaded schedule that has
// already missed today's time waits until tomorrow instead of firing at startup.
func configureQuotaTriggerSchedules(input []quotaTriggerSchedule) {
	schedules := normalizeQuotaTriggerSchedules(input)
	now := time.Now()
	quotaTriggerManager.Lock()
	defer quotaTriggerManager.Unlock()

	nextSchedules := make(map[string]quotaTriggerSchedule, len(schedules))
	nextRuntime := make(map[string]*quotaTriggerRuntime, len(schedules))
	for _, schedule := range schedules {
		nextSchedules[schedule.ID] = schedule
		runtime := quotaTriggerManager.runtime[schedule.ID]
		if runtime == nil {
			runtime = &quotaTriggerRuntime{}
			if dueAt, dateKey, ok := quotaTriggerDueAt(schedule, now); ok && !now.Before(dueAt) {
				runtime.LastScheduledDate = dateKey
			}
		}
		nextRuntime[schedule.ID] = runtime
	}
	quotaTriggerManager.schedules = nextSchedules
	quotaTriggerManager.runtime = nextRuntime
}

func quotaTriggerDueAt(schedule quotaTriggerSchedule, now time.Time) (time.Time, string, bool) {
	location, errLocation := time.LoadLocation(schedule.Timezone)
	if errLocation != nil {
		return time.Time{}, "", false
	}
	parsed, errParse := time.Parse("15:04", schedule.Time)
	if errParse != nil {
		return time.Time{}, "", false
	}
	localNow := now.In(location)
	due := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	return due, due.Format("2006-01-02"), true
}

func quotaTriggerNextRunAt(schedule quotaTriggerSchedule, now time.Time) time.Time {
	due, _, ok := quotaTriggerDueAt(schedule, now)
	if !ok || !schedule.Enabled {
		return time.Time{}
	}
	if !due.After(now) {
		location, errLocation := time.LoadLocation(schedule.Timezone)
		if errLocation != nil {
			return time.Time{}
		}
		local := now.In(location).Add(24 * time.Hour)
		parsed, errParse := time.Parse("15:04", schedule.Time)
		if errParse != nil {
			return time.Time{}
		}
		due = time.Date(local.Year(), local.Month(), local.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	}
	return due
}

func triggerDueQuotaSchedules(now time.Time) {
	type job struct {
		id       string
		schedule quotaTriggerSchedule
	}
	jobs := make([]job, 0)

	quotaTriggerManager.Lock()
	for id, schedule := range quotaTriggerManager.schedules {
		if !schedule.Enabled {
			continue
		}
		dueAt, dateKey, ok := quotaTriggerDueAt(schedule, now)
		if !ok || now.Before(dueAt) {
			continue
		}
		runtime := quotaTriggerManager.runtime[id]
		if runtime == nil {
			runtime = &quotaTriggerRuntime{}
			quotaTriggerManager.runtime[id] = runtime
		}
		if runtime.Running || runtime.LastScheduledDate == dateKey {
			continue
		}
		runtime.LastScheduledDate = dateKey
		runtime.Running = true
		runtime.LastStatus = "running"
		runtime.LastError = ""
		jobs = append(jobs, job{id: id, schedule: schedule})
	}
	quotaTriggerManager.Unlock()

	for _, item := range jobs {
		go func(item job) {
			errTrigger := executeQuotaTrigger(item.schedule, "")
			finishQuotaTrigger(item.id, errTrigger)
		}(item)
	}
}

func beginQuotaTrigger(id string) (quotaTriggerSchedule, error) {
	quotaTriggerManager.Lock()
	defer quotaTriggerManager.Unlock()
	schedule, ok := quotaTriggerManager.schedules[id]
	if !ok {
		return quotaTriggerSchedule{}, fmt.Errorf("计划不存在")
	}
	runtime := quotaTriggerManager.runtime[id]
	if runtime == nil {
		runtime = &quotaTriggerRuntime{}
		quotaTriggerManager.runtime[id] = runtime
	}
	if runtime.Running {
		return quotaTriggerSchedule{}, fmt.Errorf("该计划正在执行")
	}
	runtime.Running = true
	runtime.LastStatus = "running"
	runtime.LastError = ""
	return schedule, nil
}

func finishQuotaTrigger(id string, errTrigger error) {
	quotaTriggerManager.Lock()
	defer quotaTriggerManager.Unlock()
	runtime := quotaTriggerManager.runtime[id]
	if runtime == nil {
		return
	}
	runtime.Running = false
	runtime.LastRunAt = time.Now().UTC()
	if errTrigger == nil {
		runtime.LastStatus = "success"
		runtime.LastError = ""
		return
	}
	runtime.LastStatus = "error"
	runtime.LastError = quotaTriggerSafeError(errTrigger)
}

func quotaTriggerViews() []quotaTriggerView {
	now := time.Now()
	quotaTriggerManager.Lock()
	defer quotaTriggerManager.Unlock()
	ids := make([]string, 0, len(quotaTriggerManager.schedules))
	for id := range quotaTriggerManager.schedules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	views := make([]quotaTriggerView, 0, len(ids))
	for _, id := range ids {
		schedule := quotaTriggerManager.schedules[id]
		runtime := quotaTriggerManager.runtime[id]
		view := quotaTriggerView{quotaTriggerSchedule: schedule, LastStatus: "pending"}
		if runtime != nil {
			view.Running = runtime.Running
			if runtime.LastStatus != "" {
				view.LastStatus = runtime.LastStatus
			}
			view.LastError = runtime.LastError
			if !runtime.LastRunAt.IsZero() {
				view.LastRunAt = runtime.LastRunAt.Format(time.RFC3339)
			}
		}
		if next := quotaTriggerNextRunAt(schedule, now); !next.IsZero() {
			view.NextRunAt = next.Format(time.RFC3339)
		}
		views = append(views, view)
	}
	return views
}

func quotaTriggerViewByID(id string) (quotaTriggerView, bool) {
	for _, view := range quotaTriggerViews() {
		if view.ID == id {
			return view, true
		}
	}
	return quotaTriggerView{}, false
}

func handleQuotaTriggersGet() ([]byte, error) {
	return okEnvelope(managementResponse{
		StatusCode: http.StatusOK,
		Headers:    jsonHeaders(),
		Body: mustJSON(map[string]any{
			"provider":     providerID,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"schedules":    quotaTriggerViews(),
		}),
	})
}

func handleQuotaTriggerRun(req managementRequest) ([]byte, error) {
	var body struct {
		ID string `json:"id"`
	}
	if errDecode := json.Unmarshal(req.Body, &body); errDecode != nil || strings.TrimSpace(body.ID) == "" {
		return okEnvelope(managementResponse{StatusCode: http.StatusBadRequest, Headers: jsonHeaders(), Body: mustJSON(map[string]any{"error": "id_required", "message": "计划 id 不能为空"})})
	}
	schedule, errBegin := beginQuotaTrigger(strings.TrimSpace(body.ID))
	if errBegin != nil {
		return okEnvelope(managementResponse{StatusCode: http.StatusConflict, Headers: jsonHeaders(), Body: mustJSON(map[string]any{"error": "trigger_busy", "message": errBegin.Error()})})
	}
	errTrigger := executeQuotaTrigger(schedule, req.HostCallbackID)
	finishQuotaTrigger(schedule.ID, errTrigger)
	view, _ := quotaTriggerViewByID(schedule.ID)
	if errTrigger != nil {
		return okEnvelope(managementResponse{StatusCode: http.StatusOK, Headers: jsonHeaders(), Body: mustJSON(map[string]any{
			"status": "error", "message": quotaTriggerSafeError(errTrigger), "schedule": view,
		})})
	}
	return okEnvelope(managementResponse{StatusCode: http.StatusOK, Headers: jsonHeaders(), Body: mustJSON(map[string]any{
		"status": "success", "schedule": view,
	})})
}

func executeQuotaTrigger(schedule quotaTriggerSchedule, callbackID string) error {
	result, errGet := callHostCall("host.auth.get", map[string]any{"auth_index": schedule.AuthIndex})
	if errGet != nil {
		return fmt.Errorf("读取凭证失败")
	}
	var auth hostAuthGetResponse
	if json.Unmarshal(result, &auth) != nil || len(auth.JSON) == 0 {
		return fmt.Errorf("读取凭证失败")
	}
	if runtimeResult, errRuntime := callHostCall("host.auth.get_runtime", map[string]any{"auth_index": schedule.AuthIndex}); errRuntime == nil {
		var runtime struct {
			Auth struct {
				Disabled bool `json:"disabled"`
			} `json:"auth"`
		}
		if json.Unmarshal(runtimeResult, &runtime) == nil && runtime.Auth.Disabled {
			return fmt.Errorf("凭证已停用")
		}
	}
	material, errMaterial := quotaactivation.ParseAuthMaterial(auth.JSON)
	if errMaterial != nil {
		return errMaterial
	}

	var protocol quotaactivation.ProtocolRequest
	var errProtocol error
	switch normalizeQuotaTriggerProvider(schedule.Provider) {
	case "codex":
		protocol, errProtocol = quotaactivation.BuildCodexProtocol(material, schedule.Model, quotaTriggerPrompt)
	case "antigravity":
		protocol, errProtocol = quotaactivation.BuildAntigravityProtocol(material, schedule.Model, quotaTriggerPrompt)
	default:
		return fmt.Errorf("不支持的提供方")
	}
	if errProtocol != nil {
		return errProtocol
	}
	response, errHTTP := hostHTTPDoCall(hostHTTPRequest{
		HostCallbackID: callbackID,
		Method:         protocol.Method,
		URL:            protocol.URL,
		Headers:        protocol.Headers,
		Body:           protocol.Body,
	})
	if errHTTP != nil {
		return fmt.Errorf("上游请求失败")
	}
	var ok bool
	var message string
	if normalizeQuotaTriggerProvider(schedule.Provider) == "codex" {
		ok, message = quotaactivation.EvaluateCodexActivationSuccess(response.StatusCode, response.Body)
	} else {
		ok, message = quotaactivation.EvaluateAntigravityActivationSuccess(response.StatusCode, response.Body)
	}
	if !ok {
		if strings.TrimSpace(message) == "" {
			message = "上游返回无效响应"
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func quotaTriggerSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" || strings.Contains(strings.ToLower(message), "bearer ") {
		return "上游请求失败"
	}
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}
