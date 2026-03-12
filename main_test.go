package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fakeSave = bytes.Repeat([]byte{0x00, 0xFF}, 64) // 128 bytes of fake save data

// ── Helpers ───────────────────────────────────────────────────────────────────

// initTest redirects config/backup paths into a temp directory and returns a
// fresh mux. Call this at the start of every test.
func initTest(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	tmp := t.TempDir()
	configPath = filepath.Join(tmp, "config.json")
	backupDir = filepath.Join(tmp, "backups")

	var err error
	tmpl, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	return newMux(), tmp
}

// makeSRMFile writes fakeSave to tmp/TestGame.srm and returns the path.
func makeSRMFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "TestGame.srm")
	if err := os.WriteFile(path, fakeSave, 0644); err != nil {
		t.Fatalf("write srm: %v", err)
	}
	return path
}

// seedConfig writes a config.json with the given games.
func seedConfig(t *testing.T, games []Game) {
	t.Helper()
	cfg := Config{Games: games, Port: 8081}
	data, _ := json.Marshal(cfg)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// do sends a request to mux and returns the response.
func do(mux *http.ServeMux, method, path string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func get(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	return do(mux, http.MethodGet, path, nil, "")
}

func postForm(mux *http.ServeMux, path string, fields map[string]string) *httptest.ResponseRecorder {
	form := make([]string, 0, len(fields))
	for k, v := range fields {
		form = append(form, fmt.Sprintf("%s=%s", k, v))
	}
	body := bytes.NewBufferString(strings.Join(form, "&"))
	return do(mux, http.MethodPost, path, body, "application/x-www-form-urlencoded")
}

func postUpload(mux *http.ServeMux, path string, fileData []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "save.sav")
	fw.Write(fileData)
	w.Close()
	return do(mux, http.MethodPost, path, &buf, w.FormDataContentType())
}

// ── Page routes ───────────────────────────────────────────────────────────────

func TestIndexEmpty(t *testing.T) {
	mux, _ := initTest(t)
	rr := get(mux, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No games added yet") {
		t.Error("expected empty state message")
	}
}

func TestIndexShowsGames(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm}})

	rr := get(mux, "/")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Test Game") {
		t.Error("expected game name in response")
	}
}

func TestSettingsPage(t *testing.T) {
	mux, _ := initTest(t)
	rr := get(mux, "/settings")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Add game") {
		t.Error("expected Add game button")
	}
}

// ── Settings: Add ─────────────────────────────────────────────────────────────

func TestAddGame(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)

	rr := postForm(mux, "/settings/add", map[string]string{
		"name":           "My+Game",
		"retroarch_path": srm,
		"delta_name":     "My+Game+Delta",
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	cfg := loadConfig()
	if len(cfg.Games) != 1 {
		t.Fatalf("expected 1 game, got %d", len(cfg.Games))
	}
	if cfg.Games[0].Name != "My Game" {
		t.Errorf("unexpected name: %s", cfg.Games[0].Name)
	}
	if cfg.Games[0].DeltaName != "My Game Delta" {
		t.Errorf("unexpected delta name: %s", cfg.Games[0].DeltaName)
	}
}

func TestAddGameStripsWhitespace(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)

	postForm(mux, "/settings/add", map[string]string{
		"name":           "Game",
		"retroarch_path": "  " + srm + "  ",
		"delta_name":     "  Delta+Name  ",
	})

	game := loadConfig().Games[0]
	if game.RetroarchPath != srm {
		t.Errorf("path not trimmed: %q", game.RetroarchPath)
	}
	if game.DeltaName != "Delta Name" {
		t.Errorf("delta name not trimmed: %q", game.DeltaName)
	}
}

func TestAddGameWithoutDeltaName(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)

	postForm(mux, "/settings/add", map[string]string{
		"name":           "Game",
		"retroarch_path": srm,
	})

	if loadConfig().Games[0].DeltaName != "" {
		t.Error("expected empty delta name")
	}
}

// ── Settings: Update ──────────────────────────────────────────────────────────

func TestUpdateGame(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Old Name", RetroarchPath: srm, DeltaName: "Old Delta"}})

	rr := postForm(mux, "/settings/update/0", map[string]string{
		"name":           "New+Name",
		"retroarch_path": srm,
		"delta_name":     "New+Delta",
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}

	game := loadConfig().Games[0]
	if game.Name != "New Name" {
		t.Errorf("unexpected name: %s", game.Name)
	}
	if game.DeltaName != "New Delta" {
		t.Errorf("unexpected delta name: %s", game.DeltaName)
	}
}

// ── Settings: Delete ──────────────────────────────────────────────────────────

func TestDeleteGame(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm}})

	rr := postForm(mux, "/settings/delete/0", nil)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}
	if len(loadConfig().Games) != 0 {
		t.Error("expected empty games list")
	}
}

func TestDeleteNonexistentGame(t *testing.T) {
	mux, _ := initTest(t)
	rr := postForm(mux, "/settings/delete/99", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// ── Download ──────────────────────────────────────────────────────────────────

func TestDownloadUsesDeltaName(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm, DeltaName: "Test Game Delta"}})

	rr := get(mux, "/game/0/download")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Disposition"), `filename="Test Game Delta.sav"`) {
		t.Errorf("unexpected disposition: %s", rr.Header().Get("Content-Disposition"))
	}
	if !bytes.Equal(rr.Body.Bytes(), fakeSave) {
		t.Error("response body does not match save data")
	}
}

func TestDownloadFallsBackToSRMStem(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm, DeltaName: ""}})

	rr := get(mux, "/game/0/download")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Disposition"), `filename="TestGame.sav"`) {
		t.Errorf("unexpected disposition: %s", rr.Header().Get("Content-Disposition"))
	}
}

func TestDownloadMissingSaveFile(t *testing.T) {
	mux, _ := initTest(t)
	seedConfig(t, []Game{{Name: "X", RetroarchPath: "/nonexistent/game.srm"}})

	rr := get(mux, "/game/0/download")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if !strings.Contains(body["error"], "Save file not found") {
		t.Errorf("unexpected error: %s", body["error"])
	}
}

func TestDownloadGameNotFound(t *testing.T) {
	mux, _ := initTest(t)
	rr := get(mux, "/game/99/download")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["error"] != "Game not found" {
		t.Errorf("unexpected error: %s", body["error"])
	}
}

// ── Upload ────────────────────────────────────────────────────────────────────

func TestUploadWritesSRM(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm}})

	newSave := bytes.Repeat([]byte{0xAB, 0xCD}, 64)
	rr := postUpload(mux, "/game/0/upload", newSave)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	written, _ := os.ReadFile(srm)
	if !bytes.Equal(written, newSave) {
		t.Error("srm file content does not match uploaded data")
	}
}

func TestUploadCreatesBackup(t *testing.T) {
	mux, tmp := initTest(t)
	srm := makeSRMFile(t, tmp)
	seedConfig(t, []Game{{Name: "Test Game", RetroarchPath: srm}})

	original, _ := os.ReadFile(srm)
	postUpload(mux, "/game/0/upload", bytes.Repeat([]byte{0xAB}, 128))

	backups, _ := filepath.Glob(filepath.Join(backupDir, "*.srm.bak"))
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
	backed, _ := os.ReadFile(backups[0])
	if !bytes.Equal(backed, original) {
		t.Error("backup content does not match original")
	}
}

func TestUploadGameNotFound(t *testing.T) {
	mux, _ := initTest(t)
	rr := postUpload(mux, "/game/99/upload", fakeSave)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestUploadNoBackupWhenSRMMissing(t *testing.T) {
	mux, tmp := initTest(t)
	srmPath := filepath.Join(tmp, "saves", "NewGame.srm")
	seedConfig(t, []Game{{Name: "X", RetroarchPath: srmPath}})

	rr := postUpload(mux, "/game/0/upload", fakeSave)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	written, _ := os.ReadFile(srmPath)
	if !bytes.Equal(written, fakeSave) {
		t.Error("srm file content does not match uploaded data")
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Error("backup dir should not exist when no prior save")
	}
}
