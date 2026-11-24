package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ieyoukan/ryudai-tools/internal/config"
	"github.com/ieyoukan/ryudai-tools/internal/middleware"
	"github.com/ieyoukan/ryudai-tools/internal/provider/webclass"
	"github.com/ieyoukan/ryudai-tools/internal/session"
)

type FilesHandler struct {
	Config *config.Config
	Client func(*session.Entry) (*webclass.Client, error)
}

func (h *FilesHandler) webClassClient(entry *session.Entry) (*webclass.Client, error) {
	if h.Client != nil {
		return h.Client(entry)
	}
	return webclass.NewFromCookies(h.Config.WebClassBaseURL, entry.Cookies)
}

func (h *FilesHandler) ListCourses(w http.ResponseWriter, r *http.Request) {
	entry := middleware.GetSession(r.Context())
	if entry == nil {
		writeJSONError(w, http.StatusUnauthorized, "no session found")
		return
	}

	client, err := h.webClassClient(entry)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create scraper client")
		return
	}

	courses, err := client.FetchTimetable()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed to fetch course list: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"courses": courses,
		"count":   len(courses),
	})
}

func (h *FilesHandler) ListCourseContents(w http.ResponseWriter, r *http.Request) {
	courseID := chi.URLParam(r, "courseID")
	if courseID == "" {
		writeJSONError(w, http.StatusBadRequest, "courseID is required")
		return
	}

	entry := middleware.GetSession(r.Context())
	if entry == nil {
		writeJSONError(w, http.StatusUnauthorized, "no session found")
		return
	}

	client, err := h.webClassClient(entry)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create scraper client")
		return
	}

	contents, err := client.FetchCourseContents(courseID)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed to fetch course contents: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"course_id": courseID,
		"contents":  contents,
		"count":     len(contents),
	})
}

func (h *FilesHandler) ListContentFiles(w http.ResponseWriter, r *http.Request) {
	courseID := chi.URLParam(r, "courseID")
	contentID := chi.URLParam(r, "contentID")
	if courseID == "" || contentID == "" {
		writeJSONError(w, http.StatusBadRequest, "courseID and contentID are required")
		return
	}

	entry := middleware.GetSession(r.Context())
	if entry == nil {
		writeJSONError(w, http.StatusUnauthorized, "no session found")
		return
	}

	client, err := h.webClassClient(entry)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create scraper client")
		return
	}

	files, err := client.FetchContentFiles(courseID, contentID)
	if errors.Is(err, webclass.ErrNotMaterial) {
		writeJSONError(w, http.StatusForbidden, "only course materials are available; tests, reports and surveys are not")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed to fetch content files: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"content_id": contentID,
		"files":      files,
		"count":      len(files),
	})
}

func (h *FilesHandler) Download(w http.ResponseWriter, r *http.Request) {
	courseID := chi.URLParam(r, "courseID")
	fileID := chi.URLParam(r, "fileID")

	if courseID == "" || fileID == "" {
		writeJSONError(w, http.StatusBadRequest, "courseID and fileID are required")
		return
	}

	entry := middleware.GetSession(r.Context())
	if entry == nil {
		writeJSONError(w, http.StatusUnauthorized, "no session found")
		return
	}

	client, err := h.webClassClient(entry)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create scraper client")
		return
	}

	filePath := r.URL.Query().Get("path")
	var downloadPath string
	if filePath != "" {
		downloadPath = strings.TrimLeft(filePath, "/")
	} else {
		downloadPath = fmt.Sprintf("do_contents.php?reset_status=1&set_contents_id=%s", fileID)
	}

	resp, err := client.DownloadFile(downloadPath)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed to download file from WebClass")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeJSONError(w, resp.StatusCode, fmt.Sprintf("WebClass returned status %d", resp.StatusCode))
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		w.Header().Set("Content-Disposition", cd)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	w.WriteHeader(http.StatusOK)
	io.Copy(w, resp.Body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":"%s"}`, msg)
}
