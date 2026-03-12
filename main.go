package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

// ── Data types ────────────────────────────────────────────────────────────────

type Game struct {
	Name          string `json:"name"`
	RetroarchPath string `json:"retroarch_path"`
	DeltaName     string `json:"delta_name"`
}

type Config struct {
	Games []Game `json:"games"`
	Port  int    `json:"port"`
}

// ── Config helpers ────────────────────────────────────────────────────────────

var (
	configPath = filepath.Join(os.Getenv("HOME"), ".config", "save-sync-go", "config.json")
	backupDir  = filepath.Join(os.Getenv("HOME"), ".config", "save-sync-go", "backups")
	tmpl       *template.Template
)

func loadConfig() Config {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{Games: []Game{}, Port: 8080}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{Games: []Game{}, Port: 8080}
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	return cfg
}

func writeConfig(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// ── Response helpers ──────────────────────────────────────────────────────────

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// ── Page handlers ─────────────────────────────────────────────────────────────

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "index.html", loadConfig())
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	tmpl.ExecuteTemplate(w, "settings.html", loadConfig())
}

// ── Settings actions ──────────────────────────────────────────────────────────

func handleAddGame(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	cfg := loadConfig()
	cfg.Games = append(cfg.Games, Game{
		Name:          strings.TrimSpace(r.FormValue("name")),
		RetroarchPath: strings.TrimSpace(r.FormValue("retroarch_path")),
		DeltaName:     strings.TrimSpace(r.FormValue("delta_name")),
	})
	writeConfig(cfg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	cfg := loadConfig()
	if id >= len(cfg.Games) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cfg.Games = append(cfg.Games[:id], cfg.Games[id+1:]...)
	writeConfig(cfg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func handleUpdateGame(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	cfg := loadConfig()
	if id >= len(cfg.Games) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	r.ParseForm()
	cfg.Games[id] = Game{
		Name:          strings.TrimSpace(r.FormValue("name")),
		RetroarchPath: strings.TrimSpace(r.FormValue("retroarch_path")),
		DeltaName:     strings.TrimSpace(r.FormValue("delta_name")),
	}
	writeConfig(cfg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// ── Sync actions ──────────────────────────────────────────────────────────────

func handleDownload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	cfg := loadConfig()
	if id >= len(cfg.Games) {
		jsonError(w, "Game not found", http.StatusNotFound)
		return
	}
	game := cfg.Games[id]

	data, err := os.ReadFile(game.RetroarchPath)
	if err != nil {
		jsonError(w, fmt.Sprintf("Save file not found. Check the path in Settings: %s", game.RetroarchPath), http.StatusNotFound)
		return
	}

	stem := strings.TrimSuffix(filepath.Base(game.RetroarchPath), filepath.Ext(game.RetroarchPath))
	name := game.DeltaName
	if name == "" {
		name = stem
	}
	savName := name + ".sav"

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, savName))
	w.Write(data)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 {
		jsonError(w, "bad id", http.StatusBadRequest)
		return
	}
	cfg := loadConfig()
	if id >= len(cfg.Games) {
		jsonError(w, "Game not found", http.StatusNotFound)
		return
	}
	game := cfg.Games[id]

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, "Failed to parse upload", http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		jsonError(w, "Failed to read uploaded file", http.StatusInternalServerError)
		return
	}

	// Back up existing save before overwriting
	if _, statErr := os.Stat(game.RetroarchPath); statErr == nil {
		os.MkdirAll(backupDir, 0755)
		stem := strings.TrimSuffix(filepath.Base(game.RetroarchPath), filepath.Ext(game.RetroarchPath))
		ts := time.Now().Format("20060102_150405")
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s_%s.srm.bak", stem, ts))
		if existing, readErr := os.ReadFile(game.RetroarchPath); readErr == nil {
			os.WriteFile(backupPath, existing, 0644)
		}
	}

	os.MkdirAll(filepath.Dir(game.RetroarchPath), 0755)
	if err := os.WriteFile(game.RetroarchPath, data, 0644); err != nil {
		jsonError(w, "Failed to write save file", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok", "saved_to": game.RetroarchPath})
}

// ── Router ────────────────────────────────────────────────────────────────────

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /settings", handleSettings)
	mux.HandleFunc("POST /settings/add", handleAddGame)
	mux.HandleFunc("POST /settings/delete/{id}", handleDeleteGame)
	mux.HandleFunc("POST /settings/update/{id}", handleUpdateGame)
	mux.HandleFunc("GET /game/{id}/download", handleDownload)
	mux.HandleFunc("POST /game/{id}/upload", handleUpload)
	return mux
}

// ── Main ──────────────────────────────────────────────────────────────────────

func localIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func main() {
	var err error
	tmpl, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load templates: %v\n", err)
		os.Exit(1)
	}

	mux := newMux()

	cfg := loadConfig()
	fmt.Printf("Starting Save Sync (Go)...\n")
	fmt.Printf("Local:   http://localhost:%d\n", cfg.Port)
	fmt.Printf("Network: http://%s:%d\n", localIP(), cfg.Port)
	fmt.Printf("Bookmark the Network address on your iPhone.\n")
	fmt.Printf("Press Ctrl+C to stop.\n\n")

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
