package launcher

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/jenkins-x/go-scm/scm"
	jxv1 "github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	jxclient "github.com/jenkins-x/jx/pkg/client/clientset/versioned"
	"github.com/jenkins-x/jx/pkg/tekton/metapipeline"
	"github.com/jenkins-x/lighthouse/pkg/apis/lighthouse/v1alpha1"
	clientset "github.com/jenkins-x/lighthouse/pkg/client/clientset/versioned"
	"github.com/jenkins-x/lighthouse/pkg/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// launcher default launcher
type launcher struct {
	jxClient  jxclient.Interface
	lhClient  clientset.Interface
	namespace string
}

// NewLauncher creates a new builder
func NewLauncher(jxClient jxclient.Interface, lhClient clientset.Interface, namespace string) (PipelineLauncher, error) {
	b := &launcher{
		jxClient:  jxClient,
		lhClient:  lhClient,
		namespace: namespace,
	}
	return b, nil
}

// Launch creates a pipeline
func (b *launcher) Launch(request *v1alpha1.LighthouseJob, metapipelineClient metapipeline.Client, repository scm.Repository) (*v1alpha1.LighthouseJob, error) {
	spec := &request.Spec

	name := repository.Name
	owner := repository.Namespace
	sourceURL := repository.Clone

	pullRefData := b.getPullRefs(sourceURL, spec)
	pullRefs := ""
	if len(spec.Refs.Pulls) > 0 {
		pullRefs = pullRefData.String()
	}

	branch := b.getBranch(spec)
	if branch == "" {
		branch = repository.Branch
	}
	if branch == "" {
		branch = "master"
	}
	if pullRefs == "" {
		pullRefs = branch + ":"
	}

	job := spec.Job
	var kind metapipeline.PipelineKind
	if len(spec.Refs.Pulls) > 0 {
		kind = metapipeline.PullRequestPipeline
	} else {
		kind = metapipeline.ReleasePipeline
	}

	l := logrus.WithFields(logrus.Fields(map[string]interface{}{
		"Owner":     owner,
		"Name":      name,
		"SourceURL": sourceURL,
		"Branch":    branch,
		"PullRefs":  pullRefs,
		"Job":       job,
	}))
	l.Info("about to start Jenkinx X meta pipeline")

	sa := os.Getenv("JX_SERVICE_ACCOUNT")
	if sa == "" {
		sa = "tekton-bot"
	}

	pipelineCreateParam := metapipeline.PipelineCreateParam{
		PullRef:      pullRefData,
		PipelineKind: kind,
		Context:      spec.Context,
		// No equivalent to https://github.com/jenkins-x/jx/blob/bb59278c2707e0e99b3c24be926745c324824388/pkg/cmd/controller/pipeline/pipelinerunner_controller.go#L236
		//   for getting environment variables from the prow job here, so far as I can tell (abayer)
		// Also not finding an equivalent to labels from the PipelineRunRequest
		ServiceAccount: sa,
		// I believe we can use an empty string default image?
		DefaultImage: "",
		EnvVariables: spec.GetEnvVars(),
	}

	activityKey, tektonCRDs, err := metapipelineClient.Create(pipelineCreateParam)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create Tekton CRDs")
	}

	// Add the build number from the activity key to the labels on the job
	request.Labels[util.BuildNumLabel] = activityKey.Build
	// Set status on the job
	request.Status = v1alpha1.LighthouseJobStatus{
		State:        v1alpha1.PendingState,
		ActivityName: util.ToValidName(activityKey.Name),
		StartTime:    metav1.Now(),
	}

	// TODO: REMOVE
	jy, _ := yaml.Marshal(request)
	_ = ioutil.WriteFile(fmt.Sprintf("/tmp/lhj-%s-no-status.yaml", request.Name), jy, 0644)
	appliedJob, err := b.lhClient.LighthouseV1alpha1().LighthouseJobs(b.namespace).Create(request)
	if err != nil {
		return nil, errors.Wrap(err, "unable to apply LighthouseJob")
	}

	err = metapipelineClient.Apply(activityKey, tektonCRDs)
	if err != nil {
		return nil, errors.Wrap(err, "unable to apply Tekton CRDs")
	}
	return appliedJob, nil
}

func (b *launcher) getBranch(spec *v1alpha1.LighthouseJobSpec) string {
	branch := spec.Refs.BaseRef
	if spec.Type == v1alpha1.PostsubmitJob {
		return branch
	}
	if spec.Type == v1alpha1.BatchJob {
		return "batch"
	}
	if len(spec.Refs.Pulls) > 0 {
		branch = fmt.Sprintf("PR-%v", spec.Refs.Pulls[0].Number)
	}
	return branch
}

func (b *launcher) getPullRefs(sourceURL string, spec *v1alpha1.LighthouseJobSpec) metapipeline.PullRef {
	var pullRef metapipeline.PullRef
	if len(spec.Refs.Pulls) > 0 {
		var prs []metapipeline.PullRequestRef
		for _, pull := range spec.Refs.Pulls {
			prs = append(prs, metapipeline.PullRequestRef{ID: strconv.Itoa(pull.Number), MergeSHA: pull.SHA})
		}

		pullRef = metapipeline.NewPullRefWithPullRequest(sourceURL, spec.Refs.BaseRef, spec.Refs.BaseSHA, prs...)
	} else {
		pullRef = metapipeline.NewPullRef(sourceURL, spec.Refs.BaseRef, spec.Refs.BaseSHA)
	}

	return pullRef
}

// List list current pipelines
func (b *launcher) List(opts metav1.ListOptions) (*v1alpha1.LighthouseJobList, error) {
	list, err := b.jxClient.JenkinsV1().PipelineActivities(b.namespace).List(metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	answer := &v1alpha1.LighthouseJobList{}
	for _, pa := range list.Items {
		item := ToLighthouseJob(&pa)
		answer.Items = append(answer.Items, item)
	}
	return answer, nil
}

// ToLighthouseJob converts the PipelineActivity to a LighthouseJob object
// TODO: Rename/rework this to be about the spec at some point
func ToLighthouseJob(activity *jxv1.PipelineActivity) v1alpha1.LighthouseJob {
	spec := activity.Spec
	baseRef := "master"

	ref := &v1alpha1.Refs{
		Org:      spec.GitOwner,
		Repo:     spec.GitRepository,
		RepoLink: spec.GitURL,
		BaseRef:  baseRef,
		BaseSHA:  spec.BaseSHA,
	}

	kind := v1alpha1.PresubmitJob

	// TODO: Something for periodic.
	if spec.GitBranch == "master" {
		kind = v1alpha1.PostsubmitJob
	} else if len(spec.BatchPipelineActivity.ComprisingPulLRequests) > 0 {
		kind = v1alpha1.BatchJob
	}

	if strings.HasPrefix(spec.GitBranch, "PR-") {
		nt := strings.TrimPrefix(spec.GitBranch, "PR-")
		if nt != "" {
			n, err := strconv.Atoi(nt)
			if err == nil {
				ref.Pulls = append(ref.Pulls, v1alpha1.Pull{
					Number: n,
					SHA:    spec.LastCommitSHA,
					Title:  spec.PullTitle,
					Ref:    "pull/" + nt + "/head",

					// TODO
					// Link: spec.LastCommitURL,
					CommitLink: spec.LastCommitURL,
				})
			}
		}
	}

	return v1alpha1.LighthouseJob{
		ObjectMeta: activity.ObjectMeta,
		Spec: v1alpha1.LighthouseJobSpec{
			Type:           kind,
			Namespace:      activity.Namespace,
			Job:            spec.Pipeline, // TODO: this will end up being the config job name going forward so don't stress about it (apb)
			Refs:           ref,
			Context:        spec.Context,
			RerunCommand:   "",
			MaxConcurrency: 0,
		},
		Status: v1alpha1.LighthouseJobStatus{State: v1alpha1.ToPipelineState(spec.Status)},
	}
}
