package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type apiServer struct {
	db     *sql.DB
	origin string
}

func (a *apiServer) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := a.db.PingContext(ctx); err != nil {
			problem(w, 503, "database_unavailable")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/v1/auth/register", a.register)
	m.HandleFunc("POST /api/v1/auth/login", a.login)
	m.HandleFunc("GET /api/v1/auth/session", a.protected(a.session))
	m.HandleFunc("POST /api/v1/auth/logout", a.protected(a.logout))
	m.HandleFunc("POST /api/v1/research/tasks", a.protected(a.createTask))
	m.HandleFunc("GET /api/v1/research/tasks", a.protected(a.listTasks))
	m.HandleFunc("GET /api/v1/research/tasks/{id}", a.protected(a.getTask))
	m.HandleFunc("GET /api/v1/research/tasks/{id}/events", a.protected(a.events))
	m.HandleFunc("GET /api/v1/research/tasks/{id}/report", a.protected(a.report))
	m.HandleFunc("POST /api/v1/research/tasks/{id}/cancel", a.protected(a.cancelTask))
	m.HandleFunc("GET /api/v1/model-config", a.protected(a.modelConfig))
	m.HandleFunc("PUT /api/v1/model-config", a.protected(a.modelConfig))
	m.HandleFunc("GET /api/v1/research/tasks/{id}/sources/{source}", a.protected(a.sources))
	m.Handle("GET /", http.FileServer(http.Dir(env("WEB_DIR", "web/dist"))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				origin := r.Header.Get("Origin")
				if (origin != "" && origin != a.origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
					problem(w, 403, "origin_forbidden")
					return
				}
				media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if media != "application/json" {
					problem(w, 415, "json_required")
					return
				}
			}
		}
		m.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func problem(w http.ResponseWriter, status int, code string) {
	reply(w, status, map[string]string{"code": code})
}

func internal(w http.ResponseWriter, err error) {
	log.Printf("API error: %T", err)
	problem(w, 500, "internal_error")
}

func parse(w http.ResponseWriter, r *http.Request, dst any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(new(any)) != io.EOF {
		problem(w, 422, "invalid_json")
		return false
	}
	return true
}

type credentials struct {
	Email    string
	Password string
}

func (a *apiServer) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !parse(w, r, &c) {
		return
	}
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	local, domain, ok := strings.Cut(c.Email, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") ||
		strings.ContainsAny(c.Email, " \t\r\n") || strings.Contains(domain, "@") ||
		len(c.Email) > 254 || len(c.Password) < 10 || len(c.Password) > 72 {
		problem(w, 422, "invalid_credentials")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(w, err)
		return
	}
	_, err = a.db.ExecContext(r.Context(),
		"INSERT INTO users(id,email,password_hash) VALUES(?,?,?)", identifier(16), c.Email, hash)
	if duplicate(err) {
		problem(w, 409, "registration_conflict")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, 201, map[string]bool{"created": true})
}

func duplicate(err error) bool {
	var dbErr *mysql.MySQLError
	return errors.As(err, &dbErr) && dbErr.Number == 1062
}

var dummyPassword, _ = bcrypt.GenerateFromPassword([]byte("unused-comparison-value"), bcrypt.DefaultCost)

func (a *apiServer) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !parse(w, r, &c) {
		return
	}
	var user string
	var hash []byte
	err := a.db.QueryRowContext(r.Context(), "SELECT id,password_hash FROM users WHERE email=?",
		strings.ToLower(strings.TrimSpace(c.Email))).Scan(&user, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyPassword
	} else if err != nil {
		internal(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(c.Password)) != nil || user == "" {
		problem(w, 401, "invalid_credentials")
		return
	}

	token := identifier(32)
	digest := sha256.Sum256([]byte(token))
	_, err = a.db.ExecContext(r.Context(),
		"INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,UTC_TIMESTAMP(6)+INTERVAL 8 HOUR)",
		digest[:], user)
	if err != nil {
		internal(w, err)
		return
	}
	a.setCookie(w, token, 8*3600)
	reply(w, 200, map[string]bool{"authenticated": true})
}

func (a *apiServer) setCookie(w http.ResponseWriter, token string, age int) {
	http.SetCookie(w, &http.Cookie{
		Name: "rp_session", Value: token, Path: "/", HttpOnly: true,
		Secure: strings.HasPrefix(a.origin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
}

func (a *apiServer) identity(ctx context.Context, r *http.Request) (string, error) {
	cookie, err := r.Cookie("rp_session")
	if err != nil {
		return "", sql.ErrNoRows
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	var user string
	err = a.db.QueryRowContext(ctx, "SELECT user_id FROM sessions WHERE token_hash=? AND expires_at>UTC_TIMESTAMP(6)",
		digest[:]).Scan(&user)
	return user, err
}

func (a *apiServer) protected(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.identity(r.Context(), r)
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 401, "authentication_required")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		h(w, r, user)
	}
}

func (a *apiServer) session(w http.ResponseWriter, _ *http.Request, _ string) {
	reply(w, 200, map[string]bool{"authenticated": true})
}

func (a *apiServer) logout(w http.ResponseWriter, r *http.Request, _ string) {
	cookie, _ := r.Cookie("rp_session")
	digest := sha256.Sum256([]byte(cookie.Value))
	if _, err := a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=?", digest[:]); err != nil {
		internal(w, err)
		return
	}
	a.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

type snapshot struct {
	ID             string  `json:"task_id"`
	Goal           string  `json:"goal"`
	Status         string  `json:"status"`
	Phase          *string `json:"phase"`
	Seq            uint64  `json:"event_seq,string"`
	Error          *string `json:"error_code,omitempty"`
	TokensUsed     uint32  `json:"tokens_used"`
	TokensReserved uint32  `json:"tokens_reserved"`
	TokenLimit     uint32  `json:"token_limit"`
}

const taskColumns = "id,goal,status,phase,event_seq,error_code,tokens_used,tokens_reserved,token_limit"

func (a *apiServer) readTask(ctx context.Context, tenant, id string) (s snapshot, err error) {
	err = a.db.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM research_tasks WHERE tenant_id=? AND id=?", tenant, id).
		Scan(&s.ID, &s.Goal, &s.Status, &s.Phase, &s.Seq, &s.Error, &s.TokensUsed, &s.TokensReserved, &s.TokenLimit)
	return s, err
}

func (a *apiServer) createTask(w http.ResponseWriter, r *http.Request, tenant string) {
	var input struct{ Goal string }
	if !parse(w, r, &input) {
		return
	}
	input.Goal = strings.TrimSpace(input.Goal)
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 || input.Goal == "" || utf8.RuneCountInString(input.Goal) > 2000 {
		problem(w, 422, "invalid_task_request")
		return
	}
	ctx := r.Context()
	kd, rd := sha256.Sum256([]byte(key)), sha256.Sum256([]byte(input.Goal))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()

	id := identifier(16)
	if _, err = tx.ExecContext(ctx, "INSERT INTO research_tasks(id,tenant_id,goal,status,deadline_at,event_seq,runtime_version) VALUES(?,?,?,'QUEUED',UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,1,'m2.1')", id, tenant, input.Goal); err != nil {
		internal(w, err)
		return
	}
	var modelVersion uint32
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM model_configs WHERE tenant_id=?", tenant).Scan(&modelVersion); err != nil {
		internal(w, err)
		return
	}
	if modelVersion > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET model_version=? WHERE tenant_id=? AND id=?", modelVersion, tenant, id); err != nil {
			internal(w, err)
			return
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO request_keys VALUES(?,?,?,?)", tenant, kd[:], rd[:], id)
	if duplicate(err) {
		_ = tx.Rollback()
		var digest []byte
		if err := a.db.QueryRowContext(ctx, "SELECT request_digest,task_id FROM request_keys WHERE tenant_id=? AND key_digest=?", tenant, kd[:]).Scan(&digest, &id); err != nil {
			internal(w, err)
			return
		}
		if string(digest) != string(rd[:]) {
			problem(w, 409, "idempotency_conflict")
			return
		}
	} else {
		if err != nil {
			internal(w, err)
			return
		}
		if err = appendEvent(ctx, tx, tenant, id, 1, "task_status", map[string]string{"status": "QUEUED"}); err != nil {
			internal(w, err)
			return
		}
		if err = addOutbox(ctx, tx, tenant, id, 1); err != nil {
			internal(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			internal(w, err)
			return
		}
	}
	s, err := a.readTask(ctx, tenant, id)
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/research/tasks/"+id)
	reply(w, 202, s)
}

func (a *apiServer) listTasks(w http.ResponseWriter, r *http.Request, tenant string) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT "+taskColumns+" FROM research_tasks WHERE tenant_id=? ORDER BY created_at DESC,id DESC LIMIT 50", tenant)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	tasks := make([]snapshot, 0)
	for rows.Next() {
		var s snapshot
		if err = rows.Scan(&s.ID, &s.Goal, &s.Status, &s.Phase, &s.Seq, &s.Error, &s.TokensUsed, &s.TokensReserved, &s.TokenLimit); err != nil {
			internal(w, err)
			return
		}
		tasks = append(tasks, s)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, tasks)
}

func (a *apiServer) getTask(w http.ResponseWriter, r *http.Request, tenant string) {
	s, err := a.readTask(r.Context(), tenant, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "task_not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, s)
}

func (a *apiServer) report(w http.ResponseWriter, r *http.Request, tenant string) {
	id := r.PathValue("id")
	s, err := a.readTask(r.Context(), tenant, id)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "task_not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if s.Status == "FAILED" || s.Status == "CANCELLED" {
		problem(w, 404, "report_unavailable")
		return
	}
	if s.Status != "SUCCEEDED" && s.Status != "PARTIAL" {
		problem(w, 409, "report_not_ready")
		return
	}
	var body string
	if err = a.db.QueryRowContext(r.Context(), "SELECT markdown FROM research_reports WHERE tenant_id=? AND task_id=?", tenant, id).Scan(&body); err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, map[string]string{"task_id": id, "markdown": body})
}

type eventRecord struct {
	seq       uint64
	kind      string
	payload   json.RawMessage
	timestamp string
}

func (a *apiServer) readEvents(ctx context.Context, tenant, id string, after uint64) ([]eventRecord, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT seq,event_type,payload,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%fZ') FROM task_events WHERE tenant_id=? AND task_id=? AND seq>? ORDER BY seq LIMIT 100", tenant, id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []eventRecord
	for rows.Next() {
		var e eventRecord
		var payload []byte
		if err := rows.Scan(&e.seq, &e.kind, &payload, &e.timestamp); err != nil {
			return nil, err
		}
		e.payload = payload
		events = append(events, e)
	}
	return events, rows.Err()
}

func (a *apiServer) events(w http.ResponseWriter, r *http.Request, tenant string) {
	ctx := r.Context()
	id := r.PathValue("id")
	s, err := a.readTask(ctx, tenant, id)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "task_not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}

	var cursor uint64
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	if raw != "" {
		cursor, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || cursor > s.Seq {
			problem(w, 422, "invalid_event_cursor")
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	control := http.NewResponseController(w)
	send := func(message string) error {
		_ = control.SetWriteDeadline(time.Now().Add(5 * time.Second))
		defer func() { _ = control.SetWriteDeadline(time.Time{}) }()
		if _, err := io.WriteString(w, message); err != nil {
			return err
		}
		return control.Flush()
	}
	if send(": connected\n\n") != nil {
		return
	}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		batch, err := a.readEvents(ctx, tenant, id, cursor)
		if err != nil {
			return
		}
		for _, e := range batch {
			data, err := json.Marshal(map[string]any{
				"schema_version": 1, "task_id": id, "seq": strconv.FormatUint(e.seq, 10),
				"payload": e.payload, "timestamp": e.timestamp,
			})
			if err != nil || send(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.seq, e.kind, data)) != nil {
				return
			}
			cursor = e.seq
		}
		s, err = a.readTask(ctx, tenant, id)
		if err != nil || (terminal(s.Status) && cursor >= s.Seq) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-heartbeat.C:
			if _, err := a.identity(ctx, r); err != nil || send(": heartbeat\n\n") != nil {
				return
			}
		}
	}
}
