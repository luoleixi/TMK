package server

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tmk-glance/internal/language"
	"tmk-glance/internal/model"
	"tmk-glance/internal/objectstore"
	"tmk-glance/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const multipartOverheadAllowance = int64(2 << 20)

func (a *Application) handleUploadObject(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	maxBytes := a.maxObjectBytes(kind)
	if maxBytes == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "kind must be audio or text"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+multipartOverheadAllowance)
	if err := c.Request.ParseMultipartForm(8 << 20); err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "upload exceeds the configured size limit"})
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "multipart field 'file' is required"})
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "file exceeds the configured size limit"})
		return
	}
	originalName, err := normalizeUploadName(header.Filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	contentType, err := validateUploadedContent(kind, originalName, file)
	if err != nil {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"code": 415, "message": err.Error()})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "uploaded file is not seekable"})
		return
	}

	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	user := currentUser(c)
	ownerBytes, totalBytes, err := a.store.StorageUsage(user.ID)
	if err != nil {
		log.Printf("[db] load object storage usage failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "upload failed"})
		return
	}
	if ownerBytes+header.Size > a.perUserObjectQuota() || totalBytes+header.Size > a.totalObjectQuota() {
		c.JSON(http.StatusInsufficientStorage, gin.H{"code": 507, "message": "object storage quota exceeded"})
		return
	}
	freeBytes, err := a.objectStore.FreeBytes()
	if err != nil {
		log.Printf("[storage] inspect free space failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "upload failed"})
		return
	}
	if uint64(header.Size+a.minObjectFreeBytes()) > freeBytes {
		c.JSON(http.StatusInsufficientStorage, gin.H{"code": 507, "message": "server disk free-space reserve reached"})
		return
	}

	objectID := uuid.NewString()
	now := time.Now().UTC()
	extension := strings.ToLower(filepath.Ext(originalName))
	storageKey := fmt.Sprintf("%04d/%02d/%s%s", now.Year(), int(now.Month()), objectID, extension)
	size, digest, err := a.objectStore.Put(c.Request.Context(), storageKey, file, maxBytes)
	if err != nil {
		status := http.StatusInternalServerError
		message := "upload failed"
		if errors.Is(err, objectstore.ErrTooLarge) {
			status, message = http.StatusRequestEntityTooLarge, "file exceeds the configured size limit"
		}
		log.Printf("[storage] put object failed: %v", err)
		c.JSON(status, gin.H{"code": status, "message": message})
		return
	}
	object := &model.StorageObject{ID: objectID, OwnerUserID: user.ID, Kind: kind,
		OriginalName: originalName, StorageKey: storageKey, ContentType: contentType,
		SizeBytes: size, SHA256: digest, Status: model.ObjectStatusReady, CreatedAt: now}
	if err := a.store.CreateStorageObject(object); err != nil {
		_ = a.objectStore.Delete(storageKey)
		log.Printf("[db] save storage object failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "upload failed"})
		return
	}
	a.audit(c, user.ID, "storage.object.upload", "storage_object", object.ID, "success", gin.H{"kind": kind, "size_bytes": size, "sha256": digest})
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": object})
}

func (a *Application) handleListObjects(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind != "" && kind != model.ObjectKindAudio && kind != model.ObjectKindText {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid object kind"})
		return
	}
	offset, limit := pagination(c, 50)
	objects, total, err := a.store.ListStorageObjects(kind, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list objects failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"objects": objects, "total": total, "offset": offset, "limit": limit}})
}

func (a *Application) handleStorageUsage(c *gin.Context) {
	user := currentUser(c)
	ownerBytes, totalBytes, err := a.store.StorageUsage(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load storage usage failed"})
		return
	}
	freeBytes, err := a.objectStore.FreeBytes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "load storage usage failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{
		"current_user_bytes": ownerBytes, "total_bytes": totalBytes, "disk_free_bytes": freeBytes,
		"per_user_quota_bytes": a.perUserObjectQuota(), "total_quota_bytes": a.totalObjectQuota(),
		"min_free_bytes": a.minObjectFreeBytes(),
	}})
}

func (a *Application) handleGetObject(c *gin.Context) {
	object, ok, err := a.store.GetStorageObject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get object failed"})
		return
	}
	if !ok || object.Status != model.ObjectStatusReady {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "object not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": object})
}

func (a *Application) handleDownloadObject(c *gin.Context) {
	object, ok, err := a.store.GetStorageObject(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "download object failed"})
		return
	}
	if !ok || object.Status != model.ObjectStatusReady {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "object not found"})
		return
	}
	file, err := a.objectStore.Open(object.StorageKey)
	if err != nil {
		log.Printf("[storage] open object failed, id=%s: %v", object.ID, err)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "object content not found"})
		return
	}
	defer file.Close()
	c.Header("Content-Type", object.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": object.OriginalName}))
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, object.OriginalName, object.CreatedAt, file)
}

func (a *Application) handleDeleteObject(c *gin.Context) {
	object, ok, err := a.store.BeginDeleteStorageObject(c.Param("id"))
	if errors.Is(err, store.ErrObjectInUse) {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "object is referenced by a dataset"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete object failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "object not found"})
		return
	}
	if err := a.objectStore.Delete(object.StorageKey); err != nil {
		_ = a.store.RestoreStorageObject(object.ID)
		log.Printf("[storage] delete object failed, id=%s: %v", object.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete object failed"})
		return
	}
	if err := a.store.DeleteStorageObjectRecord(object.ID); err != nil {
		log.Printf("[db] delete object record failed, id=%s: %v", object.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete object metadata failed"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "storage.object.delete", "storage_object", object.ID, "success", gin.H{"sha256": object.SHA256})
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleCreateDataset(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Language    string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validDatasetInput(req.Name, req.Description, req.Language) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "valid name, description and language are required"})
		return
	}
	now := time.Now().UTC()
	user := currentUser(c)
	dataset := &model.Dataset{ID: uuid.NewString(), Name: strings.TrimSpace(req.Name), Description: strings.TrimSpace(req.Description),
		Language: req.Language, Status: model.DatasetStatusDraft, Revision: 1, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if err := a.store.CreateDataset(dataset); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "create dataset failed"})
		return
	}
	a.audit(c, user.ID, "dataset.create", "dataset", dataset.ID, "success", gin.H{"language": dataset.Language})
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": dataset})
}

func (a *Application) handleListDatasets(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != model.DatasetStatusDraft && status != model.DatasetStatusReady && status != model.DatasetStatusArchived {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid dataset status"})
		return
	}
	offset, limit := pagination(c, 50)
	datasets, total, err := a.store.ListDatasets(status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "list datasets failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"datasets": datasets, "total": total, "offset": offset, "limit": limit}})
}

func (a *Application) handleGetDataset(c *gin.Context) {
	dataset, ok, err := a.store.GetDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get dataset failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		return
	}
	items, err := a.store.ListDatasetItems(dataset.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get dataset failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"dataset": dataset, "items": items}})
}

func (a *Application) handleUpdateDataset(c *gin.Context) {
	dataset, ok, err := a.store.GetDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "get dataset failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		return
	}
	if dataset.Status != model.DatasetStatusDraft {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "ready or archived datasets are immutable"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Language    string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !validDatasetInput(req.Name, req.Description, req.Language) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "valid name, description and language are required"})
		return
	}
	updated, err := a.store.UpdateDatasetDraft(dataset.ID, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.Language)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "update dataset failed"})
		return
	}
	if !updated {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "dataset changed while being updated"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "dataset.update", "dataset", dataset.ID, "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleAddDatasetItem(c *gin.Context) {
	var req struct {
		AudioObjectID         string                   `json:"audio_object_id"`
		ReferenceTextObjectID string                   `json:"reference_text_object_id"`
		Notes                 string                   `json:"notes"`
		ReferenceSegments     []model.ReferenceSegment `json:"reference_segments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.AudioObjectID == "" || req.ReferenceTextObjectID == "" || len(req.Notes) > 2000 || !validReferenceSegments(req.ReferenceSegments) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "audio_object_id and reference_text_object_id are required"})
		return
	}
	for i := range req.ReferenceSegments {
		req.ReferenceSegments[i].Text = strings.TrimSpace(req.ReferenceSegments[i].Text)
	}
	user := currentUser(c)
	item := &model.DatasetItem{ID: uuid.NewString(), DatasetID: c.Param("id"), AudioObjectID: req.AudioObjectID,
		ReferenceTextObjectID: req.ReferenceTextObjectID, ReferenceSegments: req.ReferenceSegments,
		Notes: strings.TrimSpace(req.Notes), CreatedBy: user.ID, CreatedAt: time.Now().UTC()}
	if err := a.store.AddDatasetItem(item); err != nil {
		switch {
		case errors.Is(err, store.ErrDatasetImmutable):
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "ready or archived datasets are immutable"})
		case errors.Is(err, store.ErrDuplicateAudio):
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "audio object already exists in dataset"})
		case errors.Is(err, store.ErrInvalidObjectPair):
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "item requires a ready audio object and a ready text object"})
		case errors.Is(err, sql.ErrNoRows):
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		default:
			log.Printf("[db] add dataset item failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "add dataset item failed"})
		}
		return
	}
	a.audit(c, user.ID, "dataset.item.add", "dataset", item.DatasetID, "success", gin.H{
		"item_id": item.ID, "reference_segment_count": len(item.ReferenceSegments),
	})
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": item})
}

func validReferenceSegments(values []model.ReferenceSegment) bool {
	if len(values) > 2000 {
		return false
	}
	totalRunes := 0
	var previousEnd int64
	for i, value := range values {
		text := strings.TrimSpace(value.Text)
		totalRunes += utf8.RuneCountInString(text)
		if text == "" || totalRunes > 1_000_000 || value.BeginTimeMS < 0 || value.EndTimeMS < 0 {
			return false
		}
		if value.EndTimeMS > 0 && value.EndTimeMS < value.BeginTimeMS {
			return false
		}
		if i > 0 && value.BeginTimeMS > 0 && previousEnd > 0 && value.BeginTimeMS < previousEnd {
			return false
		}
		if value.EndTimeMS > 0 {
			previousEnd = value.EndTimeMS
		}
	}
	return true
}

func (a *Application) handleDeleteDatasetItem(c *gin.Context) {
	deleted, err := a.store.DeleteDatasetItem(c.Param("id"), c.Param("item_id"))
	if errors.Is(err, store.ErrDatasetImmutable) {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "ready or archived datasets are immutable"})
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete dataset item failed"})
		return
	}
	if errors.Is(err, sql.ErrNoRows) || !deleted {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset item not found"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "dataset.item.delete", "dataset", c.Param("id"), "success", gin.H{"item_id": c.Param("item_id")})
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleMarkDatasetReady(c *gin.Context) {
	updated, err := a.store.MarkDatasetReady(c.Param("id"))
	switch {
	case errors.Is(err, store.ErrDatasetEmpty):
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "dataset must contain at least one item"})
		return
	case errors.Is(err, store.ErrDatasetImmutable):
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "dataset is not a draft"})
		return
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "finalize dataset failed"})
		return
	case !updated:
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "dataset changed while being finalized"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "dataset.ready", "dataset", c.Param("id"), "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleArchiveDataset(c *gin.Context) {
	dataset, ok, err := a.store.GetDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "archive dataset failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		return
	}
	if dataset.Status != model.DatasetStatusReady {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only ready datasets can be archived"})
		return
	}
	updated, err := a.store.ArchiveDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "archive dataset failed"})
		return
	}
	if !updated {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only ready datasets can be archived"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "dataset.archive", "dataset", c.Param("id"), "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func (a *Application) handleDeleteDataset(c *gin.Context) {
	dataset, ok, err := a.store.GetDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete dataset failed"})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "dataset not found"})
		return
	}
	if dataset.Status != model.DatasetStatusDraft {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only draft datasets can be deleted"})
		return
	}
	deleted, err := a.store.DeleteDraftDataset(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "delete dataset failed"})
		return
	}
	if !deleted {
		c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "only draft datasets can be deleted"})
		return
	}
	user := currentUser(c)
	a.audit(c, user.ID, "dataset.delete", "dataset", c.Param("id"), "success", nil)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

func validateUploadedContent(kind, name string, file io.ReadSeeker) (string, error) {
	extension := strings.ToLower(filepath.Ext(name))
	if kind == model.ObjectKindText {
		allowed := map[string]bool{".txt": true, ".md": true, ".json": true, ".srt": true, ".vtt": true}
		if !allowed[extension] {
			return "", errors.New("text files must be .txt, .md, .json, .srt or .vtt")
		}
		content, err := io.ReadAll(file)
		if err != nil {
			return "", err
		}
		if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
			return "", errors.New("text file must contain valid UTF-8 without NUL bytes")
		}
		return "text/plain; charset=utf-8", nil
	}
	header := make([]byte, 16)
	read, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	header = header[:read]
	valid := false
	contentType := ""
	switch extension {
	case ".wav":
		valid = len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE"
		contentType = "audio/wav"
	case ".mp3":
		valid = len(header) >= 3 && string(header[:3]) == "ID3" || len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0
		contentType = "audio/mpeg"
	case ".flac":
		valid = len(header) >= 4 && string(header[:4]) == "fLaC"
		contentType = "audio/flac"
	case ".ogg":
		valid = len(header) >= 4 && string(header[:4]) == "OggS"
		contentType = "audio/ogg"
	case ".m4a":
		valid = len(header) >= 8 && string(header[4:8]) == "ftyp"
		contentType = "audio/mp4"
	case ".webm":
		valid = len(header) >= 4 && header[0] == 0x1a && header[1] == 0x45 && header[2] == 0xdf && header[3] == 0xa3
		contentType = "audio/webm"
	default:
		return "", errors.New("audio files must be .wav, .mp3, .flac, .ogg, .m4a or .webm")
	}
	if !valid {
		return "", errors.New("audio content does not match its file extension")
	}
	return contentType, nil
}

func normalizeUploadName(value string) (string, error) {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	if value == "" || value == "." || len(value) > 240 || strings.ContainsRune(value, 0) {
		return "", errors.New("invalid upload filename")
	}
	return value, nil
}

func validDatasetInput(name, description, languageCode string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= 160 && len(description) <= 4000 && language.IsValid(languageCode)
}

func pagination(c *gin.Context, defaultLimit int) (int, int) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = defaultLimit
	}
	return offset, limit
}

func (a *Application) maxObjectBytes(kind string) int64 {
	if kind == model.ObjectKindAudio {
		if a.cfg.ObjectStorage.MaxAudioBytes > 0 {
			return a.cfg.ObjectStorage.MaxAudioBytes
		}
		return 500 << 20
	}
	if kind == model.ObjectKindText {
		if a.cfg.ObjectStorage.MaxTextBytes > 0 {
			return a.cfg.ObjectStorage.MaxTextBytes
		}
		return 10 << 20
	}
	return 0
}

func (a *Application) perUserObjectQuota() int64 {
	if a.cfg.ObjectStorage.PerUserQuotaBytes > 0 {
		return a.cfg.ObjectStorage.PerUserQuotaBytes
	}
	return 5 << 30
}

func (a *Application) totalObjectQuota() int64 {
	if a.cfg.ObjectStorage.TotalQuotaBytes > 0 {
		return a.cfg.ObjectStorage.TotalQuotaBytes
	}
	return 20 << 30
}

func (a *Application) minObjectFreeBytes() int64 {
	if a.cfg.ObjectStorage.MinFreeBytes > 0 {
		return a.cfg.ObjectStorage.MinFreeBytes
	}
	return 2 << 30
}
