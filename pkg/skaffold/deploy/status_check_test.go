/*
Copyright 2019 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package deploy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/kubectl"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/GoogleContainerTools/skaffold/testutil"
)

func TestGetDeadlineForDeployments(t *testing.T) {
	getDeploymentCommand := "kubectl --context kubecontext --namespace test get deployments -l app.kubernetes.io/managed-by=skaffold-unknown --output go-template='{{range .items}}{{.metadata.name}}:{{.spec.progressDeadlineSeconds}},{{end}}'"

	var tests = []struct {
		description string
		command     util.Command
		expected    map[string]float32
		shouldErr   bool
	}{
		{
			description: "returns deployments",
			command: testutil.NewFakeCmd(t).
				WithRunOut(getDeploymentCommand, "dep1:100,dep2:200"),
			expected: map[string]float32{"dep1": 100, "dep2": 200},
		},
		{
			description: "no deployments",
			command: testutil.NewFakeCmd(t).
				WithRunOut(getDeploymentCommand, "''"),
			expected: map[string]float32{},
		},
		{
			description: "get deployments error",
			command: testutil.NewFakeCmd(t).
				WithRunOutErr(getDeploymentCommand, "", fmt.Errorf("error")),
			shouldErr: true,
		},
	}

	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			t.Override(&util.DefaultExecCommand, test.command)
			cli := &kubectl.CLI{
				Namespace:   "test",
				KubeContext: testKubeContext,
			}
			actual, err := getDeadlineForDeployments(context.Background(), cli)
			t.CheckErrorAndDeepEqual(test.shouldErr, err, test.expected, actual)
		})
	}
}

type MockRolloutStatus struct {
	called    int
	responses []string
	err       error
}

func (m *MockRolloutStatus) Executefunc(context.Context, *kubectl.CLI, string) (string, error) {
	var resp string
	if m.err != nil {
		m.called++
		return "", m.err
	}
	if m.called >= len(m.responses) {
		resp = m.responses[len(m.responses)-1]
	} else {
		resp = m.responses[m.called]
	}
	m.called++
	return resp, m.err
}

func TestPollDeploymentsStatus(t *testing.T) {

	var tests = []struct {
		description string
		mock        *MockRolloutStatus
		duration    int
		exactCalls  int
		shouldErr   bool
	}{
		{
			description: "rollout returns success",
			mock: &MockRolloutStatus{
				responses: []string{"dep successfully rolled out"},
			},
			exactCalls: 1,
			duration:   500,
		},
		{
			description: "rollout returns error in the first attempt",
			mock: &MockRolloutStatus{
				err: errors.New("deployment.apps/dep could not be found"),
			},
			shouldErr:  true,
			exactCalls: 1,
			duration:   500,
		},
		{
			description: "rollout returns success before time out",
			mock: &MockRolloutStatus{
				responses: []string{
					"Waiting for rollout to finish: 0 of 1 updated replicas are available...",
					"Waiting for rollout to finish: 0 of 1 updated replicas are available...",
					"deployment.apps/dep successfully rolled out"},
			},
			duration:   500,
			exactCalls: 3,
		},
		{
			description: "rollout returns did not stabilize within the given timeout",
			mock: &MockRolloutStatus{
				responses: []string{
					"Waiting for rollout to finish: 1 of 3 updated replicas are available...",
					"Waiting for rollout to finish: 1 of 3 updated replicas are available...",
					"Waiting for rollout to finish: 2 of 3 updated replicas are available..."},
			},
			duration:  1000,
			shouldErr: true,
		},
	}
	originalPollingPeriod := defaultPollPeriodInMilliseconds
	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			mock := test.mock
			// Figure out why i can't use t.Override.
			// Using t.Override throws an error "reflect: call of reflect.Value.Elem on func Value"
			executeRolloutStatus = mock.Executefunc
			defer func() { executeRolloutStatus = getRollOutStatus }()

			defaultPollPeriodInMilliseconds = 100
			defer func() { defaultPollPeriodInMilliseconds = originalPollingPeriod }()

			actual := &sync.Map{}
			pollDeploymentsStatus(context.Background(), &kubectl.CLI{}, "dep", time.Duration(test.duration)*time.Millisecond, actual)

			if _, ok := actual.Load("dep"); !ok {
				t.Error("expected result for deployment dep. But found none")
			}
			err := getDeployStatus(actual)
			t.CheckError(test.shouldErr, err)
			if test.exactCalls > 0 {
				t.CheckDeepEqual(test.exactCalls, mock.called)
			}
		})
	}
}

func TestGetDeployStatus(t *testing.T) {
	var tests = []struct {
		description    string
		deps           map[string]interface{}
		expectedErrMsg []string
		shouldErr      bool
	}{
		{
			description: "one error",
			deps: map[string]interface{}{
				"dep1": "SUCCESS",
				"dep2": fmt.Errorf("could not return within default timeout"),
			},
			expectedErrMsg: []string{"deployment dep2 failed due to could not return within default timeout"},
			shouldErr:      true,
		},
		{
			description: "no error",
			deps: map[string]interface{}{
				"dep1": "SUCCESS",
				"dep2": "RUNNING",
			},
		},
		{
			description: "multiple errors",
			deps: map[string]interface{}{
				"dep1": "SUCCESS",
				"dep2": fmt.Errorf("could not return within default timeout"),
				"dep3": fmt.Errorf("ERROR"),
			},
			expectedErrMsg: []string{"deployment dep2 failed due to could not return within default timeout",
				"deployment dep3 failed due to ERROR"},
			shouldErr: true,
		},
	}

	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			syncMap := &sync.Map{}
			for k, v := range test.deps {
				syncMap.Store(k, v)
			}
			err := getDeployStatus(syncMap)
			t.CheckError(test.shouldErr, err)
			for _, msg := range test.expectedErrMsg {
				t.CheckErrorContains(msg, err)
			}
		})
	}
}
