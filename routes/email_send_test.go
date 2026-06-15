package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"emailtracker.com/config"
	"emailtracker.com/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type MockMailSender struct{}

func (m MockMailSender) Send(to, subject, plainBody, htmlBody string) error {
	return nil
}

func TestEmailSend_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origGetTemplate := getTemplate
	origGetContact := getContact
	origSaveSendEmail := saveSendEmail
	origNewEmailSender := newEmailSender

	defer func() {
		getTemplate = origGetTemplate
		getContact = origGetContact
		saveSendEmail = origSaveSendEmail
		newEmailSender = origNewEmailSender
	}()

	os.Setenv("SMTP_USER", "test@example.com")
	os.Setenv("APP_PASSWORD", "test-password")
	config.Load()
	defer func() {
		os.Unsetenv("SMTP_USER")
		os.Unsetenv("APP_PASSWORD")
	}()

	getTemplate = func(templateId int64) (model.Template, error) {
		return model.Template{
			ID:      templateId,
			Name:    "test",
			Subject: "Hello {{name}}",
			Body:    "Welcome {{name}}",
		}, nil
	}

	getContact = func(contactId int64) (model.Contact, []model.ContactVariables, error) {
		return model.Contact{
			ID:    contactId,
			Email: "test@example.com",
		}, []model.ContactVariables{{Key: "name", Value: "John"}}, nil
	}

	saveSendEmail = func(tId, cId int64, trackId string, campaignID int64, variant string, workflowInstanceID int64) (int64, error) {
		return 1, nil
	}

	newEmailSender = func() MailSender {
		return MockMailSender{}
	}

	body := `{
		"template_id": 1,
		"contact_id": 1
	}`

	req, _ := http.NewRequest(
		http.MethodPost,
		"/send",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req

	Email_send(ctx)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "email sent successfully")
}

func TestEmailSend_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req, _ := http.NewRequest(
		http.MethodPost,
		"/send",
		bytes.NewBufferString(`invalid-json`),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req

	Email_send(ctx)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "could not get request body")
}

func TestEmailSend_TemplateError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origGetTemplate := getTemplate
	defer func() {
		getTemplate = origGetTemplate
	}()

	getTemplate = func(templateId int64) (model.Template, error) {
		return model.Template{}, assert.AnError
	}

	body := `{
		"template_id": 1,
		"contact_id": 1
	}`

	req, _ := http.NewRequest(
		http.MethodPost,
		"/send",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = req

	Email_send(ctx)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
