package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

//go:embed index.html
var webFS embed.FS

type Config struct {
	Model         string
	Project       string
	MaxIterations int
	BratanBinary  string
}

type RunRequest struct {
	Task string `json:"task"`
}

type Server struct {
	cfg Config
	mu  sync.Mutex
}

func main() {
	model := flag.String("model", "qwen3:8b", "Ollama model")
	project := flag.String("project", ".", "Project directory")
	max := flag.Int("max-iterations", 15, "Maximum iterations")
	port := flag.Int("port", 8787, "HTTP port")
	flag.Parse()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine executable:", err)
		os.Exit(1)
	}

	root := filepath.Dir(exe)
	bratan := filepath.Join(root, "bratan")

	if _, err := os.Stat(bratan); err != nil {
		fmt.Fprintln(os.Stderr, "bratan binary not found:", bratan)
		os.Exit(1)
	}

	s := &Server{
		cfg: Config{
			Model:         *model,
			Project:       *project,
			MaxIterations: *max,
			BratanBinary:  bratan,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/run", s.run)

	addr := ":" + strconv.Itoa(*port)

	fmt.Println("[BRATAN WEB] starting")
	fmt.Println("[BRATAN WEB] address: http://127.0.0.1" + addr)
	fmt.Println("[BRATAN WEB] model:", *model)
	fmt.Println("[BRATAN WEB] project:", *project)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "[BRATAN WEB] ERROR:", err)
		os.Exit(1)
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := webFS.ReadFile("index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t := template.Must(template.New("index").Parse(string(data)))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	_ = t.Execute(w, map[string]interface{}{
		"Model":         s.cfg.Model,
		"Project":       s.cfg.Project,
		"MaxIterations": s.cfg.MaxIterations,
	})
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req RunRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Task = string(bytes.TrimSpace([]byte(req.Task)))

	if req.Task == "" {
		http.Error(w, "task is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	args := []string{
		"-project", s.cfg.Project,
		"-model", s.cfg.Model,
		"-max-iterations", strconv.Itoa(s.cfg.MaxIterations),
		req.Task,
	}

	cmd := exec.CommandContext(ctx, s.cfg.BratanBinary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "[BRATAN WEB] task started")
	fmt.Fprintln(w, "[BRATAN WEB] model:", s.cfg.Model)
	fmt.Fprintln(w, "[BRATAN WEB] project:", s.cfg.Project)
	fmt.Fprintln(w)

	flusher.Flush()

	if err := cmd.Start(); err != nil {
		fmt.Fprintln(w, "[BRATAN WEB] ERROR:", err)
		flusher.Flush()
		return
	}

	done := make(chan struct{}, 2)

	stream := func(reader io.Reader) {
		defer func() {
			done <- struct{}{}
		}()

		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)

		for scanner.Scan() {
			fmt.Fprintln(w, scanner.Text())
			flusher.Flush()
		}
	}

	go stream(stdout)
	go stream(stderr)

	<-done
	<-done

	err = cmd.Wait()

	fmt.Fprintln(w)

	if err != nil {
		var exitErr *exec.ExitError
		if ok := isExitError(err, &exitErr); ok {
			fmt.Fprintf(w, "[BRATAN WEB] process exited with code %d\n", exitErr.ExitCode())
		} else if ctx.Err() != nil {
			fmt.Fprintln(w, "[BRATAN WEB] process cancelled")
		} else {
			fmt.Fprintln(w, "[BRATAN WEB] ERROR:", err)
		}
	} else {
		fmt.Fprintln(w, "[BRATAN WEB] DONE")
	}

	flusher.Flush()
}

func isExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}

	ctx := context.Background()
	_ = ctx

	if exitErr, ok := err.(*exec.ExitError); ok {
		*target = exitErr
		return true
	}

	return false
}
