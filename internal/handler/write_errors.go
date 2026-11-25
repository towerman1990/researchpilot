package handler

import (
	"errors"
	"net/http"
	taskservice "researchpilot/internal/task"

	"github.com/cloudwego/hertz/pkg/app"

	"researchpilot/internal/service"
)

var writeErrors = []struct {
	err    error
	status int
}{
	{taskservice.ErrInvalidTask, 422}, {taskservice.ErrIdempotencyConflict, 409}, {service.ErrTrialExhausted, 409},
	{service.ErrGuestTrialExhausted, 429}, {service.ErrTrialUnavailable, 503}, {taskservice.ErrModelRevoked, 409}, {taskservice.ErrTaskNotFound, 404}, {taskservice.ErrTaskFinished, 409}, {taskservice.ErrReportNotReady, 409},
	{taskservice.ErrInvalidPlan, 422}, {taskservice.ErrInvalidPreferences, 422}, {taskservice.ErrInvalidMaterials, 422}, {taskservice.ErrPlanNotAwaiting, 409},
	{taskservice.ErrInvalidSchedule, 422}, {taskservice.ErrScheduleNotFound, 404},
	{service.ErrInvalidModelConfig, 422}, {service.ErrModelHostForbidden, 422}, {service.ErrInvalidAPIKey, 422},
	{service.ErrModelEncryption, 503}, {service.ErrAPIKeyRequired, 422}, {service.ErrModelKeyUnavailable, 409},
	{service.ErrModelProfileNotFound, 404}, {service.ErrModelProfileChanged, 409},
	{service.ErrModelNotConfigured, 409}, {service.ErrModelUnavailable, 503},
}

func WriteError(err error) (int, string) {
	for _, entry := range writeErrors {
		if errors.Is(err, entry.err) {
			return entry.status, entry.err.Error()
		}
	}
	return http.StatusInternalServerError, "internal_error"
}

func writeFailure(c *app.RequestContext, err error) {
	status, code := WriteError(err)
	if status == http.StatusInternalServerError {
		internalError(c, err)
		return
	}
	problem(c, status, code)
}
