package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
)

func uploadTestObject(t *testing.T, app *Application, token, kind, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/objects?kind="+kind, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.Router().ServeHTTP(response, req)
	return response
}

func decodeStorageObject(t *testing.T, response *httptest.ResponseRecorder) model.StorageObject {
	t.Helper()
	var envelope struct {
		Data model.StorageObject `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" {
		t.Fatalf("missing object id: %s", response.Body.String())
	}
	return envelope.Data
}

func TestAdminObjectAndDatasetLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "object-dataset-api")
	pair := loginTestUser(t, app, "test@example.com", "test-password-123")

	wav := []byte{'R', 'I', 'F', 'F', 4, 0, 0, 0, 'W', 'A', 'V', 'E'}
	audioResponse := uploadTestObject(t, app, pair.AccessToken, model.ObjectKindAudio, "sample.wav", wav)
	if audioResponse.Code != http.StatusCreated {
		t.Fatalf("upload audio status=%d body=%s", audioResponse.Code, audioResponse.Body.String())
	}
	audio := decodeStorageObject(t, audioResponse)
	textResponse := uploadTestObject(t, app, pair.AccessToken, model.ObjectKindText, "reference.txt", []byte("hello world"))
	if textResponse.Code != http.StatusCreated {
		t.Fatalf("upload text status=%d body=%s", textResponse.Code, textResponse.Body.String())
	}
	text := decodeStorageObject(t, textResponse)

	created := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/datasets",
		[]byte(`{"name":"English ASR baseline","description":"comparison set","language":"en"}`), pair.AccessToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("create dataset status=%d body=%s", created.Code, created.Body.String())
	}
	var datasetEnvelope struct {
		Data model.Dataset `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &datasetEnvelope); err != nil || datasetEnvelope.Data.ID == "" {
		t.Fatalf("decode dataset err=%v body=%s", err, created.Body.String())
	}
	datasetID := datasetEnvelope.Data.ID
	itemBody, _ := json.Marshal(map[string]any{"audio_object_id": audio.ID, "reference_text_object_id": text.ID,
		"reference_segments": []map[string]any{{"text": "hello world", "begin_time_ms": 0, "end_time_ms": 1000}}})
	added := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/datasets/"+datasetID+"/items", itemBody, pair.AccessToken)
	if added.Code != http.StatusCreated {
		t.Fatalf("add item status=%d body=%s", added.Code, added.Body.String())
	}
	duplicate := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/datasets/"+datasetID+"/items", itemBody, pair.AccessToken)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate item status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	deleteReferenced := requestWithToken(app.Router(), http.MethodDelete, "/api/v1/admin/objects/"+audio.ID, nil, pair.AccessToken)
	if deleteReferenced.Code != http.StatusConflict {
		t.Fatalf("delete referenced object status=%d body=%s", deleteReferenced.Code, deleteReferenced.Body.String())
	}
	ready := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/datasets/"+datasetID+"/ready", nil, pair.AccessToken)
	if ready.Code != http.StatusOK {
		t.Fatalf("ready dataset status=%d body=%s", ready.Code, ready.Body.String())
	}
	updated := requestWithToken(app.Router(), http.MethodPatch, "/api/v1/admin/datasets/"+datasetID,
		[]byte(`{"name":"Changed","description":"","language":"en"}`), pair.AccessToken)
	if updated.Code != http.StatusConflict {
		t.Fatalf("update ready dataset status=%d body=%s", updated.Code, updated.Body.String())
	}
	got := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/datasets/"+datasetID, nil, pair.AccessToken)
	if got.Code != http.StatusOK || !bytes.Contains(got.Body.Bytes(), []byte(`"item_count":1`)) ||
		!bytes.Contains(got.Body.Bytes(), []byte(`"reference_segments":[{"text":"hello world","end_time_ms":1000}]`)) {
		t.Fatalf("get dataset status=%d body=%s", got.Code, got.Body.String())
	}
	queued := requestWithToken(app.Router(), http.MethodPost, "/api/v1/admin/evaluation-jobs",
		[]byte(`{"dataset_id":"`+datasetID+`","max_runes":30}`), pair.AccessToken)
	if queued.Code != http.StatusAccepted {
		t.Fatalf("create evaluation job status=%d body=%s", queued.Code, queued.Body.String())
	}
	var jobEnvelope struct {
		Data model.EvaluationJob `json:"data"`
	}
	if err := json.Unmarshal(queued.Body.Bytes(), &jobEnvelope); err != nil || jobEnvelope.Data.ID == "" {
		t.Fatalf("decode evaluation job err=%v body=%s", err, queued.Body.String())
	}
	job := requestWithToken(app.Router(), http.MethodGet, "/api/v1/admin/evaluation-jobs/"+jobEnvelope.Data.ID, nil, pair.AccessToken)
	if job.Code != http.StatusOK || !bytes.Contains(job.Body.Bytes(), []byte(`"dataset_revision":3`)) {
		t.Fatalf("get evaluation job status=%d body=%s", job.Code, job.Body.String())
	}
}

func TestUploadRejectsMismatchedAudioContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := newTestApplication(t, "invalid-object-api")
	pair := loginTestUser(t, app, "test@example.com", "test-password-123")
	response := uploadTestObject(t, app, pair.AccessToken, model.ObjectKindAudio, "fake.wav", []byte("not a wave file"))
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("invalid audio status=%d body=%s", response.Code, response.Body.String())
	}
}
