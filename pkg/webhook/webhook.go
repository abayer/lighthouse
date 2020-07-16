package webhook

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/go-scm/scm/factory"
	"github.com/jenkins-x/lighthouse-config/pkg/config"
	"github.com/jenkins-x/lighthouse/pkg/clients"
	"github.com/jenkins-x/lighthouse/pkg/cmd/helper"
	"github.com/jenkins-x/lighthouse/pkg/git"
	"github.com/jenkins-x/lighthouse/pkg/launcher"
	"github.com/jenkins-x/lighthouse/pkg/logrusutil"
	"github.com/jenkins-x/lighthouse/pkg/metrics"
	"github.com/jenkins-x/lighthouse/pkg/plugins"
	"github.com/jenkins-x/lighthouse/pkg/util"
	"github.com/jenkins-x/lighthouse/pkg/version"
	"github.com/jenkins-x/lighthouse/pkg/watcher"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

const (
	// HealthPath is the URL path for the HTTP endpoint that returns health status.
	HealthPath = "/health"
	// ReadyPath URL path for the HTTP endpoint that returns ready status.
	ReadyPath = "/ready"
)

// Options holds the command line arguments
type Options struct {
	BindAddress string
	Path        string
	Port        int
	JSONLog     bool

	namespace        string
	pluginFilename   string
	configFilename   string
	server           *Server
	botName          string
	gitServerURL     string
	configMapWatcher *watcher.ConfigMapWatcher
	gitClient        git.Client
	launcher         launcher.PipelineLauncher
}

// NewCmdWebhook creates the command
func NewCmdWebhook() *cobra.Command {
	options := Options{}

	cmd := &cobra.Command{
		Use:   "lighthouse",
		Short: "Runs the lighthouse webhook handler",
		Run: func(cmd *cobra.Command, args []string) {
			err := options.Run()
			helper.CheckErr(err)
		},
	}

	cmd.Flags().BoolVarP(&options.JSONLog, "json", "", true, "Enable JSON logging")
	cmd.Flags().IntVarP(&options.Port, "port", "", 8080, "The TCP port to listen on.")
	cmd.Flags().StringVarP(&options.BindAddress, "bind", "", "",
		"The interface address to bind to (by default, will listen on all interfaces/addresses).")
	cmd.Flags().StringVarP(&options.Path, "path", "", "/hook",
		"The path to listen on for requests to trigger a pipeline run.")
	cmd.Flags().StringVar(&options.pluginFilename, "plugin-file", "", "Path to the plugins.yaml file. If not specified it is loaded from the 'plugins' ConfigMap")
	cmd.Flags().StringVar(&options.configFilename, "config-file", "", "Path to the config.yaml file. If not specified it is loaded from the 'config' ConfigMap")
	cmd.Flags().StringVar(&options.botName, "bot-name", "", "The name of the bot user to run as. Defaults to $GIT_USER if not specified.")
	cmd.Flags().StringVar(&options.namespace, "namespace", "", "The namespace to listen in")
	return cmd
}

// Run will implement this command
func (o *Options) Run() error {
	if o.JSONLog {
		logrus.SetFormatter(logrusutil.CreateDefaultFormatter())
	}

	var err error
	o.server, err = o.createHookServer()
	if err != nil {
		return errors.Wrapf(err, "failed to create Hook Server")
	}
	defer o.configMapWatcher.Stop()

	_, o.gitServerURL, err = o.createSCMClient()
	if err != nil {
		return errors.Wrapf(err, "failed to create ScmClient")
	}

	gitClient, err := git.NewClient(o.gitServerURL, o.gitKind())
	if err != nil {
		logrus.WithError(err).Fatal("Error getting git client.")
	}
	defer func() {
		err := gitClient.Clean()
		if err != nil {
			logrus.WithError(err).Fatal("Error cleaning the git client.")
		}
	}()

	o.gitClient = gitClient

	_, _, lhClient, _, err := clients.GetAPIClients()
	if err != nil {
		return errors.Wrap(err, "Error creating kubernetes resource clients.")
	}
	o.launcher = launcher.NewLauncher(lhClient, o.namespace)
	mux := http.NewServeMux()
	mux.Handle(HealthPath, http.HandlerFunc(o.health))
	mux.Handle(ReadyPath, http.HandlerFunc(o.ready))

	mux.Handle("/", http.HandlerFunc(o.defaultHandler))
	mux.Handle(o.Path, http.HandlerFunc(o.handleWebHookRequests))

	logrus.Infof("Lighthouse is now listening on path %s and port %d for WebHooks", o.Path, o.Port)
	return http.ListenAndServe(":"+strconv.Itoa(o.Port), mux)
}

// health returns either HTTP 204 if the service is healthy, otherwise nothing ('cos it's dead).
func (o *Options) health(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("Health check")
	w.WriteHeader(http.StatusNoContent)
}

// ready returns either HTTP 204 if the service is ready to serve requests, otherwise HTTP 503.
func (o *Options) ready(w http.ResponseWriter, r *http.Request) {
	logrus.Debug("Ready check")
	if o.isReady() {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
}

func (o *Options) defaultHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == o.Path || strings.HasPrefix(path, o.Path+"/") {
		o.handleWebHookRequests(w, r)
		return
	}
	path = strings.TrimPrefix(path, "/")
	if path == "" || path == "index.html" {
		return
	}
	http.Error(w, fmt.Sprintf("unknown path %s", path), 404)
}

func (o *Options) isReady() bool {
	// TODO a better readiness check
	return true
}

// handle request for pipeline runs
func (o *Options) handleWebHookRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// liveness probe etc
		logrus.WithField("method", r.Method).Debug("invalid http method so returning 200")
		return
	}
	logrus.Debug("about to parse webhook")

	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logrus.Errorf("failed to Read Body: %s", err.Error())
		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: Read Body: %s", err.Error()))
		return
	}

	err = r.Body.Close() // must close
	if err != nil {
		logrus.Errorf("failed to Close Body: %s", err.Error())
		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: Read Close: %s", err.Error()))
		return
	}

	r.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
	scmClient, serverURL, err := o.createSCMClient()
	if err != nil {
		logrus.Errorf("failed to create SCM scmClient: %s", err.Error())
		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: Failed to parse webhook: %s", err.Error()))
		return
	}

	webhook, err := scmClient.Webhooks.Parse(r, o.secretFn)
	if err != nil {
		logrus.Warnf("failed to parse webhook: %s", err.Error())

		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: Failed to parse webhook: %s", err.Error()))
		return
	}
	if webhook == nil {
		logrus.Error("no webhook was parsed")

		responseHTTPError(w, http.StatusInternalServerError, "500 Internal Server Error: No webhook could be parsed")
		return
	}

	ghaSecretDir := util.GetGitHubAppSecretDir()

	var gitCloneUser string
	var token string
	if ghaSecretDir != "" {
		gitCloneUser = util.GitHubAppGitRemoteUsername
		tokenFinder := util.NewOwnerTokensDir(serverURL, ghaSecretDir)
		token, err = tokenFinder.FindToken(webhook.Repository().Namespace)
		if err != nil {
			logrus.Errorf("failed to read owner token: %s", err.Error())
			responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: failed to read owner token: %s", err.Error()))
			return
		}
	} else {
		gitCloneUser = o.GetBotName()
		token, err = o.createSCMToken(o.gitKind())
		if err != nil {
			logrus.Errorf("no scm token specified: %s", err.Error())
			responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: no scm token specified: %s", err.Error()))
			return
		}
	}
	_, kubeClient, lhClient, _, err := clients.GetAPIClients()
	if err != nil {
		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: %s", err.Error()))
	}

	o.gitClient.SetCredentials(gitCloneUser, func() []byte {
		return []byte(token)
	})
	util.AddAuthToSCMClient(scmClient, token, ghaSecretDir != "")

	o.server.ClientAgent = &plugins.ClientAgent{
		BotName:           o.GetBotName(),
		SCMProviderClient: scmClient,
		KubernetesClient:  kubeClient,
		GitClient:         o.gitClient,
		LighthouseClient:  lhClient.LighthouseV1alpha1().LighthouseJobs(o.namespace),
		LauncherClient:    o.launcher,
	}
	l, output, err := o.ProcessWebHook(logrus.WithField("Webhook", webhook.Kind()), webhook)
	if err != nil {
		responseHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("500 Internal Server Error: %s", err.Error()))
	}
	// Demux events only to external plugins that require this event.
	if external := util.ExternalPluginsForEvent(o.server.Plugins, string(webhook.Kind()), webhook.Repository().FullName); len(external) > 0 {
		go util.CallExternalPluginsWithWebhook(l, external, webhook, o.hmacToken(), &o.server.wg)
	}

	_, err = w.Write([]byte(output))
	if err != nil {
		l.Debugf("failed to process the webhook: %v", err)
	}
}

// ProcessWebHook process a webhook
func (o *Options) ProcessWebHook(l *logrus.Entry, webhook scm.Webhook) (*logrus.Entry, string, error) {
	repository := webhook.Repository()
	fields := map[string]interface{}{
		"Namespace": repository.Namespace,
		"Name":      repository.Name,
		"Branch":    repository.Branch,
		"Link":      repository.Link,
		"ID":        repository.ID,
		"Clone":     repository.Clone,
		"Webhook":   webhook.Kind(),
	}
	l = l.WithFields(logrus.Fields(fields))
	_, ok := webhook.(*scm.PingHook)
	if ok {
		l.Info("received ping")
		return l, fmt.Sprintf("pong from lighthouse %s", version.Version), nil
	}
	// If we are in GitHub App mode and have a populated config, check if the repository for this webhook is one we actually
	// know about and error out if not.
	if util.GetGitHubAppSecretDir() != "" && o.server.ConfigAgent != nil {
		cfg := o.server.ConfigAgent.Config()
		if cfg != nil {
			if len(cfg.GetPostsubmits(repository)) == 0 && len(cfg.GetPresubmits(repository)) == 0 {
				l.Infof("webhook from unconfigured repository %s, returning error", repository.Link)
				return l, "", fmt.Errorf("repository not configured: %s", repository.Link)
			}
		}
	}
	pushHook, ok := webhook.(*scm.PushHook)
	if ok {
		fields["Ref"] = pushHook.Ref
		fields["BaseRef"] = pushHook.BaseRef
		fields["Commit.Sha"] = pushHook.Commit.Sha
		fields["Commit.Link"] = pushHook.Commit.Link
		fields["Commit.Author"] = pushHook.Commit.Author
		fields["Commit.Message"] = pushHook.Commit.Message
		fields["Commit.Committer.Name"] = pushHook.Commit.Committer.Name

		l.Info("invoking Push handler")

		o.server.HandlePushEvent(l, pushHook)
		return l, "processed push hook", nil
	}
	prHook, ok := webhook.(*scm.PullRequestHook)
	if ok {
		action := prHook.Action
		fields["Action"] = action.String()
		pr := prHook.PullRequest
		fields["PR.Number"] = pr.Number
		fields["PR.Ref"] = pr.Ref
		fields["PR.Sha"] = pr.Sha
		fields["PR.Title"] = pr.Title
		fields["PR.Body"] = pr.Body

		l.Info("invoking PR handler")

		o.server.HandlePullRequestEvent(l, prHook)
		return l, "processed PR hook", nil
	}
	branchHook, ok := webhook.(*scm.BranchHook)
	if ok {
		action := branchHook.Action
		ref := branchHook.Ref
		sender := branchHook.Sender
		fields["Action"] = action.String()
		fields["Ref.Sha"] = ref.Sha
		fields["Sender.Name"] = sender.Name

		l.Info("invoking branch handler")

		o.server.HandleBranchEvent(l, branchHook)
		return l, "processed branch hook", nil
	}
	issueCommentHook, ok := webhook.(*scm.IssueCommentHook)
	if ok {
		action := issueCommentHook.Action
		issue := issueCommentHook.Issue
		comment := issueCommentHook.Comment
		sender := issueCommentHook.Sender
		fields["Action"] = action.String()
		fields["Issue.Number"] = issue.Number
		fields["Issue.Title"] = issue.Title
		fields["Issue.Body"] = issue.Body
		fields["Comment.Body"] = comment.Body
		fields["Sender.Body"] = sender.Name
		fields["Sender.Login"] = sender.Login
		fields["Kind"] = "IssueCommentHook"

		l.Info("invoking Issue Comment handler")

		o.server.HandleIssueCommentEvent(l, *issueCommentHook)
		return l, "processed issue comment hook", nil
	}
	prCommentHook, ok := webhook.(*scm.PullRequestCommentHook)
	if ok {
		action := prCommentHook.Action
		fields["Action"] = action.String()
		pr := prCommentHook.PullRequest
		fields["PR.Number"] = pr.Number
		fields["PR.Ref"] = pr.Ref
		fields["PR.Sha"] = pr.Sha
		fields["PR.Title"] = pr.Title
		fields["PR.Body"] = pr.Body
		comment := prCommentHook.Comment
		fields["Comment.Body"] = comment.Body
		author := comment.Author
		fields["Author.Name"] = author.Name
		fields["Author.Login"] = author.Login
		fields["Author.Avatar"] = author.Avatar

		l.Info("invoking PR Comment handler")

		l.Info("invoking Issue Comment handler")

		o.server.HandlePullRequestCommentEvent(l, *prCommentHook)
		return l, "processed PR comment hook", nil
	}
	prReviewHook, ok := webhook.(*scm.ReviewHook)
	if ok {
		action := prReviewHook.Action
		fields["Action"] = action.String()
		pr := prReviewHook.PullRequest
		fields["PR.Number"] = pr.Number
		fields["PR.Ref"] = pr.Ref
		fields["PR.Sha"] = pr.Sha
		fields["PR.Title"] = pr.Title
		fields["PR.Body"] = pr.Body
		fields["Review.State"] = prReviewHook.Review.State
		fields["Reviewer.Name"] = prReviewHook.Review.Author.Name
		fields["Reviewer.Login"] = prReviewHook.Review.Author.Login
		fields["Reviewer.Avatar"] = prReviewHook.Review.Author.Avatar

		l.Info("invoking PR Review handler")

		o.server.HandleReviewEvent(l, *prReviewHook)
		return l, "processed PR review hook", nil
	}
	l.Debugf("unknown kind %s webhook %#v", webhook.Kind(), webhook)
	return l, fmt.Sprintf("unknown hook %s", webhook.Kind()), nil
}

func (o *Options) hmacToken() string {
	return os.Getenv("HMAC_TOKEN")
}

func (o *Options) secretFn(webhook scm.Webhook) (string, error) {
	return o.hmacToken(), nil
}

func (o *Options) createSCMClient() (*scm.Client, string, error) {
	kind := o.gitKind()
	serverURL := os.Getenv("GIT_SERVER")

	client, err := factory.NewClient(kind, serverURL, "")
	return client, serverURL, err
}

func (o *Options) gitKind() string {
	kind := os.Getenv("GIT_KIND")
	if kind == "" {
		kind = "github"
	}
	return kind
}

// GetBotName returns the bot name
func (o *Options) GetBotName() string {
	if util.GetGitHubAppSecretDir() != "" {
		ghaBotName, err := util.GetGitHubAppAPIUser()
		// TODO: Probably should handle error cases here better, but for now, just fall through.
		if err == nil && ghaBotName != "" {
			return ghaBotName
		}
	}
	o.botName = os.Getenv("GIT_USER")
	if o.botName == "" {
		o.botName = "jenkins-x-bot"
	}
	return o.botName
}

func (o *Options) createSCMToken(gitKind string) (string, error) {
	envName := "GIT_TOKEN"
	value := os.Getenv(envName)
	if value == "" {
		return value, fmt.Errorf("No token available for git kind %s at environment variable $%s", gitKind, envName)
	}
	return value, nil
}

func (o *Options) createHookServer() (*Server, error) {
	configAgent := &config.Agent{}
	pluginAgent := &plugins.ConfigAgent{}

	onConfigYamlChange := func(text string) {
		if text != "" {
			config, err := config.LoadYAMLConfig([]byte(text))
			if err != nil {
				logrus.WithError(err).Error("Error processing the prow Config YAML")
			} else {
				logrus.Info("updating the prow core configuration")
				configAgent.Set(config)
			}
		}
	}

	onPluginsYamlChange := func(text string) {
		if text != "" {
			config, err := pluginAgent.LoadYAMLConfig([]byte(text))
			if err != nil {
				logrus.WithError(err).Error("Error processing the prow Plugins YAML")
			} else {
				logrus.Info("updating the prow plugins configuration")
				pluginAgent.Set(config)
			}
		}
	}

	_, kubeClient, _, _, err := clients.GetAPIClients()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create Kube client")
	}

	callbacks := []watcher.ConfigMapCallback{
		&watcher.ConfigMapEntryCallback{
			Name:     util.ProwConfigMapName,
			Key:      util.ProwConfigFilename,
			Callback: onConfigYamlChange,
		},
		&watcher.ConfigMapEntryCallback{
			Name:     util.ProwPluginsConfigMapName,
			Key:      util.ProwPluginsFilename,
			Callback: onPluginsYamlChange,
		},
	}
	o.configMapWatcher, err = watcher.NewConfigMapWatcher(kubeClient, o.namespace, callbacks, stopper())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create ConfigMap watcher")
	}

	promMetrics := NewMetrics()

	// Push metrics to the configured prometheus pushgateway endpoint.
	agentConfig := configAgent.Config()
	if agentConfig != nil {
		pushGateway := agentConfig.PushGateway
		if pushGateway.Endpoint != "" {
			logrus.WithField("gateway", pushGateway.Endpoint).Infof("using push gateway")
			go metrics.ExposeMetrics("hook", pushGateway)
		} else {
			logrus.Warn("not pushing metrics as there is no push_gateway defined in the config.yaml")
		}
	} else {
		logrus.Warn("no configAgent configuration")
	}

	serverURL, err := url.Parse(o.gitServerURL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse server URL %s", o.gitServerURL)
	}
	server := &Server{
		ConfigAgent: configAgent,
		Plugins:     pluginAgent,
		Metrics:     promMetrics,
		ServerURL:   serverURL,
		//TokenGenerator: secretAgent.GetTokenGenerator(o.webhookSecretFile),
	}
	return server, nil
}

func responseHTTPError(w http.ResponseWriter, statusCode int, response string) {
	logrus.WithFields(logrus.Fields{
		"response":    response,
		"status-code": statusCode,
	}).Info(response)
	http.Error(w, response, statusCode)
}

// stopper returns a channel that remains open until an interrupt is received.
func stopper() chan struct{} {
	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logrus.Warn("Interrupt received, attempting clean shutdown...")
		close(stop)
		<-c
		logrus.Error("Second interrupt received, force exiting...")
		os.Exit(1)
	}()
	return stop
}
