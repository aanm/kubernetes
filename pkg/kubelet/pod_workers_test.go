/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package kubelet

import (
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/record"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	kubetypes "k8s.io/kubernetes/pkg/kubelet/types"
	"k8s.io/kubernetes/pkg/kubelet/util/queue"
	"k8s.io/kubernetes/pkg/types"
)

func newPod(uid, name string) *api.Pod {
	return &api.Pod{
		ObjectMeta: api.ObjectMeta{
			UID:  types.UID(uid),
			Name: name,
		},
	}
}

func createPodWorkers() (*podWorkers, map[types.UID][]string) {
	lock := sync.Mutex{}
	processed := make(map[types.UID][]string)
	fakeRecorder := &record.FakeRecorder{}
	fakeRuntime := &kubecontainer.FakeRuntime{}
	fakeRuntimeCache := kubecontainer.NewFakeRuntimeCache(fakeRuntime)
	podWorkers := newPodWorkers(
		fakeRuntimeCache,
		func(pod *api.Pod, mirrorPod *api.Pod, runningPod kubecontainer.Pod, updateType kubetypes.SyncPodType) error {
			func() {
				lock.Lock()
				defer lock.Unlock()
				processed[pod.UID] = append(processed[pod.UID], pod.Name)
			}()
			return nil
		},
		fakeRecorder,
		queue.NewBasicWorkQueue(),
		time.Second,
		time.Second,
		nil,
	)
	return podWorkers, processed
}

func drainWorkers(podWorkers *podWorkers, numPods int) {
	for {
		stillWorking := false
		podWorkers.podLock.Lock()
		for i := 0; i < numPods; i++ {
			if podWorkers.isWorking[types.UID(string(i))] {
				stillWorking = true
			}
		}
		podWorkers.podLock.Unlock()
		if !stillWorking {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestUpdatePod(t *testing.T) {
	podWorkers, processed := createPodWorkers()

	// Check whether all pod updates will be processed.
	numPods := 20
	for i := 0; i < numPods; i++ {
		for j := i; j < numPods; j++ {
			podWorkers.UpdatePod(newPod(string(j), string(i)), nil, kubetypes.SyncPodCreate, func() {})
		}
	}
	drainWorkers(podWorkers, numPods)

	if len(processed) != 20 {
		t.Errorf("Not all pods processed: %v", len(processed))
		return
	}
	for i := 0; i < numPods; i++ {
		uid := types.UID(i)
		if len(processed[uid]) < 1 || len(processed[uid]) > i+1 {
			t.Errorf("Pod %v processed %v times", i, len(processed[uid]))
			continue
		}

		first := 0
		last := len(processed[uid]) - 1
		if processed[uid][first] != string(0) {
			t.Errorf("Pod %v: incorrect order %v, %v", i, first, processed[uid][first])

		}
		if processed[uid][last] != string(i) {
			t.Errorf("Pod %v: incorrect order %v, %v", i, last, processed[uid][last])
		}
	}
}

func TestForgetNonExistingPodWorkers(t *testing.T) {
	podWorkers, _ := createPodWorkers()

	numPods := 20
	for i := 0; i < numPods; i++ {
		podWorkers.UpdatePod(newPod(string(i), "name"), nil, kubetypes.SyncPodUpdate, func() {})
	}
	drainWorkers(podWorkers, numPods)

	if len(podWorkers.podUpdates) != numPods {
		t.Errorf("Incorrect number of open channels %v", len(podWorkers.podUpdates))
	}

	desiredPods := map[types.UID]empty{}
	desiredPods[types.UID(2)] = empty{}
	desiredPods[types.UID(14)] = empty{}
	podWorkers.ForgetNonExistingPodWorkers(desiredPods)
	if len(podWorkers.podUpdates) != 2 {
		t.Errorf("Incorrect number of open channels %v", len(podWorkers.podUpdates))
	}
	if _, exists := podWorkers.podUpdates[types.UID(2)]; !exists {
		t.Errorf("No updates channel for pod 2")
	}
	if _, exists := podWorkers.podUpdates[types.UID(14)]; !exists {
		t.Errorf("No updates channel for pod 14")
	}

	podWorkers.ForgetNonExistingPodWorkers(map[types.UID]empty{})
	if len(podWorkers.podUpdates) != 0 {
		t.Errorf("Incorrect number of open channels %v", len(podWorkers.podUpdates))
	}
}

type simpleFakeKubelet struct {
	pod        *api.Pod
	mirrorPod  *api.Pod
	runningPod kubecontainer.Pod
	wg         sync.WaitGroup
}

func (kl *simpleFakeKubelet) syncPod(pod *api.Pod, mirrorPod *api.Pod, runningPod kubecontainer.Pod, updateType kubetypes.SyncPodType) error {
	kl.pod, kl.mirrorPod, kl.runningPod = pod, mirrorPod, runningPod
	return nil
}

func (kl *simpleFakeKubelet) syncPodWithWaitGroup(pod *api.Pod, mirrorPod *api.Pod, runningPod kubecontainer.Pod, updateType kubetypes.SyncPodType) error {
	kl.pod, kl.mirrorPod, kl.runningPod = pod, mirrorPod, runningPod
	kl.wg.Done()
	return nil
}

// byContainerName sort the containers in a running pod by their names.
type byContainerName kubecontainer.Pod

func (b byContainerName) Len() int { return len(b.Containers) }

func (b byContainerName) Swap(i, j int) {
	b.Containers[i], b.Containers[j] = b.Containers[j], b.Containers[i]
}

func (b byContainerName) Less(i, j int) bool {
	return b.Containers[i].Name < b.Containers[j].Name
}

// TestFakePodWorkers verifies that the fakePodWorkers behaves the same way as the real podWorkers
// for their invocation of the syncPodFn.
func TestFakePodWorkers(t *testing.T) {
	fakeRecorder := &record.FakeRecorder{}
	fakeRuntime := &kubecontainer.FakeRuntime{}
	fakeRuntimeCache := kubecontainer.NewFakeRuntimeCache(fakeRuntime)

	kubeletForRealWorkers := &simpleFakeKubelet{}
	kubeletForFakeWorkers := &simpleFakeKubelet{}

	realPodWorkers := newPodWorkers(fakeRuntimeCache, kubeletForRealWorkers.syncPodWithWaitGroup, fakeRecorder, queue.NewBasicWorkQueue(), time.Second, time.Second, nil)
	fakePodWorkers := &fakePodWorkers{kubeletForFakeWorkers.syncPod, fakeRuntimeCache, t}

	tests := []struct {
		pod                    *api.Pod
		mirrorPod              *api.Pod
		podList                []*kubecontainer.Pod
		containersInRunningPod int
	}{
		{
			&api.Pod{},
			&api.Pod{},
			[]*kubecontainer.Pod{},
			0,
		},
		{
			&api.Pod{
				ObjectMeta: api.ObjectMeta{
					UID:       "12345678",
					Name:      "foo",
					Namespace: "new",
				},
			},
			&api.Pod{
				ObjectMeta: api.ObjectMeta{
					UID:       "12345678",
					Name:      "fooMirror",
					Namespace: "new",
				},
			},
			[]*kubecontainer.Pod{
				{
					ID:        "12345678",
					Name:      "foo",
					Namespace: "new",
					Containers: []*kubecontainer.Container{
						{
							Name: "fooContainer",
						},
					},
				},
				{
					ID:        "12345678",
					Name:      "fooMirror",
					Namespace: "new",
					Containers: []*kubecontainer.Container{
						{
							Name: "fooContainerMirror",
						},
					},
				},
			},
			1,
		},
		{
			&api.Pod{
				ObjectMeta: api.ObjectMeta{
					UID:       "98765",
					Name:      "bar",
					Namespace: "new",
				},
			},
			&api.Pod{
				ObjectMeta: api.ObjectMeta{
					UID:       "98765",
					Name:      "barMirror",
					Namespace: "new",
				},
			},
			[]*kubecontainer.Pod{
				{
					ID:        "98765",
					Name:      "bar",
					Namespace: "new",
					Containers: []*kubecontainer.Container{
						{
							Name: "barContainer0",
						},
						{
							Name: "barContainer1",
						},
					},
				},
				{
					ID:        "98765",
					Name:      "barMirror",
					Namespace: "new",
					Containers: []*kubecontainer.Container{
						{
							Name: "barContainerMirror0",
						},
						{
							Name: "barContainerMirror1",
						},
					},
				},
			},
			2,
		},
	}

	for i, tt := range tests {
		kubeletForRealWorkers.wg.Add(1)

		fakeRuntime.PodList = tt.podList
		realPodWorkers.UpdatePod(tt.pod, tt.mirrorPod, kubetypes.SyncPodUpdate, func() {})
		fakePodWorkers.UpdatePod(tt.pod, tt.mirrorPod, kubetypes.SyncPodUpdate, func() {})

		kubeletForRealWorkers.wg.Wait()

		if !reflect.DeepEqual(kubeletForRealWorkers.pod, kubeletForFakeWorkers.pod) {
			t.Errorf("%d: Expected: %#v, Actual: %#v", i, kubeletForRealWorkers.pod, kubeletForFakeWorkers.pod)
		}

		if !reflect.DeepEqual(kubeletForRealWorkers.mirrorPod, kubeletForFakeWorkers.mirrorPod) {
			t.Errorf("%d: Expected: %#v, Actual: %#v", i, kubeletForRealWorkers.mirrorPod, kubeletForFakeWorkers.mirrorPod)
		}

		if tt.containersInRunningPod != len(kubeletForFakeWorkers.runningPod.Containers) {
			t.Errorf("%d: Expected: %#v, Actual: %#v", i, tt.containersInRunningPod, len(kubeletForFakeWorkers.runningPod.Containers))
		}

		sort.Sort(byContainerName(kubeletForRealWorkers.runningPod))
		sort.Sort(byContainerName(kubeletForFakeWorkers.runningPod))
		if !reflect.DeepEqual(kubeletForRealWorkers.runningPod, kubeletForFakeWorkers.runningPod) {
			t.Errorf("%d: Expected: %#v, Actual: %#v", i, kubeletForRealWorkers.runningPod, kubeletForFakeWorkers.runningPod)
		}
	}
}
