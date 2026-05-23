package mapui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Manabreaker/ROI_based_video_encoding/internal/roi"
)

const finalResultName = "roi_high_quality_region.mp4"

// Server serves the browser painter and writes generated YAML configs.
type Server struct {
	opts          Options
	inputPath     string
	configPath    string
	mux           *http.ServeMux
	runEncoder    func(roi.Config) error
	runMu         sync.Mutex
	resultMu      sync.RWMutex
	lastResultDir string
}

// NewServer creates a configured local UI server.
func NewServer(opts Options) (*Server, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}

	inputPath, err := filepath.Abs(opts.Input)
	if err != nil {
		return nil, err
	}
	configPath, err := filepath.Abs(opts.ConfigOut)
	if err != nil {
		return nil, err
	}

	s := &Server{
		opts:       opts,
		inputPath:  inputPath,
		configPath: configPath,
		runEncoder: roi.Run,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/video", s.handleVideo)
	mux.HandleFunc("/result/", s.handleResult)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/run", s.handleRun)
	s.mux = mux

	return s, nil
}

// Handler returns the HTTP handler for tests or custom hosting.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Run starts the localhost server and blocks.
func Run(opts Options) error {
	s, err := NewServer(opts)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return err
	}
	defer func() {
		_ = ln.Close()
	}()

	url := "http://" + ln.Addr().String() + "/"
	fmt.Printf("ROI map UI: %s\n", url)
	fmt.Printf("Input: %s\n", opts.Input)
	fmt.Printf("Config output: %s\n", opts.ConfigOut)
	if opts.OpenBrowser {
		if err := openBrowser(url); err != nil {
			fmt.Printf("Could not open browser automatically: %v\n", err)
		}
	}

	return http.Serve(ln, s.Handler())
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	disableBrowserCache(w)
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	disableBrowserCache(w)
	http.ServeFile(w, r, s.inputPath)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	disableBrowserCache(w)
	writeJSON(w, metaResponse{
		Input:         s.opts.Input,
		VideoURL:      s.videoURL(),
		ConfigOut:     s.opts.ConfigOut,
		OutDir:        s.opts.OutDir,
		TargetBitrate: s.opts.TargetBitrate,
		Encoder:       s.opts.Encoder,
		Preset:        s.opts.Preset,
		NVENCPreset:   s.opts.NVENCPreset,
		BitrateWindow: s.opts.BitrateWindow,
		ROIBlockSize:  s.opts.ROIBlockSize,
		Palette:       Palette,
	})
}

func (s *Server) videoURL() string {
	return "/video?v=" + url.QueryEscape(videoCacheKey(s.inputPath))
}

func videoCacheKey(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return path
	}
	return fmt.Sprintf("%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
}

func disableBrowserCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, blocks, err := s.writePostedConfig(w, r, s.configPath)
	if err != nil {
		return
	}

	writeJSON(w, configResponse{
		Path:       s.configPath,
		BlockCount: len(req.Cells),
		RectCount:  len(blocks),
		Command:    fmt.Sprintf("go run ./cmd/roi --config %s", shellQuoteIfNeeded(s.opts.ConfigOut)),
	})
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.runMu.Lock()
	defer s.runMu.Unlock()

	req, blocks, err := s.writePostedConfig(w, r, s.configPath)
	if err != nil {
		return
	}

	runConfigPath, err := s.writeOutputConfigCopy(req, blocks)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cfg, err := roi.ParseArgs([]string{"--config", runConfigPath})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg.Serve = false

	if err := s.runEncoder(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	outDirAbs, err := filepath.Abs(cfg.OutDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resultPath := filepath.Join(outDirAbs, finalResultName)
	if _, err := os.Stat(resultPath); err != nil {
		http.Error(w, fmt.Sprintf("processing finished but final result %q is not available: %v", resultPath, err), http.StatusInternalServerError)
		return
	}

	resultID := time.Now().UnixNano()
	s.resultMu.Lock()
	s.lastResultDir = outDirAbs
	s.resultMu.Unlock()

	writeJSON(w, runResponse{
		ConfigPath: runConfigPath,
		OutputDir:  outDirAbs,
		ResultPath: resultPath,
		ResultURL:  fmt.Sprintf("/result/%s?v=%d", finalResultName, resultID),
		BlockCount: len(req.Cells),
		RectCount:  len(blocks),
	})
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.resultMu.RLock()
	dir := s.lastResultDir
	s.resultMu.RUnlock()
	if dir == "" {
		http.NotFound(w, r)
		return
	}

	http.StripPrefix("/result/", http.FileServer(http.Dir(dir))).ServeHTTP(w, r)
}

func (s *Server) writePostedConfig(w http.ResponseWriter, r *http.Request, path string) (ConfigRequest, []roi.QPMapBlock, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer func() {
		_ = r.Body.Close()
	}()

	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return ConfigRequest{}, nil, err
	}
	if err := validateConfigRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return ConfigRequest{}, nil, err
	}

	blocks, err := GroupCells(req.Cells)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return ConfigRequest{}, nil, err
	}
	if len(blocks) == 0 {
		err := fmt.Errorf("paint at least one ROI block")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return ConfigRequest{}, nil, err
	}
	if err := WriteConfig(path, s.opts, req, blocks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return ConfigRequest{}, nil, err
	}

	return req, blocks, nil
}

func (s *Server) writeOutputConfigCopy(req ConfigRequest, blocks []roi.QPMapBlock) (string, error) {
	outDir, err := filepath.Abs(strings.TrimSpace(req.OutDir))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	path := filepath.Join(outDir, "roi_blocks_config.yaml")
	if err := WriteConfig(path, s.opts, req, blocks); err != nil {
		return "", err
	}
	return path, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func shellQuoteIfNeeded(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\"'\\") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
