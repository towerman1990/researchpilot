// Command gen generates typed queries for repository models.
package main

import (
	"gorm.io/gen"

	"researchpilot/internal/repository/model"
)

type sessionCommands interface {
	// SELECT user_id FROM @@table WHERE token_hash=UNHEX(@digestHex) AND expires_at>UTC_TIMESTAMP(6)
	FindSessionUser(digestHex string) ([]string, error)
	// DELETE FROM @@table WHERE token_hash=UNHEX(@digestHex)
	RemoveSession(digestHex string) error

	// INSERT INTO @@table (token_hash,user_id,expires_at) VALUES (UNHEX(@digestHex),@user,UTC_TIMESTAMP(6)+INTERVAL 8 HOUR)
	InsertSession(digestHex, user string) error
}

type taskCommands interface {
	// UPDATE @@table SET phase=@phase WHERE tenant_id=@tenant AND id=@id
	SetExecutionPhase(tenant, id, phase string) error
	// SELECT source_id FROM research_sources WHERE tenant_id=@tenant AND task_id=@id AND fingerprint=UNHEX(@fingerprint)
	FindExecutionSource(tenant, id, fingerprint string) ([]string, error)
	// INSERT INTO research_sources(tenant_id,task_id,source_id,title,canonical_url,excerpt,fingerprint) VALUES(@tenant,@id,@source,@title,@url,@excerpt,UNHEX(@fingerprint))
	InsertExecutionSource(tenant, id, source, title, url, excerpt, fingerprint string) error
	// INSERT INTO research_sources(tenant_id,task_id,source_id,title,canonical_url,excerpt,fingerprint,source_type) VALUES(@tenant,@id,@source,@title,@url,@excerpt,UNHEX(@fingerprint),'upload')
	InsertUploadedSource(tenant, id, source, title, url, excerpt, fingerprint string) error
	// SELECT source_id,title,canonical_url,excerpt,source_type FROM research_sources WHERE tenant_id=@tenant AND task_id=@id AND source_type='upload' ORDER BY retrieved_at,source_id
	ListUploadedSources(tenant, id string) ([]model.Source, error)
	// SELECT source_id,title,canonical_url,excerpt,source_type FROM research_sources WHERE tenant_id=@tenant AND task_id=@id ORDER BY retrieved_at,source_id LIMIT 8
	PartialReportSources(tenant, id string) ([]model.Source, error)
	// INSERT INTO research_reports(tenant_id,task_id,markdown,kind) VALUES(@tenant,@id,@markdown,@kind)
	InsertExecutionReport(tenant, id, markdown, kind string) error
	// INSERT INTO report_citations(tenant_id,task_id,ordinal,source_id) VALUES(@tenant,@id,@ordinal,@source)
	InsertReportCitation(tenant, id, source string, ordinal int) error
	// UPDATE @@table SET status='PARTIAL' WHERE tenant_id=@tenant AND id=@id
	SetExecutionPartial(tenant, id string) error

	// UPDATE execution_attempts SET status='ABANDONED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND status='STARTED'
	AbandonTaskAttempts(tenant, id string) error
	// UPDATE research_tasks SET status=@status,phase=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),error_code=NULLIF(@code,'') WHERE tenant_id=@tenant AND id=@id
	SetTaskFinished(tenant, id, status, code string) error
	// UPDATE research_tasks SET status='RETRY_WAIT',phase=NULL,lease_until=NULL,retry_count=retry_count+1,retry_at=UTC_TIMESTAMP(6)+INTERVAL 2 SECOND,error_code='execution_interrupted' WHERE tenant_id=@tenant AND id=@id
	SetRetryWait(tenant, id string) error

	// UPDATE research_tasks SET retry_count=retry_count+1 WHERE tenant_id=@tenant AND id=@id
	IncreaseRetry(tenant, id string) error
	// SELECT retry_at<=UTC_TIMESTAMP(6) FROM research_tasks WHERE tenant_id=@tenant AND id=@id
	RetryDue(tenant, id string) ([]bool, error)
	// SELECT COALESCE(sent_at<=UTC_TIMESTAMP(6)-INTERVAL 60 SECOND,0) FROM task_outbox WHERE tenant_id=@tenant AND task_id=@id AND generation=@generation
	DispatchExpired(tenant, id string, generation uint32) ([]bool, error)
	// UPDATE research_tasks SET status='QUEUED',phase=NULL,generation=@generation,event_seq=@seq,lease_until=NULL WHERE tenant_id=@tenant AND id=@id
	RequeueTask(tenant, id string, generation uint32, seq uint64) error
	// INSERT INTO task_outbox(tenant_id,task_id,generation) VALUES(@tenant,@id,@generation)
	InsertTaskOutbox(tenant, id string, generation uint32) error
	// INSERT INTO task_events(tenant_id,task_id,seq,event_type,payload) VALUES(@tenant,@id,@seq,@kind,@payload)
	InsertTaskEvent(tenant, id string, seq uint64, kind, payload string) error
	// UPDATE research_tasks SET event_seq=@seq WHERE tenant_id=@tenant AND id=@id
	SetEventSequence(tenant, id string, seq uint64) error

	// SELECT status,goal,parent_task_id,COALESCE(creation_logid,'') AS creation_logid,CAST(preferences_snapshot AS CHAR) AS preferences_snapshot,requires_plan_review,review_citations,CASE WHEN plan_approved THEN CAST(review_plan AS CHAR) END AS approved_plan,generation,lease_epoch,event_seq,deadline_at<=UTC_TIMESTAMP(6) AS past_deadline,COALESCE(lease_until>UTC_TIMESTAMP(6),0) AS valid_lease,retry_count,TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),deadline_at)/1000000.0 AS remaining_seconds,COALESCE(model_version,0) AS model_version FROM research_tasks WHERE tenant_id=@tenant AND id=@id FOR UPDATE
	LockExecutionTask(tenant, id string) ([]model.ExecutionTask, error)
	// UPDATE research_tasks SET status='RUNNING',phase='planning',lease_epoch=@epoch,event_seq=@seq,lease_until=UTC_TIMESTAMP(6)+INTERVAL 30 SECOND WHERE tenant_id=@tenant AND id=@id
	ClaimExecutionTask(tenant, id string, epoch uint32, seq uint64) error
	// UPDATE research_tasks SET lease_until=LEAST(deadline_at,UTC_TIMESTAMP(6)+INTERVAL 30 SECOND) WHERE tenant_id=@tenant AND id=@id AND status='RUNNING' AND lease_epoch=@epoch AND lease_until>UTC_TIMESTAMP(6) AND deadline_at>UTC_TIMESTAMP(6) AND (model_version IS NULL OR EXISTS(SELECT 1 FROM model_configs m WHERE m.tenant_id=research_tasks.tenant_id AND m.version=research_tasks.model_version AND m.revoked=false))
	RenewExecutionLease(tenant, id string, epoch uint32) (gen.RowsAffected, error)

	// SELECT s.source_id,s.title,s.canonical_url,s.excerpt,s.source_type FROM report_citations c JOIN research_sources s ON s.tenant_id=c.tenant_id AND s.task_id=c.task_id AND s.source_id=c.source_id WHERE c.tenant_id=@tenant AND c.task_id=@id ORDER BY c.ordinal
	EvaluationSources(tenant, id string) ([]model.Source, error)
	// SELECT COUNT(*) FROM report_citations WHERE tenant_id=@tenant AND task_id=@id
	EvaluationCitationCount(tenant, id string) ([]int, error)
	// SELECT COUNT(*) AS finished_tasks,COALESCE(SUM(t.status='SUCCEEDED'),0) AS succeeded,COALESCE(SUM(t.status='PARTIAL'),0) AS partial,COALESCE(SUM(t.status='FAILED'),0) AS failed,COALESCE(SUM(t.status='CANCELLED'),0) AS cancelled,COUNT(r.task_id) AS reports,COALESCE(SUM(r.task_id IS NOT NULL AND EXISTS(SELECT 1 FROM report_citations c WHERE c.tenant_id=t.tenant_id AND c.task_id=t.id)),0) AS cited_reports,COALESCE(SUM(f.rating='helpful'),0) AS helpful,COALESCE(SUM(f.rating='not_helpful'),0) AS not_helpful FROM research_tasks t LEFT JOIN research_reports r ON r.tenant_id=t.tenant_id AND r.task_id=t.id LEFT JOIN report_feedback f ON f.tenant_id=r.tenant_id AND f.task_id=r.task_id WHERE t.tenant_id=@tenant AND t.model_version IS NOT NULL AND t.status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') AND t.finished_at>=@start AND t.finished_at<@end
	ResearchEvaluationSummary(tenant, start, end string) ([]model.EvaluationSummary, error)
	// SELECT t.id AS task_id,t.goal,t.status,DATE_FORMAT(t.finished_at,'%Y-%m-%dT%H:%i:%sZ') AS finished_at,f.rating FROM research_tasks t LEFT JOIN report_feedback f ON f.tenant_id=t.tenant_id AND f.task_id=t.id WHERE t.tenant_id=@tenant AND t.model_version IS NOT NULL AND t.finished_at>=@start AND t.finished_at<@end AND (t.status IN ('PARTIAL','FAILED') OR (t.status='SUCCEEDED' AND f.rating='not_helpful')) ORDER BY t.finished_at DESC,t.id DESC LIMIT 12
	ResearchEvaluationReviewQueue(tenant, start, end string) ([]model.EvaluationReviewItem, error)
	// SELECT r.kind,(SELECT COUNT(*) FROM research_sources s WHERE s.tenant_id=r.tenant_id AND s.task_id=r.task_id) AS archived_sources FROM research_reports r WHERE r.tenant_id=@tenant AND r.task_id=@id
	ReportEvidenceSummary(tenant, id string) ([]model.ReportEvidence, error)
	// SELECT s.source_id,s.title,s.source_type,c.ordinal FROM report_citations c JOIN research_sources s ON s.tenant_id=c.tenant_id AND s.task_id=c.task_id AND s.source_id=c.source_id WHERE c.tenant_id=@tenant AND c.task_id=@id ORDER BY c.ordinal
	ReportCitationReferences(tenant, id string) ([]model.CitationReference, error)
	// SELECT rating,comment FROM report_feedback WHERE tenant_id=@tenant AND task_id=@id
	ReadReportFeedback(tenant, id string) ([]model.ReportFeedback, error)
	// INSERT INTO report_feedback(tenant_id,task_id,rating,comment) VALUES(@tenant,@id,@rating,@comment) AS incoming ON DUPLICATE KEY UPDATE rating=incoming.rating,comment=incoming.comment
	SaveReportFeedback(tenant, id, rating, comment string) error
	// INSERT INTO research_schedules(tenant_id,id,goal,interval_days,next_run,notify_results) VALUES(@tenant,@id,@goal,@interval,@next,@notify)
	InsertResearchSchedule(tenant, id, goal string, interval int, next string, notify bool) error
	// SELECT id,goal,interval_days,status,DATE_FORMAT(next_run,'%Y-%m-%dT%H:%i:%sZ') AS next_run,last_task_id,notify_results FROM research_schedules WHERE tenant_id=@tenant ORDER BY (status='active') DESC,created_at DESC,id DESC LIMIT 100
	ListResearchSchedules(tenant string) ([]model.ResearchSchedule, error)
	// UPDATE research_schedules SET status='cancelled',next_run=NULL WHERE tenant_id=@tenant AND id=@id AND status='active'
	CancelResearchSchedule(tenant, id string) (gen.RowsAffected, error)
	// SELECT tenant_id,id FROM research_schedules WHERE status='active' AND next_run<=@now ORDER BY next_run,tenant_id,id LIMIT 20
	ListDueResearchSchedules(now string) ([]model.DueSchedule, error)
	// SELECT id,goal,interval_days,status,DATE_FORMAT(next_run,'%Y-%m-%dT%H:%i:%sZ') AS next_run,notify_results FROM research_schedules WHERE tenant_id=@tenant AND id=@id AND status='active' AND next_run<=@now FOR UPDATE
	LockDueResearchSchedule(tenant, id, now string) ([]model.ResearchSchedule, error)
	// UPDATE research_schedules SET status=@status,next_run=NULLIF(@next,''),last_task_id=COALESCE(NULLIF(@taskID,''),last_task_id) WHERE tenant_id=@tenant AND id=@id
	AdvanceResearchSchedule(tenant, id, status, next, taskID string) error
	// UPDATE research_schedules SET notify_results=@notify WHERE tenant_id=@tenant AND id=@id AND status='active' AND NOT (notify_results <=> @notify)
	SetScheduleSubscription(tenant, id string, notify bool) error
	// INSERT INTO research_schedule_results(tenant_id,task_id,schedule_id) VALUES(@tenant,@taskID,@scheduleID)
	RecordScheduleResult(tenant, taskID, scheduleID string) error
	// SELECT t.id AS task_id,t.goal,t.status,DATE_FORMAT(t.finished_at,'%Y-%m-%dT%H:%i:%sZ') AS finished_at,(r.read_at IS NULL) AS unread FROM research_schedule_results r JOIN research_tasks t ON t.tenant_id=r.tenant_id AND t.id=r.task_id WHERE r.tenant_id=@tenant AND t.finished_at IS NOT NULL AND t.status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') ORDER BY unread DESC,t.finished_at DESC,t.id DESC LIMIT 50
	ListScheduleResults(tenant string) ([]model.ScheduleResult, error)
	// UPDATE research_schedule_results r JOIN research_tasks t ON t.tenant_id=r.tenant_id AND t.id=r.task_id SET r.read_at=UTC_TIMESTAMP(6) WHERE r.tenant_id=@tenant AND r.task_id=@taskID AND r.read_at IS NULL AND t.finished_at IS NOT NULL AND t.status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED')
	MarkScheduleResultRead(tenant, taskID string) error

	// SELECT COUNT(*) FROM model_attempts WHERE tenant_id=@tenant AND task_id=@id AND logical_key=@key
	ModelAttemptCount(tenant, id, key string) ([]uint32, error)
	// SELECT DATE_FORMAT(settled_at,'%Y-%m-%d') AS day,SUM(CASE WHEN status='ACTUAL' THEN charged_tokens ELSE 0 END) AS actual_tokens,SUM(CASE WHEN status='ESTIMATED' THEN charged_tokens ELSE 0 END) AS estimated_tokens,COUNT(*) AS attempts,SUM(error_code IS NOT NULL) AS failed_attempts FROM model_attempts WHERE tenant_id=@tenant AND status IN ('ACTUAL','ESTIMATED') AND settled_at>=@start AND settled_at<@end GROUP BY day ORDER BY day
	DailyModelUsage(tenant, start, end string) ([]model.UsageDay, error)
	// SELECT CAST(m.base_url AS BINARY) AS base_url,CAST(m.model AS BINARY) AS model,COALESCE(SUM(a.input_tokens),0) AS input_tokens,COALESCE(SUM(a.output_tokens),0) AS output_tokens,COALESCE(SUM(CASE WHEN a.status='ACTUAL' AND a.input_tokens IS NULL THEN a.charged_tokens ELSE 0 END),0) AS unsplit_tokens,COALESCE(SUM(CASE WHEN a.status='ESTIMATED' THEN a.charged_tokens ELSE 0 END),0) AS estimated_tokens FROM model_attempts a JOIN research_tasks t ON t.tenant_id=a.tenant_id AND t.id=a.task_id JOIN model_configs m ON m.tenant_id=t.tenant_id AND m.version=t.model_version WHERE a.tenant_id=@tenant AND a.settled_at>=@start AND a.settled_at<@end AND a.status IN ('ACTUAL','ESTIMATED') AND m.is_trial=false GROUP BY CAST(m.base_url AS BINARY),CAST(m.model AS BINARY) ORDER BY model,base_url
	UsageByModel(tenant, start, end string) ([]model.ModelUsage, error)
	// UPDATE research_tasks SET tokens_reserved=tokens_reserved+@tokens WHERE tenant_id=@tenant AND id=@id AND tokens_used+tokens_reserved+@tokens<=token_limit
	ReserveTokens(tenant, id string, tokens uint32) (gen.RowsAffected, error)
	// INSERT INTO model_attempts(tenant_id,task_id,attempt_id,lease_epoch,status,reserved_tokens,logical_key,attempt_no) VALUES(@tenant,@id,@attemptID,@epoch,'RESERVED',@tokens,@logical,@attempt)
	InsertModelAttempt(tenant, id, attemptID string, epoch, tokens uint32, logical *string, attempt *uint32) error
	// SELECT reserved_tokens,status FROM model_attempts WHERE tenant_id=@tenant AND task_id=@id AND attempt_id=@attemptID AND lease_epoch=@epoch
	FindModelAttempt(tenant, id, attemptID string, epoch uint32) ([]model.BudgetAttempt, error)
	// UPDATE model_attempts SET status=@status,charged_tokens=@used,input_tokens=@input,output_tokens=@output,error_code=NULLIF(@failure,''),settled_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND attempt_id=@attemptID
	SettleModelAttempt(tenant, id, attemptID, status, failure string, used uint32, input, output *uint32) error
	// UPDATE research_tasks SET tokens_reserved=tokens_reserved-@reserved,tokens_used=tokens_used+@used WHERE tenant_id=@tenant AND id=@id
	ChargeTokens(tenant, id string, reserved, used uint32) error

	// SELECT result_json FROM task_checkpoints WHERE tenant_id=@tenant AND task_id=@id AND step_key=@key
	Checkpoint(tenant, id, key string) ([]string, error)
	// UPDATE execution_attempts SET status='ABANDONED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND step_key=@key AND status='STARTED'
	AbandonStep(tenant, id, key string) error
	// SELECT COALESCE(MAX(attempt_no),0)+1 FROM execution_attempts WHERE tenant_id=@tenant AND task_id=@id AND step_key=@key
	NextStepAttempt(tenant, id, key string) ([]uint32, error)
	// INSERT INTO execution_attempts(tenant_id,task_id,step_key,attempt_no,lease_epoch,status) VALUES(@tenant,@id,@key,@attempt,@epoch,'STARTED')
	StartStep(tenant, id, key string, attempt, epoch uint32) error
	// INSERT INTO task_checkpoints(tenant_id,task_id,step_key,result_json) VALUES(@tenant,@id,@key,@value)
	SaveCheckpoint(tenant, id, key, value string) error
	// UPDATE execution_attempts SET status=@status,error_code=NULLIF(@failure,''),finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND step_key=@key AND attempt_no=@attempt AND lease_epoch=@epoch
	FinishStep(tenant, id, key, status, failure string, attempt, epoch uint32) error

	// SELECT t.tenant_id,t.id AS task_id FROM research_tasks t LEFT JOIN task_outbox o ON o.tenant_id=t.tenant_id AND o.task_id=t.id AND o.generation=t.generation WHERE t.id>@after AND ((t.status IN ('QUEUED','RUNNING') AND t.deadline_at<=UTC_TIMESTAMP(6)) OR (t.status='RUNNING' AND t.lease_until<=UTC_TIMESTAMP(6)) OR (t.status='QUEUED' AND o.sent_at<=UTC_TIMESTAMP(6)-INTERVAL 60 SECOND) OR (t.status='CANCELLING' AND t.lease_until<=UTC_TIMESTAMP(6)) OR (t.status='RETRY_WAIT' AND (t.retry_at<=UTC_TIMESTAMP(6) OR t.deadline_at<=UTC_TIMESTAMP(6)))) ORDER BY t.id LIMIT @limit
	RecoveryCandidates(after string, limit int) ([]model.Delivery, error)
	// SELECT COALESCE(error_code,'model_retry_exhausted') FROM model_attempts WHERE tenant_id=@tenant AND task_id=@id AND logical_key=@key ORDER BY attempt_no DESC LIMIT 1
	PreviousModelFailure(tenant, id, key string) ([]string, error)

	// SELECT tenant_id,id AS task_id FROM @@table WHERE status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') AND finished_at<TIMESTAMPADD(DAY,-@days,UTC_TIMESTAMP(6)) AND min_event_seq<=event_seq LIMIT 100
	FindExpiredEvents(days int) ([]model.Delivery, error)
	// UPDATE @@table SET min_event_seq=event_seq+1 WHERE tenant_id=@tenant AND id=@id
	AdvanceEventFloor(tenant, id string) error
	// DELETE FROM task_events WHERE tenant_id=@tenant AND task_id=@id
	RemoveTaskEvents(tenant, id string) error
	// SELECT request_digest,task_id FROM request_keys WHERE tenant_id=@tenant AND key_digest=UNHEX(@keyHex)
	FindRequestKey(tenant, keyHex string) ([]model.RequestKey, error)
	// INSERT INTO request_keys(tenant_id,key_digest,request_digest,task_id) VALUES (@tenant,UNHEX(@keyHex),UNHEX(@requestHex),@id)
	AddRequestKey(tenant, id, keyHex, requestHex string) error
	// INSERT INTO @@table (id,tenant_id,goal,parent_task_id,creation_logid,requires_plan_review,review_citations,preferences_snapshot,status,deadline_at,event_seq,runtime_version) VALUES (@id,@tenant,@goal,NULLIF(@parentID,''),NULLIF(@creationLogID,''),@reviewPlan,@reviewCitations,CASE WHEN @snapshotPreferences THEN (SELECT preferences FROM research_preferences WHERE tenant_id=@tenant) END,'QUEUED',UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,1,'m2.2')
	AddTask(tenant, id, goal, parentID, creationLogID string, reviewPlan, reviewCitations, snapshotPreferences bool) error
	// SELECT CAST(preferences AS CHAR) FROM research_preferences WHERE tenant_id=@tenant
	ReadResearchPreferences(tenant string) ([]string, error)
	// INSERT INTO research_preferences(tenant_id,preferences) VALUES(@tenant,@preferences) AS incoming ON DUPLICATE KEY UPDATE preferences=incoming.preferences
	SaveResearchPreferences(tenant, preferences string) error
	// DELETE FROM research_preferences WHERE tenant_id=@tenant
	ClearResearchPreferences(tenant string) error
	// UPDATE research_tasks SET status='AWAITING_APPROVAL',phase=NULL,lease_until=NULL,review_plan=@plan WHERE tenant_id=@tenant AND id=@id
	PauseForPlanReview(tenant, id, plan string) error
	// UPDATE research_tasks SET status='QUEUED',review_plan=@plan,plan_approved=true,generation=generation+1,event_seq=@seq,deadline_at=UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE WHERE tenant_id=@tenant AND id=@id
	ApprovePlan(tenant, id, plan string, seq uint64) error
	// SELECT t.status IN ('SUCCEEDED','PARTIAL') AND EXISTS(SELECT 1 FROM research_reports r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id) FROM @@table t WHERE t.tenant_id=@tenant AND t.id=@id
	CanFollowUp(tenant, id string) ([]bool, error)
	// UPDATE @@table SET model_version=@version WHERE tenant_id=@tenant AND id=@id
	SetTaskModel(tenant, id string, version uint32) error
	// INSERT INTO task_outbox(tenant_id,task_id,generation) SELECT tenant_id,id,generation FROM research_tasks WHERE tenant_id=@tenant AND id=@id
	AddOutbox(tenant, id string) error
	// INSERT INTO task_events(tenant_id,task_id,seq,event_type,payload) VALUES (@tenant,@id,@seq,'task_status',@payload)
	AppendStatusEvent(tenant, id string, seq uint64, payload string) error
	// SELECT status,event_seq FROM @@table WHERE tenant_id=@tenant AND id=@id FOR UPDATE
	LockTaskStatus(tenant, id string) ([]model.TaskSnapshot, error)
	// UPDATE @@table SET cancel_requested_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND id=@id
	FlagCancellation(tenant, id string) error
	// UPDATE @@table SET status='CANCELLING',event_seq=@seq WHERE tenant_id=@tenant AND id=@id
	MarkCancelling(tenant, id string, seq uint64) error
	// UPDATE @@table SET status='CANCELLED',event_seq=@seq,phase=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),error_code=NULL WHERE tenant_id=@tenant AND id=@id
	MarkCancelled(tenant, id string, seq uint64) error
	// UPDATE @@table SET tokens_used=tokens_used+tokens_reserved,tokens_reserved=0 WHERE tenant_id=@tenant AND id=@id
	ChargeReservedTokens(tenant, id string) error
	// UPDATE model_attempts SET status='ESTIMATED',charged_tokens=reserved_tokens,settled_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND status='RESERVED'
	SettleReservedAttempts(tenant, id string) error
	// UPDATE execution_attempts SET status='ABANDONED',finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=@tenant AND task_id=@id AND status='STARTED'
	AbandonStartedAttempts(tenant, id string) error
	// SELECT source_id,title,canonical_url,excerpt,source_type FROM research_sources WHERE tenant_id=@tenant AND task_id=@id AND source_id=@source
	FindSource(tenant, id, source string) ([]model.Source, error)

	// SELECT status,event_seq AS latest_seq,min_event_seq AS min_seq FROM @@table WHERE tenant_id=@tenant AND id=@id
	EventPosition(tenant, id string) ([]model.EventPosition, error)
	// SELECT t.status,t.event_seq AS latest_seq,t.min_event_seq AS min_seq,e.seq,e.event_type AS kind,e.payload,DATE_FORMAT(e.created_at,'%Y-%m-%dT%H:%i:%s.%fZ') AS timestamp FROM @@table t LEFT JOIN task_events e ON e.tenant_id=t.tenant_id AND e.task_id=t.id AND e.seq>@after AND (CAST(@after AS DECIMAL(21,0))+1>=t.min_event_seq) WHERE t.tenant_id=@tenant AND t.id=@id ORDER BY e.seq LIMIT 100
	EventBatch(tenant, id string, after uint64) ([]model.EventBatchRow, error)

	// SELECT id,goal,parent_task_id,creation_logid,status,phase,event_seq,error_code,tokens_used,tokens_reserved,token_limit,min_event_seq,CASE WHEN status='AWAITING_APPROVAL' THEN CAST(review_plan AS CHAR) END AS review_plan FROM @@table WHERE tenant_id=@tenant AND id=@id
	FindSnapshot(tenant, id string) ([]model.TaskSnapshot, error)
	// SELECT id,goal,parent_task_id,status,phase,event_seq,error_code,tokens_used,tokens_reserved,token_limit,min_event_seq FROM @@table WHERE tenant_id=@tenant ORDER BY created_at DESC,id DESC LIMIT 51
	ListSnapshots(tenant string) ([]model.TaskSnapshot, error)
	// SELECT t.id,t.goal,t.parent_task_id,t.status,t.phase,t.event_seq,t.error_code,t.tokens_used,t.tokens_reserved,t.token_limit,t.min_event_seq FROM @@table t JOIN @@table anchor ON anchor.tenant_id=t.tenant_id AND anchor.id=@before WHERE t.tenant_id=@tenant AND (t.created_at<anchor.created_at OR (t.created_at=anchor.created_at AND t.id<anchor.id)) ORDER BY t.created_at DESC,t.id DESC LIMIT 51
	ListSnapshotsBefore(tenant, before string) ([]model.TaskSnapshot, error)
	// SELECT id,goal,status,phase,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ') AS created_at FROM @@table WHERE tenant_id=@tenant AND status NOT IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') ORDER BY created_at DESC,id DESC LIMIT 51
	ListOpenTasks(tenant string) ([]model.OverviewTask, error)
	// SELECT id,goal,status,phase,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%sZ') AS created_at,DATE_FORMAT(finished_at,'%Y-%m-%dT%H:%i:%sZ') AS finished_at FROM @@table WHERE tenant_id=@tenant AND status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') ORDER BY finished_at DESC,created_at DESC,id DESC LIMIT 10
	ListRecentlyFinishedTasks(tenant string) ([]model.OverviewTask, error)
	// SELECT a.step_key,a.attempt_no,a.status FROM execution_attempts a WHERE a.tenant_id=@tenant AND a.task_id=@id AND a.attempt_no=(SELECT MAX(b.attempt_no) FROM execution_attempts b WHERE b.tenant_id=a.tenant_id AND b.task_id=a.task_id AND b.step_key=a.step_key) ORDER BY a.step_key
	LatestCalls(tenant, id string) ([]model.CallSnapshot, error)
	// SELECT markdown FROM research_reports WHERE tenant_id=@tenant AND task_id=@id
	ReadReport(tenant, id string) ([]string, error)
}

type modelCommands interface {
	// SELECT p.tenant_id FROM model_profiles p JOIN model_configs m ON m.tenant_id=p.tenant_id AND m.version=p.version WHERE p.is_active=true AND m.revoked=false AND m.is_trial=false
	EvaluationTenants() ([]string, error)

	// SELECT revoked FROM @@table WHERE tenant_id=@tenant AND version=@version
	ModelRevoked(tenant string, version uint32) ([]bool, error)

	// SELECT COALESCE(MAX(m.version),0) FROM model_profiles p JOIN model_configs m ON m.tenant_id=p.tenant_id AND m.version=p.version WHERE p.tenant_id=@tenant AND p.is_active=true AND m.is_trial=false AND m.revoked=false
	ActiveVersion(tenant string) ([]uint32, error)
	// SELECT COALESCE(MAX(version),0)+1 FROM @@table WHERE tenant_id=@tenant
	NextVersion(tenant string) ([]uint32, error)
	// SELECT version,base_url,model,key_cipher,key_version FROM @@table WHERE tenant_id=@tenant AND version=@version AND revoked=false
	FindCredential(tenant string, version uint32) ([]model.Credential, error)
	// SELECT tenant_id,version,key_version,key_cipher FROM @@table ORDER BY tenant_id,version FOR UPDATE
	LockCredentialsForRotation() ([]model.RotationCredential, error)
	// UPDATE @@table SET key_cipher=UNHEX(@cipherHex),key_version=@keyVersion WHERE tenant_id=@tenant AND version=@version
	UpdateCredentialKey(tenant string, version, keyVersion uint32, cipherHex string) error
	// INSERT INTO @@table (tenant_id,version,base_url,model,key_cipher,key_tail,key_version,is_trial) VALUES (@tenant,@version,@base,@name,UNHEX(@cipherHex),@tail,@keyVersion,@trial)
	SaveCredential(tenant string, version uint32, base, name, cipherHex, tail string, keyVersion uint32, trial bool) error
	// INSERT INTO trial_usage(tenant_id,task_id) VALUES (@tenant,@id)
	RecordTrial(tenant, id string) error
	// SELECT COUNT(*) FROM trial_usage WHERE tenant_id=@tenant
	TrialCount(tenant string) ([]int, error)
	// SELECT version,base_url,model,key_tail,revoked FROM @@table WHERE tenant_id=@tenant AND is_trial=false ORDER BY version DESC LIMIT 1
	LatestPersonalModel(tenant string) ([]model.ModelSummary, error)
	// UPDATE @@table SET revoked=true WHERE tenant_id=@tenant AND is_trial=false
	RevokePersonalModels(tenant string) error
	// SELECT p.id,p.label,p.is_active,m.version,m.base_url,m.model,m.key_tail FROM model_profiles p JOIN model_configs m ON m.tenant_id=p.tenant_id AND m.version=p.version WHERE p.tenant_id=@tenant AND m.revoked=false AND m.is_trial=false ORDER BY p.is_active DESC,p.id
	ListModelProfiles(tenant string) ([]model.ModelProfile, error)
	// SELECT p.id,p.label,p.is_active,m.version,m.base_url,m.model,m.key_tail FROM model_profiles p JOIN model_configs m ON m.tenant_id=p.tenant_id AND m.version=p.version WHERE p.tenant_id=@tenant AND p.id=@id AND m.revoked=false AND m.is_trial=false
	FindModelProfile(tenant string, id uint32) ([]model.ModelProfile, error)
	// SELECT p.id,p.label,p.is_active,m.version,m.base_url,m.model,m.key_tail FROM model_profiles p JOIN model_configs m ON m.tenant_id=p.tenant_id AND m.version=p.version WHERE p.tenant_id=@tenant AND p.is_active=true AND m.revoked=false AND m.is_trial=false LIMIT 1
	FindActiveModelProfile(tenant string) ([]model.ModelProfile, error)
	// INSERT INTO model_profiles(tenant_id,id,version,label) VALUES(@tenant,@id,@version,@label) AS incoming ON DUPLICATE KEY UPDATE version=incoming.version,label=incoming.label
	SaveModelProfile(tenant string, id, version uint32, label string) error
	// UPDATE model_profiles SET is_active=(id=@id) WHERE tenant_id=@tenant
	SetActiveModelProfile(tenant string, id uint32) error
}

type outboxCommands interface {
	// SELECT EXISTS(SELECT 1 FROM @@table o JOIN research_tasks t ON t.tenant_id=o.tenant_id AND t.id=o.task_id AND t.generation=o.generation WHERE t.status='QUEUED' AND o.available_at<=TIMESTAMPADD(SECOND,-@seconds,UTC_TIMESTAMP(6)))
	HasDelayedTasks(seconds int) ([]bool, error)
	// SELECT tenant_id,task_id,generation,claim_epoch FROM @@table WHERE sent_at IS NULL AND available_at<=UTC_TIMESTAMP(6) AND (claim_until IS NULL OR claim_until<=UTC_TIMESTAMP(6)) ORDER BY available_at LIMIT 1 FOR UPDATE SKIP LOCKED
	FindNext() ([]model.OutboxClaim, error)
	// UPDATE @@table SET claim_epoch=@epoch,claim_until=UTC_TIMESTAMP(6)+INTERVAL 15 SECOND WHERE tenant_id=@tenant AND task_id=@id AND generation=@generation
	Reserve(tenant, id string, generation, epoch uint32) error
	// UPDATE @@table SET sent_at=UTC_TIMESTAMP(6),claim_until=NULL WHERE tenant_id=@tenant AND task_id=@id AND generation=@generation AND claim_epoch=@epoch
	MarkSent(tenant, id string, generation, epoch uint32) error
}

func main() {
	g := gen.NewGenerator(gen.Config{OutPath: "internal/repository/query", Mode: gen.WithDefaultQuery | gen.WithQueryInterface})
	g.ApplyBasic(model.User{}, model.Session{})
	g.ApplyInterface(func(sessionCommands) {}, model.Session{})
	g.ApplyInterface(func(taskCommands) {}, model.TaskSnapshot{})
	g.ApplyInterface(func(modelCommands) {}, model.ModelSummary{})
	g.ApplyInterface(func(outboxCommands) {}, model.OutboxClaim{})
	g.Execute()
}
