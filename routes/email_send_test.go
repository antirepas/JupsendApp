package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestEmailSend_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origEnqueue := enqueueSendFn
	defer func() {
		enqueueSendFn = origEnqueue
	}()

	enqueueSendFn = func(userID, templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
		return 1, nil
	}

	body := `{
		"template_id": 1,
		"contact_id": 1
	}`

	req, _ := http.NewRequest(http.MethodPost, "/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req
	setTestUser(ctx, 1)

	Email_send(ctx)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "queued")
}

func TestEmailSend_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req, _ := http.NewRequest(http.MethodPost, "/send", bytes.NewBufferString(`invalid-json`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req
	setTestUser(ctx, 1)

	Email_send(ctx)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "could not get request body")
}

func TestEmailSend_EnqueueError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origEnqueue := enqueueSendFn
	defer func() {
		enqueueSendFn = origEnqueue
	}()

	enqueueSendFn = func(userID, templateID, contactID, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
		return 0, assert.AnError
	}

	body := `{"template_id": 1, "contact_id": 1}`
	req, _ := http.NewRequest(http.MethodPost, "/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req
	setTestUser(ctx, 1)

	Email_send(ctx)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
