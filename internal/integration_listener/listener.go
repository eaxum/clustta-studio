// Package integration_listener runs per-studio background workers that
// subscribe to external systems (currently Kitsu) and reflect task
// assignments and statuses into Clustta project databases.
//
// One file holds the manager, listener, dispatcher, reconciler and the
// assignee resolver because they are tightly coupled and small.
package integration_listener

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clustta/internal/integrations"
	"clustta/internal/repository"
	"clustta/internal/server/studio_integration_service"
	"clustta/internal/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// ReconcileInterval is how often each listener re-pulls authoritative state
// from the external system to catch any events it may have missed. It is a
// var rather than a const so the server can override it from CONFIG at
// startup (see SetReconcileInterval).
var ReconcileInterval = 30 * time.Minute

// SetReconcileInterval overrides the default reconcile cadence. Values
// <= 0 are ignored so a missing or zero env var keeps the safe default.
func SetReconcileInterval(d time.Duration) {
	if d > 0 {
		ReconcileInterval = d
	}
}

// backoffMax caps the reconnect delay after repeated failures.
const backoffMax = 60 * time.Second

// Manager owns one StudioListener per (studio_id, integration_id) and
// coordinates startup, shutdown and restart on config changes.
type Manager struct {
	centralDB   *sqlx.DB
	masterKey   []byte
	projectsDir string

	mu        sync.Mutex
	listeners map[string]*StudioListener // key = studioId + ":" + integrationId
}

// NewManager constructs an idle manager. Call StartAll to boot listeners
// from persisted config rows.
func NewManager(centralDB *sqlx.DB, masterKey []byte, projectsDir string) *Manager {
	return &Manager{
		centralDB:   centralDB,
		masterKey:   masterKey,
		projectsDir: projectsDir,
		listeners:   make(map[string]*StudioListener),
	}
}

// StartAll loads every enabled studio_integration_config row and starts a
// listener for each. Failures are logged; they do not abort startup.
func (m *Manager) StartAll(ctx context.Context) {
	tx, err := m.centralDB.Beginx()
	if err != nil {
		log.Printf("listener: StartAll begin tx: %v", err)
		return
	}
	defer tx.Rollback()
	configs, err := studio_integration_service.List(tx)
	if err != nil {
		log.Printf("listener: StartAll list configs: %v", err)
		return
	}
	for _, cfg := range configs {
		if err := m.startLocked(ctx, cfg); err != nil {
			log.Printf("listener: start %s/%s: %v", cfg.StudioId, cfg.IntegrationId, err)
		}
	}
}

// Start spawns a listener for the given (studio, integration) if one is not
// already running. Safe to call from HTTP handlers after a config save.
func (m *Manager) Start(ctx context.Context, studioId, integrationId string) error {
	tx, err := m.centralDB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cfg, err := studio_integration_service.Get(tx, studioId, integrationId)
	if err != nil {
		return err
	}
	return m.startLocked(ctx, cfg)
}

func (m *Manager) startLocked(ctx context.Context, cfg studio_integration_service.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := listenerKey(cfg.StudioId, cfg.IntegrationId)
	if _, exists := m.listeners[key]; exists {
		return nil
	}
	lctx, cancel := context.WithCancel(ctx)
	l := &StudioListener{
		manager:   m,
		config:    cfg,
		ctx:       lctx,
		cancel:    cancel,
		eventChan: make(chan integrations.KitsuEvent, 100),
	}
	m.listeners[key] = l
	go l.run()
	return nil
}

// Stop cancels and removes the listener for (studio, integration). Returns
// nil if no listener was running.
func (m *Manager) Stop(studioId, integrationId string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := listenerKey(studioId, integrationId)
	if l, ok := m.listeners[key]; ok {
		l.cancel()
		delete(m.listeners, key)
	}
}

// Restart stops then starts the listener so a credentials or URL change
// takes effect immediately.
func (m *Manager) Restart(ctx context.Context, studioId, integrationId string) error {
	m.Stop(studioId, integrationId)
	return m.Start(ctx, studioId, integrationId)
}

// StopAll cancels every listener; called during graceful shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, l := range m.listeners {
		l.cancel()
		delete(m.listeners, k)
	}
}

// Status reports whether a listener is currently running.
func (m *Manager) Status(studioId, integrationId string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.listeners[listenerKey(studioId, integrationId)]; ok {
		return "running"
	}
	return "stopped"
}

func listenerKey(studioId, integrationId string) string {
	return studioId + ":" + integrationId
}

// recordError persists the latest listener error so the studio admin can
// see it in the settings UI. Sanitises the message to protect the admin
// from upstream control characters and bound the size of the stored row.
func (m *Manager) recordError(cfgId, msg string) {
	tx, err := m.centralDB.Beginx()
	if err != nil {
		return
	}
	defer tx.Rollback()
	_ = studio_integration_service.SetLastError(tx, cfgId, sanitiseLastError(msg))
	_ = tx.Commit()
}

// maxLastErrorLen caps how much upstream text we persist to keep UI rows
// readable and the central DB compact even if Kitsu echoes a giant payload.
const maxLastErrorLen = 500

// sanitiseLastError strips control characters from upstream messages and
// truncates to maxLastErrorLen. The output is plain ASCII-safe text that
// the FE can render via Vue interpolation without surprises.
func sanitiseLastError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		switch {
		case r == '\t' || r == ' ':
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			// drop other control chars (newlines, escape, etc.)
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > maxLastErrorLen {
		out = out[:maxLastErrorLen-1] + "\u2026"
	}
	return out
}

// recordValidated clears any prior error and marks the listener healthy.
func (m *Manager) recordValidated(cfgId string) {
	tx, err := m.centralDB.Beginx()
	if err != nil {
		return
	}
	defer tx.Rollback()
	_ = studio_integration_service.SetLastValidated(tx, cfgId, time.Now().Unix())
	_ = tx.Commit()
}

// StudioListener subscribes to one external system on behalf of one studio
// and writes assignment updates into that studio's project databases.
type StudioListener struct {
	manager *Manager
	config  studio_integration_service.Config

	ctx    context.Context
	cancel context.CancelFunc

	eventChan chan integrations.KitsuEvent
	resolver  *assigneeResolver

	// reconciling guards against overlapping reconcile runs; reconcile
	// can be slow and we run it in its own goroutine so live events stay
	// responsive (see session()).
	reconciling atomic.Bool

	// projectLinks caches each Clustta project's link row
	// (clustta projectId -> external projectId) so dispatch and reconcile
	// don't reopen every project DB just to read this single row. Refreshed
	// on every reconcile cycle.
	linksMu      sync.RWMutex
	projectLinks map[string]string
}

// run is the listener's main loop. It re-establishes the Kitsu session on
// failure with exponential backoff. Live events come from the socket
// subscriber; the reconcile timer is a safety net for missed events.
func (l *StudioListener) run() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("listener: PANIC studio=%s err=%v\n%s", l.config.StudioId, r, debug.Stack())
			l.manager.recordError(l.config.Id, fmt.Sprintf("listener panic: %v", r))
		}
	}()

	backoff := time.Second
	for {
		if err := l.session(); err != nil {
			if l.ctx.Err() != nil {
				return
			}
			log.Printf("listener: session error studio=%s: %v", l.config.StudioId, err)
			l.manager.recordError(l.config.Id, err.Error())
			select {
			case <-l.ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > backoffMax {
				backoff = backoffMax
			}
			continue
		}
		// session() returned cleanly only when ctx was cancelled
		return
	}
}

// session authenticates once, runs reconcile, then loops on events + timer
// until ctx is cancelled or the underlying subscriber errors.
func (l *StudioListener) session() error {
	integration, err := integrations.Get(l.config.IntegrationId)
	if err != nil {
		return fmt.Errorf("integration not registered: %w", err)
	}

	creds, err := studio_integration_service.DecryptCredentials(l.config, l.manager.masterKey)
	if err != nil {
		return fmt.Errorf("decrypt credentials: %w", err)
	}

	authResult, err := integration.Authenticate(map[string]string{
		"email":    creds.Email,
		"password": creds.Password,
		"api_url":  l.config.ApiUrl,
	})
	if err != nil || !authResult.Success {
		msg := err
		if msg == nil {
			msg = fmt.Errorf("%s", authResult.Error)
		}
		return fmt.Errorf("authenticate: %w", msg)
	}
	token := authResult.AccessToken
	l.manager.recordValidated(l.config.Id)

	kitsuClient, ok := integration.(*integrations.KitsuClient)
	if !ok {
		return fmt.Errorf("integration %s is not a KitsuClient", l.config.IntegrationId)
	}
	l.resolver = newAssigneeResolver(kitsuClient, token, l.config.ApiUrl)

	// Reconcile once on session start to catch anything missed while the
	// listener was down (also runs whenever the integration is toggled on).
	// Runs in its own goroutine so the main select loop below can start
	// servicing live events immediately instead of blocking on a possibly
	// minutes-long first reconcile for big studios.
	l.spawnReconcile(kitsuClient, token)

	subscriber := integrations.NewKitsuSocketSubscriber(token, l.config.ApiUrl)
	subCtx, subCancel := context.WithCancel(l.ctx)
	defer subCancel()
	subDone := make(chan error, 1)
	go func() { subDone <- subscriber.Subscribe(subCtx, l.eventChan) }()

	// Coalesce events by TaskId over a short window so a burst of edits
	// to the same task (e.g. task:update + task:assign + task:status
	// fired together) collapses into a single Kitsu GetTask call.
	taskFetchChan := make(chan string, cap(l.eventChan))
	go coalesceEvents(subCtx, l.eventChan, taskFetchChan)

	ticker := time.NewTicker(ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			return nil
		case taskId := <-taskFetchChan:
			log.Printf("listener: fetch task=%s", taskId)
			if err := l.fetchAndDispatch(kitsuClient, token, taskId); err != nil {
				log.Printf("listener: handle task=%s: %v", taskId, err)
			}
		case <-ticker.C:
			l.spawnReconcile(kitsuClient, token)
		case err := <-subDone:
			if l.ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("subscriber: %w", err)
			}
			return fmt.Errorf("subscriber exited unexpectedly")
		}
	}
}

// fetchAndDispatch is the post-coalesce hot path: GetTask once for a task
// id then dispatch the authoritative state to every matching project.
func (l *StudioListener) fetchAndDispatch(client *integrations.KitsuClient, token, taskId string) error {
	assignment, err := client.GetTask(token, l.config.ApiUrl, taskId)
	if err != nil {
		return err
	}
	projects, err := l.studioProjects()
	if err != nil {
		return err
	}
	return l.dispatch(projects, assignment)
}

// dispatch applies one authoritative assignment to every project DB owned
// by this studio that is linked to the source Kitsu project. Callers must
// pass the studio's project id snapshot; this avoids re-querying the
// central DB for every assignment during a reconcile fanout.
func (l *StudioListener) dispatch(projects []string, assignment integrations.ExternalAssignment) error {
	log.Printf("listener: dispatch task=%s assignees=%s projects=%d",
		assignment.TaskId, summariseAssignees(assignment.PersonIds), len(projects))

	// Pre-filter the project list to ones whose cached link matches this
	// assignment's external project id. Falls back to iterating all
	// projects when the cache is cold (first event before initial
	// reconcile completes); applyToProject will skip non-linked rows.
	targets := l.filterLinkedProjects(projects, assignment.ProjectId)

	// Resolve the first assignee's email once via Kitsu (cached) before
	// touching any project DB, so no HTTP call is held inside a SQLite
	// write transaction.
	var assigneeEmail string
	if len(assignment.PersonIds) > 0 {
		email, err := l.resolver.email(assignment.PersonIds[0])
		if err != nil {
			log.Printf("listener: resolve assignee task=%s person=%s: %v",
				assignment.TaskId, assignment.PersonIds[0], err)
		} else {
			assigneeEmail = email
		}
	}

	for _, projectId := range targets {
		// Single-tenant flat layout: no studio_id segment in path.
		path := filepath.Join(l.manager.projectsDir, projectId+".clst")
		if !utils.FileExists(path) {
			continue
		}
		status, err := l.applyToProject(path, assignment, assigneeEmail)
		if err != nil {
			log.Printf("listener: apply project=%s task=%s error: %v", projectId, assignment.TaskId, err)
			continue
		}
		if strings.HasPrefix(status, "skipped") {
			continue
		}
		log.Printf("listener: apply project=%s task=%s %s", projectId, assignment.TaskId, status)
	}
	return nil
}

// applyToProject performs the writes per matched task in one transaction:
//  1. asset.assignee_id (what artists see).
//  2. asset.status_id (when sync_options.status_mappings translates the external status).
//  3. integration_asset_mapping memory (external_assignees, external_status).
//
// Returns a short status string describing what happened so the caller can log it.
// Bumps the project's sync_token when the asset actually changes so connected
// clients pick up the change on their next sync-token poll.
//
// assigneeEmail is the pre-resolved email of the new assignee ("" when
// unassigned or unresolvable); fetched outside the tx by the dispatcher.
func (l *StudioListener) applyToProject(projectPath string, a integrations.ExternalAssignment, assigneeEmail string) (string, error) {
	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		return "", err
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	link, err := repository.GetIntegrationProjectByIntegrationId(tx, a.IntegrationId)
	if err != nil || link.ExternalProjectId != a.ProjectId {
		return "skipped-not-linked", nil
	}

	mapping, err := repository.GetAssetMappingByExternalId(tx, a.IntegrationId, a.TaskId)
	if err != nil || mapping.AssetId == "" {
		return "skipped-no-mapping", nil
	}

	previousAssignees := decodeAssignees(mapping.ExternalAssignees)
	assigneesSame := assigneesEqual(previousAssignees, a.PersonIds)
	statusSame := mapping.ExternalStatus == a.Status
	if assigneesSame && statusSame {
		return "skipped-unchanged", nil
	}

	var newUserId string
	if assigneeEmail != "" {
		if uid, ok := l.resolver.userIdForEmail(tx, assigneeEmail); ok {
			newUserId = uid
		}
	}

	// Resolve external status id to Clustta status id via sync_options.status_mappings.
	// The map is keyed by clustta_status_id so we invert it for inbound lookups.
	newStatusId, statusMapped := resolveStatusId(link.SyncOptions, a.Status)

	asset, err := repository.GetAsset(tx, mapping.AssetId)
	if err != nil {
		if _, uerr := repository.UpdateAssetMapping(tx,
			mapping.Id, mapping.ExternalName, mapping.ExternalParentId,
			mapping.ExternalType, a.Status,
			encodeAssignees(a.PersonIds), mapping.ExternalMetadata,
			"", mapping.LastPushedCheckpointId,
			time.Now().UTC().Format(time.RFC3339),
		); uerr != nil {
			return "", uerr
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "mapping-only-asset-missing", nil
	}

	assigneeChanged := false
	statusChanged := false
	if !assigneesSame {
		if newUserId == "" {
			if asset.AssigneeId != "" {
				if err := repository.UnAssignAsset(tx, asset.Id); err != nil {
					return "", err
				}
				assigneeChanged = true
			}
		} else if asset.AssigneeId != newUserId {
			if err := repository.AssignAsset(tx, asset.Id, newUserId); err != nil {
				return "", err
			}
			assigneeChanged = true
		}
	}
	if !statusSame && statusMapped && newStatusId != "" && asset.StatusId != newStatusId {
		if err := repository.UpdateStatus(tx, asset.Id, newStatusId); err != nil {
			return "", err
		}
		statusChanged = true
	}

	_, err = repository.UpdateAssetMapping(tx,
		mapping.Id, mapping.ExternalName, mapping.ExternalParentId,
		mapping.ExternalType, a.Status,
		encodeAssignees(a.PersonIds), mapping.ExternalMetadata,
		mapping.AssetId, mapping.LastPushedCheckpointId,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}

	// Bump sync_token so connected clients pull the new state on next poll.
	assetChanged := assigneeChanged || statusChanged
	if assetChanged {
		if err := utils.SetProjectSyncToken(tx, uuid.New().String()); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	if !assetChanged {
		if !statusSame && !statusMapped {
			return "status-no-mapping:" + a.Status, nil
		}
		return "mapping-only-resolver-empty", nil
	}
	parts := []string{}
	if assigneeChanged {
		if newUserId == "" {
			parts = append(parts, "unassigned")
		} else {
			parts = append(parts, "assigned:"+newUserId)
		}
	}
	if statusChanged {
		parts = append(parts, "status:"+newStatusId)
	}
	return strings.Join(parts, "+"), nil
}

// resolveStatusId inverts sync_options.status_mappings (clustta_id -> external_id)
// and returns the Clustta status id for the supplied external status id.
// The bool reports whether a mapping existed.
func resolveStatusId(syncOptionsJSON, externalStatusId string) (string, bool) {
	if externalStatusId == "" || syncOptionsJSON == "" {
		return "", false
	}
	var opts integrations.SyncOptions
	if err := json.Unmarshal([]byte(syncOptionsJSON), &opts); err != nil {
		return "", false
	}
	for clustta, external := range opts.StatusMappings {
		if external == externalStatusId {
			return clustta, true
		}
	}
	return "", false
}

// spawnReconcile runs reconcile in a separate goroutine so the main
// session() select keeps draining live events. If a previous reconcile is
// still in-flight (slow Kitsu, big project), the new tick is dropped; the
// next tick will pick up the work and there is no value in stacking them.
func (l *StudioListener) spawnReconcile(client *integrations.KitsuClient, token string) {
	if !l.reconciling.CompareAndSwap(false, true) {
		log.Printf("listener: reconcile skipped studio=%s (previous still running)", l.config.StudioId)
		return
	}
	go func() {
		defer l.reconciling.Store(false)
		if err := l.reconcile(client, token); err != nil {
			log.Printf("listener: reconcile error studio=%s: %v", l.config.StudioId, err)
		}
	}()
}

// coalesceWindow is how long coalesceEvents waits after the first event in
// a burst before flushing distinct task IDs downstream. Long enough for
// Kitsu to emit a related task:update + task:assign + task:status triplet
// in one batch, short enough that the artist sees the change quickly.
const coalesceWindow = 200 * time.Millisecond

// coalesceEvents reads raw Kitsu events and forwards each distinct TaskId
// downstream at most once per coalesceWindow. Multiple events for the same
// task within the window collapse into a single fetch, which avoids
// hammering Kitsu when admins do rapid edits in the Zou UI.
func coalesceEvents(ctx context.Context, in <-chan integrations.KitsuEvent, out chan<- string) {
	pending := make(map[string]struct{})
	var timer *time.Timer
	var timerCh <-chan time.Time

	flush := func() {
		for id := range pending {
			select {
			case out <- id:
			case <-ctx.Done():
				return
			}
		}
		pending = make(map[string]struct{})
		timer = nil
		timerCh = nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				flush()
				return
			}
			if ev.TaskId == "" {
				continue
			}
			pending[ev.TaskId] = struct{}{}
			if timer == nil {
				timer = time.NewTimer(coalesceWindow)
				timerCh = timer.C
			}
		case <-timerCh:
			flush()
		}
	}
}

// reconcile pulls the authoritative assignment state for every linked
// Kitsu project this studio has and applies any diffs. Catches events
// missed while the listener was disconnected.
func (l *StudioListener) reconcile(client *integrations.KitsuClient, token string) error {
	log.Printf("listener: reconcile studio=%s", l.config.StudioId)
	projects, err := l.studioProjects()
	if err != nil {
		return err
	}
	// Refresh the per-listener project-link cache as a side effect; the
	// freshly-built map is also used directly below to drive the reconcile
	// fetch + dispatch fanout for this cycle.
	links := l.refreshProjectLinks(projects)

	// Invert clustta -> external into external -> []clustta so we hit each
	// distinct external project exactly once per cycle.
	kitsuProjects := make(map[string][]string, len(links))
	for projectId, externalId := range links {
		kitsuProjects[externalId] = append(kitsuProjects[externalId], projectId)
	}

	for kitsuProjectId := range kitsuProjects {
		assignments, err := client.GetProjectAssignments(token, l.config.ApiUrl, kitsuProjectId)
		if err != nil {
			log.Printf("listener: fetch kitsu project %s: %v", kitsuProjectId, err)
			continue
		}
		for _, a := range assignments {
			if err := l.dispatch(projects, a); err != nil {
				log.Printf("listener: dispatch task %s: %v", a.TaskId, err)
			}
		}
	}
	return nil
}

// refreshProjectLinks rebuilds the cached (clustta projectId -> external
// projectId) map by reading the link row from each project DB. Called from
// reconcile so dispatch can later filter projects without re-opening DBs.
// Returns the freshly built map; also stores it on the listener.
func (l *StudioListener) refreshProjectLinks(projects []string) map[string]string {
	links := make(map[string]string, len(projects))
	for _, projectId := range projects {
		// Single-tenant flat layout: no studio_id segment in path.
		path := filepath.Join(l.manager.projectsDir, projectId+".clst")
		if !utils.FileExists(path) {
			continue
		}
		dbConn, err := utils.OpenDb(path)
		if err != nil {
			continue
		}
		tx, err := dbConn.Beginx()
		if err != nil {
			dbConn.Close()
			continue
		}
		link, err := repository.GetIntegrationProjectByIntegrationId(tx, l.config.IntegrationId)
		tx.Rollback()
		dbConn.Close()
		if err != nil || link.ExternalProjectId == "" {
			continue
		}
		links[projectId] = link.ExternalProjectId
	}
	l.linksMu.Lock()
	l.projectLinks = links
	l.linksMu.Unlock()
	return links
}

// filterLinkedProjects narrows a project list to those whose cached link
// matches externalProjectId. Returns the original list unchanged when the
// cache is empty (cold-start before the first reconcile) so dispatch
// continues to function before warmup; applyToProject will then skip any
// non-matching projects via its in-tx link check.
func (l *StudioListener) filterLinkedProjects(projects []string, externalProjectId string) []string {
	l.linksMu.RLock()
	cache := l.projectLinks
	l.linksMu.RUnlock()
	if len(cache) == 0 || externalProjectId == "" {
		return projects
	}
	out := make([]string, 0, len(projects))
	for _, projectId := range projects {
		if cache[projectId] == externalProjectId {
			out = append(out, projectId)
		}
	}
	return out
}

// studioProjects returns Clustta project IDs by listing *.clst in projectsDir.
// Single-tenant: no central registry table to query.
func (l *StudioListener) studioProjects() ([]string, error) {
	entries, err := os.ReadDir(l.manager.projectsDir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".clst") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".clst"))
	}
	return ids, nil
}

// assigneeResolver translates Kitsu person IDs into Clustta user IDs via
// the email join. Caches the externalId -> email lookup per listener so
// reconciling N tasks for the same assignee only costs one Kitsu call.
// Entries expire after assigneeCacheTTL so an upstream email change is
// picked up without needing a listener restart.
type assigneeResolver struct {
	client *integrations.KitsuClient
	token  string
	apiUrl string

	mu    sync.Mutex
	cache map[string]assigneeCacheEntry // externalPersonId -> entry
}

// assigneeCacheTTL bounds how long a stale Kitsu person -> email mapping
// can survive in the resolver cache. 15 min is comfortably below the
// 30-min reconcile cadence so a renamed user's email is picked up by the
// next reconcile at the latest.
const assigneeCacheTTL = 15 * time.Minute

type assigneeCacheEntry struct {
	email     string
	expiresAt time.Time
}

func newAssigneeResolver(client *integrations.KitsuClient, token, apiUrl string) *assigneeResolver {
	return &assigneeResolver{
		client: client,
		token:  token,
		apiUrl: apiUrl,
		cache:  make(map[string]assigneeCacheEntry),
	}
}

// resolve returns the Clustta user ID for an external person, looking up
// email via Kitsu (cached) and then `user` table in the project DB.
// Returns (("", false, nil)) when the email has no matching Clustta user.
//
// Split into two phases (email fetch + user lookup) so the HTTP call is
// never held inside a SQLite write transaction.
func (r *assigneeResolver) email(externalPersonId string) (string, error) {
	if externalPersonId == "" {
		return "", nil
	}
	now := time.Now()
	r.mu.Lock()
	entry, cached := r.cache[externalPersonId]
	r.mu.Unlock()
	if cached && now.Before(entry.expiresAt) {
		return entry.email, nil
	}
	person, err := r.client.GetPerson(r.token, r.apiUrl, externalPersonId)
	if err != nil {
		return "", err
	}
	email := strings.ToLower(strings.TrimSpace(person.Email))
	r.mu.Lock()
	r.cache[externalPersonId] = assigneeCacheEntry{
		email:     email,
		expiresAt: now.Add(assigneeCacheTTL),
	}
	r.mu.Unlock()
	return email, nil
}

// userIdForEmail returns the Clustta user id for an email, scoped to one
// project DB transaction. Returns ("", false) when no match.
func (r *assigneeResolver) userIdForEmail(tx *sqlx.Tx, email string) (string, bool) {
	if email == "" {
		return "", false
	}
	var userId string
	if err := tx.Get(&userId, `SELECT id FROM user WHERE LOWER(email) = ? LIMIT 1`, email); err != nil {
		return "", false
	}
	return userId, true
}

// decodeAssignees parses the JSON array stored in
// integration_asset_mapping.external_assignees. Tolerates empty/invalid values.
func decodeAssignees(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// encodeAssignees serialises a list back to the JSON shape expected by the
// mapping column.
func encodeAssignees(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// summariseAssignees renders an assignee list for logs without dumping
// every UUID. Shows the first id plus a "+N more" suffix so noisy fanout
// to many assignees doesn't explode log volume.
func summariseAssignees(ids []string) string {
	switch len(ids) {
	case 0:
		return "[]"
	case 1:
		return "[" + ids[0] + "]"
	default:
		return fmt.Sprintf("[%s +%d]", ids[0], len(ids)-1)
	}
}

// assigneesEqual returns true when two assignee sets are identical (order-
// insensitive). Used to short-circuit no-op updates.
func assigneesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
	}
	return true
}
