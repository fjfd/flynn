package installer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/badgerodon/ioutil"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/sse"
)

type installerJSConfig struct {
	Endpoints            map[string]string `json:"endpoints"`
	HasAWSEnvCredentials bool              `json:"has_aws_env_credentials"`
}

type jsonInput struct {
	Creds        jsonInputCreds `json:"creds"`
	Region       string         `json:"region"`
	InstanceType string         `json:"instance_type"`
	NumInstances int            `json:"num_instances"`
	VpcCidr      string         `json:"vpc_cidr,omitempty"`
	SubnetCidr   string         `json:"subnet_cidr,omitempty"`
}

type jsonInputCreds struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

type httpPrompt struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"`
	Message  string `json:"message,omitempty"`
	Yes      bool   `json:"yes,omitempty"`
	Input    string `json:"input,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
	resChan  chan *httpPrompt
}

type httpEvent struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Prompt      *httpPrompt `json:"prompt,omitempty"`
}

type httpInstaller struct {
	ID            string           `json:"id"`
	Stack         *Stack           `json:"-"`
	PromptOutChan chan *httpPrompt `json:"-"`
	PromptInChan  chan *httpPrompt `json:"-"`
	logger        log.Logger
	subscribeMux  sync.Mutex
	subscriptions []*httpInstallerSubscription
	eventsMux     sync.Mutex
	events        []*httpEvent
	err           error
	done          bool
}

type httpInstallerSubscription struct {
	EventIndex int
	EventChan  chan *httpEvent
	DoneChan   chan struct{}
	ErrChan    chan error
	done       bool
}

func (sub *httpInstallerSubscription) sendEvents(s *httpInstaller) {
	if sub.done {
		return
	}
	for index, event := range s.events {
		if index <= sub.EventIndex {
			continue
		}
		sub.EventIndex = index
		sub.EventChan <- event
	}
}

func (sub *httpInstallerSubscription) handleError(err error) {
	if sub.done {
		return
	}
	sub.ErrChan <- err
}

func (sub *httpInstallerSubscription) handleDone() {
	if sub.done {
		return
	}
	sub.done = true
	close(sub.DoneChan)
}

func (prompt *httpPrompt) Resolve(res *httpPrompt) {
	httpInstallerPromtsMux.Lock()
	delete(httpInstallerPrompts, prompt.ID)
	httpInstallerPromtsMux.Unlock()
	prompt.Resolved = true
	prompt.resChan <- res
}

func (s *httpInstaller) YesNoPrompt(msg string) bool {
	s.logger.Info(fmt.Sprintf("YesNoPrompt %s", msg))
	prompt := &httpPrompt{
		ID:      random.Hex(16),
		Type:    "yes_no",
		Message: msg,
		resChan: make(chan *httpPrompt),
	}
	httpInstallerPromtsMux.Lock()
	httpInstallerPrompts[prompt.ID] = prompt
	httpInstallerPromtsMux.Unlock()

	s.logger.Info(prompt.Message)
	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	s.logger.Info("YesNoPrompt waiting...")
	res := <-prompt.resChan

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	s.logger.Info(fmt.Sprintf("YesNoPrompt %v", res))
	return res.Yes
}

func (s *httpInstaller) PromptInput(msg string) string {
	s.logger.Info(fmt.Sprintf("PromptInput %s", msg))
	prompt := &httpPrompt{
		ID:      random.Hex(16),
		Type:    "input",
		Message: msg,
		resChan: make(chan *httpPrompt),
	}
	httpInstallerPromtsMux.Lock()
	httpInstallerPrompts[prompt.ID] = prompt
	httpInstallerPromtsMux.Unlock()

	s.logger.Info(prompt.Message)
	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	s.logger.Info("PromptInput waiting...")
	res := <-prompt.resChan

	s.sendEvent(&httpEvent{
		Type:   "prompt",
		Prompt: prompt,
	})

	s.logger.Info(fmt.Sprintf("PromptInput %v", res))
	return res.Input
}

func (s *httpInstaller) Subscribe(eventChan chan *httpEvent) (<-chan struct{}, <-chan error) {
	s.subscribeMux.Lock()
	defer s.subscribeMux.Unlock()

	subscription := &httpInstallerSubscription{
		EventIndex: -1,
		EventChan:  eventChan,
		DoneChan:   make(chan struct{}),
		ErrChan:    make(chan error),
	}

	go func() {
		subscription.sendEvents(s)
		if s.err != nil {
			subscription.handleError(s.err)
		}
		if s.done {
			subscription.handleDone()
		}
	}()

	s.subscriptions = append(s.subscriptions, subscription)

	return subscription.DoneChan, subscription.ErrChan
}

func (s *httpInstaller) sendEvent(event *httpEvent) {
	s.eventsMux.Lock()
	s.events = append(s.events, event)
	s.eventsMux.Unlock()

	for _, sub := range s.subscriptions {
		go sub.sendEvents(s)
	}
}

func (s *httpInstaller) handleError(err error) {
	for _, sub := range s.subscriptions {
		go sub.handleError(err)
	}
}

func (s *httpInstaller) handleDone() {
	if s.Stack.Domain != nil {
		s.logger.Info("sending domain")
		s.sendEvent(&httpEvent{
			Type:        "domain",
			Description: s.Stack.Domain.Name,
		})
	}
	if s.Stack.DashboardLoginToken != "" {
		s.logger.Info("sending DashboardLoginToken")
		s.sendEvent(&httpEvent{
			Type:        "dashboard_login_token",
			Description: s.Stack.DashboardLoginToken,
		})
	}
	if s.Stack.CACert != "" {
		s.logger.Info("sending CACert")
		s.sendEvent(&httpEvent{
			Type:        "ca_cert",
			Description: base64.URLEncoding.EncodeToString([]byte(s.Stack.CACert)),
		})
	}
	s.sendEvent(&httpEvent{
		Type: "done",
	})

	for _, sub := range s.subscriptions {
		go sub.handleDone()
	}
}

func (s *httpInstaller) handleEvents() {
	for {
		select {
		case event := <-s.Stack.EventChan:
			s.logger.Info(event.Description)
			s.sendEvent(&httpEvent{
				Type:        "status",
				Description: event.Description,
			})
		case err := <-s.Stack.ErrChan:
			s.logger.Info(err.Error())
			s.handleError(err)
		case <-s.Stack.Done:
			s.logger.Info("stack install complete")
			s.handleDone()
			return
		}
	}
}

var httpInstallerPrompts = make(map[string]*httpPrompt)
var httpInstallerPromtsMux sync.Mutex
var httpInstallerStacks = make(map[string]*httpInstaller)
var httpInstallerStackMux sync.Mutex
var awsEnvCreds aws.CredentialsProvider

func ServeHTTP(port string) error {
	if creds, err := aws.EnvCreds(); err == nil {
		awsEnvCreds = creds
	}

	httpRouter := httprouter.New()

	httpRouter.GET("/", serveTemplate)
	httpRouter.GET("/install", serveTemplate)
	httpRouter.GET("/install/:id", serveTemplate)
	httpRouter.POST("/install", installHandler)
	httpRouter.GET("/events/:id", eventsHandler)
	httpRouter.POST("/prompt/:id", promptHandler)
	httpRouter.GET("/application.js", serveApplicationJS)
	httpRouter.GET("/assets/*assetPath", serveAsset)

	addr := fmt.Sprintf(":%s", port)
	fmt.Printf("Navigate to http://localhost%s in your web browser to get started.\n", addr)
	return http.ListenAndServe(addr, corsHandler(httpRouter))
}

func corsHandler(main http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httphelper.CORSAllowAllHandler(w, r)
		main.ServeHTTP(w, r)
	})
}

func installHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	var input *jsonInput
	if err := httphelper.DecodeJSON(req, &input); err != nil {
		httphelper.Error(w, err)
		return
	}
	httpInstallerStackMux.Lock()
	defer httpInstallerStackMux.Unlock()

	if len(httpInstallerStacks) > 0 {
		httphelper.Error(w, &httphelper.JSONError{
			Code:    httphelper.ObjectExistsError,
			Message: "install already started",
		})
		return
	}

	var id = random.Hex(16)
	var creds aws.CredentialsProvider
	if input.Creds.AccessKeyID != "" && input.Creds.SecretAccessKey != "" {
		creds = aws.Creds(input.Creds.AccessKeyID, input.Creds.SecretAccessKey, "")
	} else {
		var err error
		creds, err = aws.EnvCreds()
		if err != nil {
			httphelper.Error(w, &httphelper.JSONError{
				Code:    httphelper.ValidationError,
				Message: err.Error(),
			})
			return
		}
	}
	s := &httpInstaller{
		ID:            id,
		PromptOutChan: make(chan *httpPrompt),
		PromptInChan:  make(chan *httpPrompt),
		logger:        log.New(),
	}
	s.Stack = &Stack{
		Creds:        creds,
		Region:       input.Region,
		InstanceType: input.InstanceType,
		NumInstances: input.NumInstances,
		VpcCidr:      input.VpcCidr,
		SubnetCidr:   input.SubnetCidr,
		PromptInput:  s.PromptInput,
		YesNoPrompt:  s.YesNoPrompt,
	}
	if err := s.Stack.RunAWS(); err != nil {
		httphelper.Error(w, err)
		return
	}
	httpInstallerStacks[id] = s
	go s.handleEvents()
	httphelper.JSON(w, 200, s)
}

func eventsHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	s := httpInstallerStacks[id]
	if s == nil {
		httphelper.Error(w, &httphelper.JSONError{
			Code:    httphelper.NotFoundError,
			Message: "install instance not found",
		})
		return
	}

	eventChan := make(chan *httpEvent)
	doneChan, errChan := s.Subscribe(eventChan)

	stream := sse.NewStream(w, eventChan, s.logger)
	stream.Serve()

	s.logger.Info(fmt.Sprintf("streaming events for %s", s.ID))

	go func() {
		for {
			select {
			case err := <-errChan:
				s.logger.Info(err.Error())
				stream.Error(err)
			case <-doneChan:
				s.logger.Info("closing stream")
				stream.Close()
				return
			}
		}
	}()

	stream.Wait()

	s.logger.Info(s.Stack.DashboardLoginMsg())
}

func promptHandler(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	prompt := httpInstallerPrompts[id]
	if prompt == nil {
		httphelper.Error(w, &httphelper.JSONError{
			Code:    httphelper.NotFoundError,
			Message: "prompt not found",
		})
		return
	}

	var input *httpPrompt
	if err := httphelper.DecodeJSON(req, &input); err != nil {
		httphelper.Error(w, err)
		return
	}
	prompt.Resolve(input)
	w.WriteHeader(200)
}

func serveApplicationJS(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	path := filepath.Join("app", "build", "application.js")
	f, err := os.Open(path)
	if err != nil {
		fmt.Println(err)
		w.WriteHeader(500)
		return
	}

	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		fmt.Println(err)
		return
	}

	var jsConf bytes.Buffer
	jsConf.Write([]byte("window.InstallerConfig = "))
	json.NewEncoder(&jsConf).Encode(installerJSConfig{
		Endpoints: map[string]string{
			"install": "/install",
			"events":  "/events/:id",
			"prompt":  "/prompt/:id",
		},
		HasAWSEnvCredentials: awsEnvCreds != nil,
	})
	jsConf.Write([]byte(";\n"))

	r := ioutil.NewMultiReadSeeker(bytes.NewReader(jsConf.Bytes()), f)

	http.ServeContent(w, req, path, fi.ModTime(), r)
}

func serveAsset(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	http.ServeFile(w, req, filepath.Join("app", "build", params.ByName("assetPath")))
}

func serveTemplate(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
	if req.Header.Get("Accept") == "application/json" {
		s := httpInstallerStacks[params.ByName("id")]
		if s == nil && len(httpInstallerStacks) > 0 {
			for id := range httpInstallerStacks {
				s = httpInstallerStacks[id]
				break
			}
		}
		if s == nil {
			w.WriteHeader(404)
		} else {
			httphelper.JSON(w, 200, s)
		}
		return
	}

	http.ServeFile(w, req, filepath.Join("app", "installer.html"))
}
