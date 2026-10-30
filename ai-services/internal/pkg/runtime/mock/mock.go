// Package mock provides an in-memory runtime.Runtime for local development.
//
// It lets a worker register with the catalog and serve commands from a machine
// that has no Podman VM, OpenShift cluster, Spyre cards, Caddy or Helm — for
// example a developer laptop. Nothing is executed: pods, secrets and Helm
// releases are only recorded in memory, and every pod reports as running and
// healthy so readiness checks pass.
//
// The mock mirrors the real runtimes' capability split: namespace, route, CRD
// and PVC operations succeed for RuntimeTypeOpenShift and return
// "unsupported method" for RuntimeTypePodman, exactly like the real clients.
package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// EnvMockRuntime enables the mock runtime when set to "true".
const EnvMockRuntime = "AI_SERVICES_MOCK_RUNTIME"

const (
	mockNamespace   = "mock-namespace"
	releaseLabelKey = "ai-services.io/service"
	podRunning      = "Running"
	podExited       = "Exited"
	ctrRunning      = "running"
	ctrCreated      = "created"
	infraSuffix     = "infra"
	decoderBuf      = 4096

	fakeCPUs        = 16
	fakeAvailCPUs   = 12.0
	fakeMemBytes    = 64 << 30
	fakeMemAvail    = 48 << 30
	fakeSpyreCards  = 8
	fakePodCPU      = 0.5
	fakePodMemBytes = 512 << 20
)

// Enabled reports whether the mock runtime has been requested via the environment.
func Enabled() bool {
	return utils.GetEnv(EnvMockRuntime, "") == "true"
}

// Client is an in-memory runtime.Runtime.
type Client struct {
	rtType types.RuntimeType

	mu       sync.Mutex
	pods     map[string]*types.Pod        // keyed by pod name
	secrets  map[string]map[string]string // name → labels
	volumes  map[string]struct{}
	images   map[string]struct{}
	releases map[string]struct{} // "<namespace>/<release>"
	routes   map[string]types.Route
}

// New returns a mock runtime that identifies itself as rtType.
func New(rtType types.RuntimeType) *Client {
	return &Client{
		rtType:   rtType,
		pods:     map[string]*types.Pod{},
		secrets:  map[string]map[string]string{},
		volumes:  map[string]struct{}{},
		images:   map[string]struct{}{},
		releases: map[string]struct{}{},
		routes:   map[string]types.Route{},
	}
}

// Type implements runtime.Runtime.
func (c *Client) Type() types.RuntimeType { return c.rtType }

func (c *Client) isOpenShift() bool { return c.rtType == types.RuntimeTypeOpenShift }

// requireOpenShift mirrors the real Podman client, which rejects OpenShift-only calls.
func (c *Client) requireOpenShift() error {
	if !c.isOpenShift() {
		return fmt.Errorf("unsupported method")
	}

	return nil
}

// ─── Images ──────────────────────────────────────────────────────────────────

// ListImages implements runtime.Runtime.
func (c *Client) ListImages(_ context.Context) ([]types.Image, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]types.Image, 0, len(c.images))
	for img := range c.images {
		out = append(out, types.Image{RepoTags: []string{img}})
	}

	return out, nil
}

// PullImage implements runtime.Runtime.
func (c *Client) PullImage(_ context.Context, image string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.images[image] = struct{}{}

	return nil
}

// ─── Pods ────────────────────────────────────────────────────────────────────

// kubeObject is the subset of a Kubernetes manifest the mock cares about.
type kubeObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              corev1.PodSpec `json:"spec"`
}

// ListPods implements runtime.Runtime. Supports the "label" and "name" filters.
func (c *Client) ListPods(_ context.Context, filters map[string][]string) ([]types.Pod, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]types.Pod, 0, len(c.pods))

	for _, p := range c.pods {
		if matchLabels(p.Labels, filters["label"]) && matchName(p.Name, filters["name"]) {
			out = append(out, clonePod(p))
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// CreatePod implements runtime.Runtime. It parses the kube-play YAML body:
// Pod documents become running pods, Secret documents are recorded as secrets,
// PersistentVolumeClaim documents and the volumes a pod declares (PVC claims and
// secret-backed volumes) are recorded as volumes, everything else is ignored.
func (c *Client) CreatePod(_ context.Context, body io.Reader, opts map[string]string) ([]types.Pod, error) {
	start := opts["start"] != constants.PodStartOff
	dec := k8syaml.NewYAMLOrJSONDecoder(body, decoderBuf)

	c.mu.Lock()
	defer c.mu.Unlock()

	var created []types.Pod

	for {
		var obj kubeObject
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("mock runtime: decode kube manifest: %w", err)
		}

		switch obj.Kind {
		case "Pod":
			pod := buildPod(&obj, start)
			c.pods[pod.Name] = pod
			created = append(created, clonePod(pod))
			c.recordVolumes(obj.Spec.Volumes)
		case "Secret":
			c.secrets[obj.Name] = copyMap(obj.Labels)
		case "PersistentVolumeClaim":
			c.volumes[obj.Name] = struct{}{}
		}
	}

	return created, nil
}

// recordVolumes records the named volumes a pod declares. Secret-backed volumes
// are included because the catalog lists them in the pod's ai-services.io/volume
// label and validates them with VolumeExists. Caller holds c.mu.
func (c *Client) recordVolumes(vols []corev1.Volume) {
	for i := range vols {
		switch {
		case vols[i].PersistentVolumeClaim != nil:
			c.volumes[vols[i].PersistentVolumeClaim.ClaimName] = struct{}{}
		case vols[i].Secret != nil:
			c.volumes[vols[i].Secret.SecretName] = struct{}{}
		}
	}
}

// DeletePod implements runtime.Runtime.
func (c *Client) DeletePod(_ context.Context, id string, _ *bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.findPod(id)
	if p == nil {
		return fmt.Errorf("no such pod: %s", id)
	}

	delete(c.pods, p.Name)

	return nil
}

// StopPod implements runtime.Runtime.
func (c *Client) StopPod(_ context.Context, id string) error {
	return c.setPodState(id, podExited, ctrCreated)
}

// StartPod implements runtime.Runtime.
func (c *Client) StartPod(_ context.Context, id string) error {
	return c.setPodState(id, podRunning, ctrRunning)
}

func (c *Client) setPodState(id, podState, ctrState string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.findPod(id)
	if p == nil {
		return fmt.Errorf("no such pod: %s", id)
	}

	p.Status, p.State = podState, podState
	for i := range p.Containers {
		p.Containers[i].Status = ctrState
	}

	return nil
}

// InspectPod implements runtime.Runtime.
func (c *Client) InspectPod(_ context.Context, nameOrID string) (*types.Pod, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.findPod(nameOrID)
	if p == nil {
		return nil, fmt.Errorf("no such pod: %s", nameOrID)
	}

	out := clonePod(p)

	return &out, nil
}

// PodExists implements runtime.Runtime.
func (c *Client) PodExists(_ context.Context, nameOrID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.findPod(nameOrID) != nil, nil
}

// PodLogs implements runtime.Runtime.
func (c *Client) PodLogs(_ context.Context, nameOrID string, _ bool) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.findPod(nameOrID)
	if p == nil {
		return nil, fmt.Errorf("no such pod: %s", nameOrID)
	}

	return []string{fmt.Sprintf("[mock] pod %s is running", p.Name)}, nil
}

// GetPodResources implements runtime.Runtime.
func (c *Client) GetPodResources(_ context.Context, nameOrID string) (*types.PodResources, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.findPod(nameOrID) == nil {
		return nil, fmt.Errorf("no such pod: %s", nameOrID)
	}

	return &types.PodResources{CPU: fakePodCPU, MemUsage: fakePodMemBytes}, nil
}

// GetNamespace implements runtime.Runtime.
func (c *Client) GetNamespace(_ context.Context) (string, error) {
	if err := c.requireOpenShift(); err != nil {
		return "", err
	}

	return mockNamespace, nil
}

// findPod resolves a pod by ID, exact name, or name prefix. Caller holds c.mu.
func (c *Client) findPod(nameOrID string) *types.Pod {
	if p, ok := c.pods[nameOrID]; ok {
		return p
	}

	for _, p := range c.pods {
		if p.ID == nameOrID || (nameOrID != "" && strings.HasPrefix(p.Name, nameOrID)) {
			return p
		}
	}

	return nil
}

// ─── Secrets ─────────────────────────────────────────────────────────────────

// ListSecrets implements runtime.Runtime. Supports the "name" filter.
func (c *Client) ListSecrets(_ context.Context, filters map[string][]string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := []string{}

	for name := range c.secrets {
		if matchName(name, filters["name"]) {
			out = append(out, name)
		}
	}

	sort.Strings(out)

	return out, nil
}

// DeleteSecret implements runtime.Runtime.
func (c *Client) DeleteSecret(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.secrets[name]; !ok {
		return fmt.Errorf("%s: %s", constants.ErrSecretNotFound, name)
	}

	delete(c.secrets, name)

	return nil
}

// SecretExists implements runtime.Runtime.
func (c *Client) SecretExists(_ context.Context, nameOrID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.secrets[nameOrID]

	return ok, nil
}

// UpdateSecret implements runtime.Runtime.
func (c *Client) UpdateSecret(_ context.Context, name, _ string, _ map[string][]byte) error {
	if err := c.requireOpenShift(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.secrets[name]; !ok {
		c.secrets[name] = map[string]string{}
	}

	return nil
}

// DeleteSecrets implements runtime.Runtime. Removes every secret matching labelSelector.
func (c *Client) DeleteSecrets(_ context.Context, labelSelector string) error {
	if err := c.requireOpenShift(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	selectors := splitSelector(labelSelector)
	for name, labels := range c.secrets {
		if matchLabels(labels, selectors) {
			delete(c.secrets, name)
		}
	}

	return nil
}

// ─── Volumes ─────────────────────────────────────────────────────────────────

// DeleteVolume implements runtime.Runtime.
func (c *Client) DeleteVolume(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.volumes, name)

	return nil
}

// VolumeExists implements runtime.Runtime.
func (c *Client) VolumeExists(_ context.Context, nameOrID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.volumes[nameOrID]

	return ok, nil
}

// ─── Containers ──────────────────────────────────────────────────────────────

// InspectContainer implements runtime.Runtime.
func (c *Client) InspectContainer(_ context.Context, nameOrID string) (*types.Container, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctr := c.findContainer(nameOrID)
	if ctr == nil {
		return nil, fmt.Errorf("no such container: %s", nameOrID)
	}

	out := *ctr

	return &out, nil
}

// ContainerExists implements runtime.Runtime.
func (c *Client) ContainerExists(_ context.Context, nameOrID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.findContainer(nameOrID) != nil, nil
}

// ContainerLogs implements runtime.Runtime.
func (c *Client) ContainerLogs(_ context.Context, _ string) error { return nil }

// ExecInContainerWithCmd implements runtime.Runtime. Nothing is executed.
func (c *Client) ExecInContainerWithCmd(_ context.Context, _, _ string, _ []string) (string, error) {
	return "", nil
}

func (c *Client) findContainer(nameOrID string) *types.Container {
	for _, p := range c.pods {
		for i := range p.Containers {
			if p.Containers[i].ID == nameOrID || p.Containers[i].Name == nameOrID {
				return &p.Containers[i]
			}
		}
	}

	return nil
}

// ─── OpenShift-only operations ───────────────────────────────────────────────

// ListRoutes implements runtime.Runtime for OpenShift mock.
func (c *Client) ListRoutes(_ context.Context, labelSelector string) ([]types.Route, error) {
	if err := c.requireOpenShift(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	selectors := splitSelector(labelSelector)
	var out []types.Route
	for _, r := range c.routes {
		if matchLabels(r.Labels, selectors) {
			out = append(out, r)
		}
	}

	return out, nil
}

// ListCRD implements runtime.Runtime. The mock never has custom resources.
func (c *Client) ListCRD(_ context.Context, _ *unstructured.UnstructuredList, _ map[string][]string) ([]types.CRDResource, error) {
	if err := c.requireOpenShift(); err != nil {
		return nil, err
	}

	return []types.CRDResource{}, nil
}

// DeleteNamespace implements runtime.Runtime.
func (c *Client) DeleteNamespace(_ context.Context, _ string) error {
	return c.requireOpenShift()
}

// DeletePVCs implements runtime.Runtime.
func (c *Client) DeletePVCs(_ context.Context, _ string) error {
	return c.requireOpenShift()
}

// WaitForInferenceServiceReady implements runtime.Runtime.
func (c *Client) WaitForInferenceServiceReady(_ context.Context, _ string) error { return nil }

// ─── System ──────────────────────────────────────────────────────────────────

// GetSystemInfo implements runtime.Runtime with fixed fake capacity.
func (c *Client) GetSystemInfo(_ context.Context) (*models.SystemInfo, error) {
	return &models.SystemInfo{
		CPU:    &models.CPUInfo{Total: fakeCPUs, Available: fakeAvailCPUs},
		Memory: &models.MemoryInfo{TotalBytes: fakeMemBytes, AvailableBytes: fakeMemAvail},
		Accelerators: map[string]*models.AcceleratorInfo{
			constants.SpyreResourceName: {Total: fakeSpyreCards, Available: fakeSpyreCards},
		},
	}, nil
}

// FreeSpyreCards returns fake PCI addresses for the mock's Spyre cards.
func (c *Client) FreeSpyreCards() []string {
	cards := make([]string, 0, fakeSpyreCards)
	for i := range fakeSpyreCards {
		cards = append(cards, fmt.Sprintf("0000:%02x:00.0", i+1))
	}

	return cards
}

// ─── Helm (handled by the dispatcher; recorded here) ─────────────────────────

// InstallRelease records a Helm release. When templateID is set it also creates
// one running, healthy pod for the release labelled ai-services.io/template, which
// is how the catalog finds the pods of a deployed service or component. A repeated
// install (upgrade) replaces the pod. Releases without a template ID (e.g. serving
// runtimes) get no pod.
func (c *Client) InstallRelease(namespace, release, templateID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.releases[namespace+"/"+release] = struct{}{}

	if templateID == "" {
		return
	}

	name := releasePodName(release)
	obj := &kubeObject{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			constants.ApplicationTemplateKey: templateID,
			releaseLabelKey:                  release,
		},
	}}
	obj.Spec.Containers = []corev1.Container{{Name: "main", LivenessProbe: &corev1.Probe{}}}

	// Kubernetes pods have no infra container, and the OpenShift sync requires
	// every container to be healthy.
	pod := buildPod(obj, true)
	pod.Containers = pod.Containers[1:]
	pod.InfraContainerID = ""

	c.pods[name] = pod

	if c.isOpenShift() {
		apiRouteName := release + "-api"
		c.routes[apiRouteName] = types.Route{
			Name:        apiRouteName,
			HostPort:    fmt.Sprintf("%s-api.apps.mock.local", release),
			TargetPort:  "8080",
			ServiceName: fmt.Sprintf("%s-svc", release),
			Labels: map[string]string{
				releaseLabelKey:                release,
				constants.EndpointTypeLabelKey: "api",
			},
		}

		uiRouteName := release + "-ui"
		c.routes[uiRouteName] = types.Route{
			Name:        uiRouteName,
			HostPort:    fmt.Sprintf("%s-ui.apps.mock.local", release),
			TargetPort:  "8080",
			ServiceName: fmt.Sprintf("%s-ui-svc", release),
			Labels: map[string]string{
				releaseLabelKey:                release,
				constants.EndpointTypeLabelKey: "ui",
			},
		}
	}
}

// UninstallRelease forgets a Helm release, the pod, and routes created for it.
func (c *Client) UninstallRelease(namespace, release string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.releases, namespace+"/"+release)
	delete(c.pods, releasePodName(release))
	delete(c.routes, release+"-api")
	delete(c.routes, release+"-ui")
}

func releasePodName(release string) string { return release + "-mock" }

// ReleaseExists reports whether a Helm release has been recorded.
func (c *Client) ReleaseExists(namespace, release string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.releases[namespace+"/"+release]

	return ok
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildPod turns a Pod manifest into a running (or created, when start is false) pod.
// Like podman, it adds an infra container alongside the spec containers.
func buildPod(obj *kubeObject, start bool) *types.Pod {
	podState, ctrState := podRunning, ctrRunning
	if !start {
		podState, ctrState = podExited, ctrCreated
	}

	podID := hashID(obj.Name)
	containers := make([]types.Container, 0, 1+len(obj.Spec.Containers))
	containers = append(containers, types.Container{
		ID:     hashID(obj.Name + "/" + infraSuffix),
		Name:   obj.Name + "-" + infraSuffix,
		Status: ctrState,
	})

	ports := map[string][]string{}

	for i := range obj.Spec.Containers {
		sc := &obj.Spec.Containers[i]

		ctr := types.Container{
			ID:          hashID(obj.Name + "/" + sc.Name),
			Name:        obj.Name + "-" + sc.Name,
			Status:      ctrState,
			Annotations: copyMap(obj.Annotations),
			Env:         envMap(sc.Env),
		}
		if sc.LivenessProbe != nil {
			ctr.Health = string(constants.Ready)
		}

		containers = append(containers, ctr)

		for _, port := range sc.Ports {
			if port.HostPort != 0 {
				key := fmt.Sprintf("%d/tcp", port.ContainerPort)
				ports[key] = append(ports[key], fmt.Sprintf("%d", port.HostPort))
			}
		}
	}

	return &types.Pod{
		ID:               podID,
		Name:             obj.Name,
		Status:           podState,
		State:            podState,
		Labels:           copyMap(obj.Labels),
		Containers:       containers,
		Created:          time.Now(),
		Ports:            ports,
		InfraContainerID: containers[0].ID,
	}
}

func clonePod(p *types.Pod) types.Pod {
	out := *p
	out.Labels = copyMap(p.Labels)
	out.Containers = append([]types.Container(nil), p.Containers...)

	return out
}

func envMap(env []corev1.EnvVar) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		out[e.Name] = e.Value
	}

	return out
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func hashID(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

// splitSelector splits a comma-separated label selector into "k=v" / "k" terms.
func splitSelector(selector string) []string {
	if selector == "" {
		return nil
	}

	return strings.Split(selector, ",")
}

// matchLabels reports whether labels satisfy every "k=v" (or bare "k") selector.
func matchLabels(labels map[string]string, selectors []string) bool {
	for _, s := range selectors {
		key, val, hasVal := strings.Cut(strings.TrimSpace(s), "=")

		got, ok := labels[key]
		if !ok || (hasVal && got != val) {
			return false
		}
	}

	return true
}

// matchName reports whether name equals one of names, or names is empty.
func matchName(name string, names []string) bool {
	if len(names) == 0 {
		return true
	}

	for _, n := range names {
		if n == name {
			return true
		}
	}

	return false
}
