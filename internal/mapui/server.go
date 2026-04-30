package mapui

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Server serves the browser painter and writes generated YAML configs.
type Server struct {
	opts       Options
	inputPath  string
	configPath string
	mux        *http.ServeMux
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
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/video", s.handleVideo)
	mux.HandleFunc("/api/meta", s.handleMeta)
	mux.HandleFunc("/api/config", s.handleConfig)
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
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.ServeFile(w, r, s.inputPath)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, metaResponse{
		Input:         s.opts.Input,
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

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer func() {
		_ = r.Body.Close()
	}()

	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateConfigRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	blocks, err := GroupCells(req.Cells)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(blocks) == 0 {
		http.Error(w, "paint at least one ROI block", http.StatusBadRequest)
		return
	}
	if err := WriteConfig(s.configPath, s.opts, req, blocks); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, configResponse{
		Path:       s.configPath,
		BlockCount: len(req.Cells),
		RectCount:  len(blocks),
		Command:    fmt.Sprintf("go run ./cmd/roi --config %s", shellQuoteIfNeeded(s.opts.ConfigOut)),
	})
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
