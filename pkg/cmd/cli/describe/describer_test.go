package describe

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	ktestclient "k8s.io/kubernetes/pkg/client/unversioned/testclient"
	"k8s.io/kubernetes/pkg/kubectl"
	"k8s.io/kubernetes/pkg/labels"

	api "github.com/openshift/origin/pkg/api"
	authorizationapi "github.com/openshift/origin/pkg/authorization/api"
	buildapi "github.com/openshift/origin/pkg/build/api"
	"github.com/openshift/origin/pkg/client"
	"github.com/openshift/origin/pkg/client/testclient"
	deployapi "github.com/openshift/origin/pkg/deploy/api"
	deployapitest "github.com/openshift/origin/pkg/deploy/api/test"
	deployutil "github.com/openshift/origin/pkg/deploy/util"
	imageapi "github.com/openshift/origin/pkg/image/api"
	oauthapi "github.com/openshift/origin/pkg/oauth/api"
	projectapi "github.com/openshift/origin/pkg/project/api"
	sdnapi "github.com/openshift/origin/pkg/sdn/api"

	// install all APIs
	_ "github.com/openshift/origin/pkg/api/install"
	_ "k8s.io/kubernetes/pkg/api/install"
	_ "k8s.io/kubernetes/pkg/apis/extensions/install"
)

type describeClient struct {
	T         *testing.T
	Namespace string
	Err       error
	*testclient.Fake
}

// DescriberCoverageExceptions is the list of API types that do NOT have corresponding describers
// If you add something to this list, explain why it doesn't need validation.  waaaa is not a valid
// reason.
var DescriberCoverageExceptions = []reflect.Type{
	reflect.TypeOf(&buildapi.BuildLog{}),                              // normal users don't ever look at these
	reflect.TypeOf(&buildapi.BuildLogOptions{}),                       // normal users don't ever look at these
	reflect.TypeOf(&buildapi.BinaryBuildRequestOptions{}),             // normal users don't ever look at these
	reflect.TypeOf(&buildapi.BuildRequest{}),                          // normal users don't ever look at these
	reflect.TypeOf(&deployapi.DeploymentConfigRollback{}),             // normal users don't ever look at these
	reflect.TypeOf(&deployapi.DeploymentLog{}),                        // normal users don't ever look at these
	reflect.TypeOf(&deployapi.DeploymentLogOptions{}),                 // normal users don't ever look at these
	reflect.TypeOf(&imageapi.DockerImage{}),                           // not a top level resource
	reflect.TypeOf(&imageapi.ImageStreamImport{}),                     // normal users don't ever look at these
	reflect.TypeOf(&oauthapi.OAuthAccessToken{}),                      // normal users don't ever look at these
	reflect.TypeOf(&oauthapi.OAuthAuthorizeToken{}),                   // normal users don't ever look at these
	reflect.TypeOf(&oauthapi.OAuthClientAuthorization{}),              // normal users don't ever look at these
	reflect.TypeOf(&projectapi.ProjectRequest{}),                      // normal users don't ever look at these
	reflect.TypeOf(&authorizationapi.IsPersonalSubjectAccessReview{}), // not a top level resource

	// these resources can't be "GET"ed, so you can't make a describer for them
	reflect.TypeOf(&authorizationapi.SubjectAccessReviewResponse{}),
	reflect.TypeOf(&authorizationapi.ResourceAccessReviewResponse{}),
	reflect.TypeOf(&authorizationapi.SubjectAccessReview{}),
	reflect.TypeOf(&authorizationapi.ResourceAccessReview{}),
	reflect.TypeOf(&authorizationapi.LocalSubjectAccessReview{}),
	reflect.TypeOf(&authorizationapi.LocalResourceAccessReview{}),
}

// MissingDescriberCoverageExceptions is the list of types that were missing describer methods when I started
// You should never add to this list
// TODO describers should be added for these types
var MissingDescriberCoverageExceptions = []reflect.Type{
	reflect.TypeOf(&imageapi.ImageStreamMapping{}),
	reflect.TypeOf(&oauthapi.OAuthClient{}),
	reflect.TypeOf(&sdnapi.ClusterNetwork{}),
	reflect.TypeOf(&sdnapi.HostSubnet{}),
	reflect.TypeOf(&sdnapi.NetNamespace{}),
}

func TestDescriberCoverage(t *testing.T) {
	c := &client.Client{}

main:
	for _, apiType := range kapi.Scheme.KnownTypes(api.SchemeGroupVersion) {
		if !strings.Contains(apiType.PkgPath(), "openshift/origin") {
			continue
		}
		// we don't describe lists
		if strings.HasSuffix(apiType.Name(), "List") {
			continue
		}

		ptrType := reflect.PtrTo(apiType)
		for _, exception := range DescriberCoverageExceptions {
			if ptrType == exception {
				continue main
			}
		}
		for _, exception := range MissingDescriberCoverageExceptions {
			if ptrType == exception {
				continue main
			}
		}

		_, ok := DescriberFor(api.SchemeGroupVersion.WithKind(apiType.Name()).GroupKind(), c, &ktestclient.Fake{}, "")
		if !ok {
			t.Errorf("missing printer for %v.  Check pkg/cmd/cli/describe/describer.go", apiType)
		}
	}
}

func TestDescribers(t *testing.T) {
	fake := &testclient.Fake{}
	fakeKube := &ktestclient.Fake{}
	c := &describeClient{T: t, Namespace: "foo", Fake: fake}

	testDescriberList := []kubectl.Describer{
		&BuildDescriber{c, fakeKube},
		&BuildConfigDescriber{c, ""},
		&ImageDescriber{c},
		&ImageStreamDescriber{c},
		&ImageStreamTagDescriber{c},
		&ImageStreamImageDescriber{c},
		&RouteDescriber{c, fakeKube},
		&ProjectDescriber{c, fakeKube},
		&PolicyDescriber{c},
		&PolicyBindingDescriber{c},
		&TemplateDescriber{c, nil, nil, nil},
	}

	for _, d := range testDescriberList {
		out, err := d.Describe("foo", "bar")
		if err != nil {
			t.Errorf("unexpected error for %v: %v", d, err)
		}
		if !strings.Contains(out, "Name:") || !strings.Contains(out, "Labels:") {
			t.Errorf("unexpected out: %s", out)
		}
	}
}

func TestDeploymentConfigDescriber(t *testing.T) {
	config := deployapitest.OkDeploymentConfig(1)
	deployment, _ := deployutil.MakeDeployment(config, kapi.Codecs.LegacyCodec(deployapi.SchemeGroupVersion))
	podList := &kapi.PodList{}
	eventList := &kapi.EventList{}
	deploymentList := &kapi.ReplicationControllerList{}

	d := &DeploymentConfigDescriber{
		client: &genericDeploymentDescriberClient{
			getDeploymentConfigFunc: func(namespace, name string) (*deployapi.DeploymentConfig, error) {
				return config, nil
			},
			getDeploymentFunc: func(namespace, name string) (*kapi.ReplicationController, error) {
				return deployment, nil
			},
			listDeploymentsFunc: func(namespace string, selector labels.Selector) (*kapi.ReplicationControllerList, error) {
				return deploymentList, nil
			},
			listPodsFunc: func(namespace string, selector labels.Selector) (*kapi.PodList, error) {
				return podList, nil
			},
			listEventsFunc: func(deploymentConfig *deployapi.DeploymentConfig) (*kapi.EventList, error) {
				return eventList, nil
			},
		},
	}

	describe := func() {
		if output, err := d.Describe("test", "deployment"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		} else {
			t.Logf("describer output:\n%s\n", output)
		}
	}

	podList.Items = []kapi.Pod{*mkPod(kapi.PodRunning, 0)}
	describe()

	config.Spec.Triggers = append(config.Spec.Triggers, deployapitest.OkConfigChangeTrigger())
	describe()

	config.Spec.Strategy = deployapitest.OkCustomStrategy()
	describe()

	config.Spec.Triggers[0].ImageChangeParams.From = kapi.ObjectReference{Name: "imageRepo"}
	describe()

	config.Spec.Strategy = deployapitest.OkStrategy()
	config.Spec.Strategy.RecreateParams = &deployapi.RecreateDeploymentStrategyParams{
		Pre: &deployapi.LifecycleHook{
			FailurePolicy: deployapi.LifecycleHookFailurePolicyAbort,
			ExecNewPod: &deployapi.ExecNewPodHook{
				ContainerName: "container",
				Command:       []string{"/command1", "args"},
				Env: []kapi.EnvVar{
					{
						Name:  "KEY1",
						Value: "value1",
					},
				},
			},
		},
		Post: &deployapi.LifecycleHook{
			FailurePolicy: deployapi.LifecycleHookFailurePolicyIgnore,
			ExecNewPod: &deployapi.ExecNewPodHook{
				ContainerName: "container",
				Command:       []string{"/command2", "args"},
				Env: []kapi.EnvVar{
					{
						Name:  "KEY2",
						Value: "value2",
					},
				},
			},
		},
	}
	describe()
}

func TestDescribeBuildDuration(t *testing.T) {
	type testBuild struct {
		build  *buildapi.Build
		output string
	}

	creation := unversioned.Date(2015, time.April, 9, 6, 0, 0, 0, time.Local)
	// now a minute ago
	minuteAgo := unversioned.Unix(unversioned.Now().Rfc3339Copy().Time.Unix()-60, 0)
	start := unversioned.Date(2015, time.April, 9, 6, 1, 0, 0, time.Local)
	completion := unversioned.Date(2015, time.April, 9, 6, 2, 0, 0, time.Local)
	duration := completion.Rfc3339Copy().Time.Sub(start.Rfc3339Copy().Time)
	zeroDuration := time.Duration(0)

	tests := []testBuild{
		{ // 0 - build new
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: minuteAgo},
				Status: buildapi.BuildStatus{
					Phase:    buildapi.BuildPhaseNew,
					Duration: zeroDuration,
				},
			},
			"waiting for 1m0s",
		},
		{ // 1 - build pending
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: minuteAgo},
				Status: buildapi.BuildStatus{
					Phase:    buildapi.BuildPhasePending,
					Duration: zeroDuration,
				},
			},
			"waiting for 1m0s",
		},
		{ // 2 - build running
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					StartTimestamp: &start,
					Phase:          buildapi.BuildPhaseRunning,
					Duration:       duration,
				},
			},
			"running for 1m0s",
		},
		{ // 3 - build completed
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					StartTimestamp:      &start,
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseComplete,
					Duration:            duration,
				},
			},
			"1m0s",
		},
		{ // 4 - build failed
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					StartTimestamp:      &start,
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseFailed,
					Duration:            duration,
				},
			},
			"1m0s",
		},
		{ // 5 - build error
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					StartTimestamp:      &start,
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseError,
					Duration:            duration,
				},
			},
			"1m0s",
		},
		{ // 6 - build cancelled before running, start time wasn't set yet
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseCancelled,
					Duration:            duration,
				},
			},
			"waited for 2m0s",
		},
		{ // 7 - build cancelled while running, start time is set already
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					StartTimestamp:      &start,
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseCancelled,
					Duration:            duration,
				},
			},
			"1m0s",
		},
		{ // 8 - build failed before running, start time wasn't set yet
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseFailed,
					Duration:            duration,
				},
			},
			"waited for 2m0s",
		},
		{ // 9 - build error before running, start time wasn't set yet
			&buildapi.Build{
				ObjectMeta: kapi.ObjectMeta{CreationTimestamp: creation},
				Status: buildapi.BuildStatus{
					CompletionTimestamp: &completion,
					Phase:               buildapi.BuildPhaseError,
					Duration:            duration,
				},
			},
			"waited for 2m0s",
		},
	}

	for i, tc := range tests {
		if actual, expected := describeBuildDuration(tc.build), tc.output; actual != expected {
			t.Errorf("(%d) expected duration output %s, got %s", i, expected, actual)
		}
	}
}

func mkPod(status kapi.PodPhase, exitCode int) *kapi.Pod {
	return &kapi.Pod{
		ObjectMeta: kapi.ObjectMeta{Name: "PodName"},
		Status: kapi.PodStatus{
			Phase: status,
			ContainerStatuses: []kapi.ContainerStatus{
				{
					State: kapi.ContainerState{
						Terminated: &kapi.ContainerStateTerminated{ExitCode: exitCode},
					},
				},
			},
		},
	}
}

func TestDescribePostCommitHook(t *testing.T) {
	tests := []struct {
		hook buildapi.BuildPostCommitSpec
		want string
	}{
		{
			hook: buildapi.BuildPostCommitSpec{},
			want: "",
		},
		{
			hook: buildapi.BuildPostCommitSpec{
				Script: "go test",
			},
			want: `"/bin/sh", "-ic", "go test"`,
		},
		{
			hook: buildapi.BuildPostCommitSpec{
				Command: []string{"go", "test"},
			},
			want: `"go", "test"`,
		},
		{
			hook: buildapi.BuildPostCommitSpec{
				Args: []string{"go", "test"},
			},
			want: `"<image-entrypoint>", "go", "test"`,
		},
		{
			hook: buildapi.BuildPostCommitSpec{
				Script: `go test "$@"`,
				Args:   []string{"-v", "-timeout", "2s"},
			},
			want: `"/bin/sh", "-ic", "go test \"$@\"", "/bin/sh", "-v", "-timeout", "2s"`,
		},
		{
			hook: buildapi.BuildPostCommitSpec{
				Command: []string{"go", "test"},
				Args:    []string{"-v", "-timeout", "2s"},
			},
			want: `"go", "test", "-v", "-timeout", "2s"`,
		},
		{
			// Invalid hook: Script and Command are not allowed
			// together. For printing, Script takes precedence.
			hook: buildapi.BuildPostCommitSpec{
				Script:  "go test -v",
				Command: []string{"go", "test"},
			},
			want: `"/bin/sh", "-ic", "go test -v"`,
		},
	}
	for _, tt := range tests {
		var b bytes.Buffer
		out := tabwriter.NewWriter(&b, 0, 8, 0, '\t', 0)
		describePostCommitHook(tt.hook, out)
		if err := out.Flush(); err != nil {
			t.Fatalf("%+v: flush error: %v", tt.hook, err)
		}
		var want string
		if tt.want != "" {
			want = fmt.Sprintf("Post Commit Hook:\t[%s]\n", tt.want)
		}
		if got := b.String(); got != want {
			t.Errorf("describePostCommitHook(%+v, out) = %q, want %q", tt.hook, got, want)
		}
	}
}
