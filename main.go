package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/justinas/alice"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	yaml "gopkg.in/yaml.v2"
)

type Param struct {
	Dir      string   `json:"dir,omitempty"`
	Stdin    string   `json:"stdin,omitempty"`
	Args     []string `json:"args,omitempty"`
	Envs     []string `json:"envs,omitempty"`
	Callback string   `json:"callback,omitempty"`
}

type Cmd struct {
	Command string   `yaml:"command"`
	Dir     string   `yaml:"dir,omitempty"`
	Allow   []string `yaml:"allow,omitempty,flow"`
	Stdin   string   `yaml:"stdin,omitempty"`
	Args    []string `yaml:"args,omitempty,flow"`
	Envs    []string `yaml:"envs,omitempty,flow"`
}

func (c Cmd) isAllowed(key string) bool {
	for _, v := range c.Allow {
		if v == key {
			return true
		}
	}
	return false
}

type Result struct {
	RequestID string `json:"request_id"`
	Output    string `json:"output"`
	Error     string `json:"error"`
	Status    int    `json:"status"`
}

type Callback struct {
	RequestID string `json:"request_id"`
	URL       string `json:"url"`
}

var (
	configFile = flag.String("c", "./config.yml", "")
	logTo      = flag.String("l", "stdout", "")
	port       = flag.Int("p", 1307, "")
)

var usage = `Usage: cas [options...]

Options:
  -c  Config file. Default to ./config.yml
  -l  Write log to either stdout, stderr, or file. Default to stdout.
  -p  Port to listen. Defalut to 1307.
`

var logger zerolog.Logger

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
	}
	flag.Parse()

	switch *logTo {
	case "stdout":
		logger = zerolog.New(os.Stdout)
	case "stderr":
		logger = zerolog.New(os.Stderr)
	case "":
		logger = zerolog.New(ioutil.Discard)
	default:
		// TODO: log to file.
	}

	logger.With().Timestamp()

	content, err := ioutil.ReadFile(*configFile)
	if err != nil {
		fail(fmt.Sprintf("Failed to read config file: %s", err))
	}

	var config map[string]Cmd
	if err = yaml.Unmarshal(content, &config); err != nil {
		fail(fmt.Sprintf("Failed to parse config: %s", err))
	}

	http.Handle("/", handlerFunc(config))
	http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, msg+"\n")
	os.Exit(1)
}

func handlerFunc(config map[string]Cmd) http.Handler {
	// Middleware.
	m := alice.New()
	m = m.Append(setConfig(config))
	m = m.Append(hlog.NewHandler(logger))
	m = m.Append(hlog.RequestIDHandler("request_id", "Request-Id"))
	m = m.Append(hlog.RequestHandler("request"))
	m = m.Append(hlog.RemoteAddrHandler("ip"))
	m = m.Append(hlog.UserAgentHandler("user_agent"))

	return m.Then(http.HandlerFunc(handle))
}

// setConfig is a middleware that sets config in request's context.
func setConfig(config map[string]Cmd) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), "config", config)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	if _, ok := requestConfig(w, r); !ok {
		return
	}

	if !requestMethodAllowed(w, r) {
		return
	}

	c, ok := requestCmd(w, r)
	if !ok {
		return
	}

	p, ok := requestParam(w, r, c)
	if !ok {
		return
	}

	c.Args = append(c.Args, p.Args...)
	c.Envs = append(c.Envs, p.Envs...)
	if p.Stdin != "" {
		c.Stdin = p.Stdin
	}
	if p.Dir != "" {
		c.Dir = p.Dir
	}

	if callback(w, r, c, p) {
		return
	}

	result := Result{
		RequestID: requestId(r),
	}
	output, err := run(c)
	if err != nil {
		result.Error = err.Error()
	}
	result.Output = string(output)
	result.Status = http.StatusOK

	jsonResult(w, r, result)
}

func handleError(w http.ResponseWriter, r *http.Request, err error, status int) {
	result := Result{
		RequestID: requestId(r),
		Error:     err.Error(),
		Status:    status,
	}
	jsonResult(w, r, result)
}

func jsonResult(w http.ResponseWriter, r *http.Request, result Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.Status)

	json.NewEncoder(w).Encode(result)

	logResult(r, result)
}

func requestConfig(w http.ResponseWriter, r *http.Request) (config map[string]Cmd, ok bool) {
	if c := r.Context().Value("config"); c != nil {
		config, ok = c.(map[string]Cmd)
	}
	if !ok {
		handleError(w, r, errors.New("config not found in request context"), http.StatusInternalServerError)
	}

	return config, ok
}

func requestMethodAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == "POST" {
		return true
	}
	w.Header().Set("Allow", "POST")
	handleError(w, r, errors.New("invalid request method"), http.StatusMethodNotAllowed)

	return false
}

func requestCmd(w http.ResponseWriter, r *http.Request) (c Cmd, ok bool) {
	config := r.Context().Value("config").(map[string]Cmd)

	c, ok = config[r.URL.Path]
	if !ok {
		handleError(w, r, errors.New("handler not found"), http.StatusNotFound)
	}
	return c, ok
}

func requestParam(w http.ResponseWriter, r *http.Request, c Cmd) (p Param, ok bool) {
	err := json.NewDecoder(r.Body).Decode(&p)

	// Empty body.
	if err == io.EOF {
		return p, true
	} else if err != nil {
		handleError(w, r, err, http.StatusBadRequest)
		return
	}

	if err = checkRequestParam(r, c, p); err != nil {
		handleError(w, r, err, http.StatusForbidden)
	} else {
		ok = true
	}

	return p, ok
}

func checkRequestParam(r *http.Request, c Cmd, p Param) error {
	if !c.isAllowed("args") && len(p.Args) > 0 {
		return errors.New("args param is not allowed")
	}
	if !c.isAllowed("envs") && len(p.Envs) > 0 {
		return errors.New("envs param is not allowed")
	}
	if !c.isAllowed("stdin") && p.Stdin != "" {
		return errors.New("stdin param is not allowed")
	}
	if !c.isAllowed("dir") && p.Dir != "" {
		return errors.New("dir param is not allowed")
	}
	if !c.isAllowed("callback") && p.Callback != "" {
		return errors.New("callback param is not allowed")
	}

	return nil
}

func callback(w http.ResponseWriter, r *http.Request, c Cmd, p Param) bool {
	if p.Callback == "" {
		return false
	}

	go runCallback(r, c, p)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	cb := Callback{
		RequestID: requestId(r),
		URL:       p.Callback,
	}

	json.NewEncoder(w).Encode(cb)

	hlog.FromRequest(r).Info().
		Int("status", http.StatusOK).
		Str("callback_url", p.Callback).
		Msg("")

	return true
}

func runCallback(r *http.Request, c Cmd, p Param) {
	result := Result{
		RequestID: requestId(r),
	}
	output, err := run(c)
	if err != nil {
		result.Error = err.Error()
	}
	result.Output = string(output)
	result.Status = http.StatusOK

	logResult(r, result)

	b, err := json.Marshal(result)
	if err != nil && result.Error == "" {
		result.Error = err.Error()
	}

	resp, err := http.Post(p.Callback, "application/json", bytes.NewBuffer(b))
	if err != nil {
		hlog.FromRequest(r).Error().Err(err).Msg("")
	}
	hlog.FromRequest(r).Info().
		Str("callback_resp_status", resp.Status).
		Msg("")
}

func requestId(r *http.Request) string {
	var reqId string
	if id, ok := hlog.IDFromRequest(r); ok {
		reqId = id.String()
	}
	return reqId
}

func run(c Cmd) ([]byte, error) {
	cmd := exec.Command(c.Command, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Envs

	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}

	return cmd.CombinedOutput()
}

func logResult(r *http.Request, result Result) {
	hlog.FromRequest(r).Info().
		Int("status", result.Status).
		Str("output", result.Output).
		Str("error", result.Error).
		Msg("")
}
