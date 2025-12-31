package runtime

import (
	"context"
	"testing"

	"github.com/ptone/scion-agent/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesRuntime_List(t *testing.T) {
	// Create a fake clientset
	clientset := k8sfake.NewSimpleClientset()
	
	// Create a pod that mimics what we expect
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "default",
			Labels: map[string]string{
				"scion.name": "test-agent",
				"scion.template": "test-template",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: "test-image",
				},
			},
		},
	}
	
	_, err := clientset.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create pod: %v", err)
	}

	// Create a generic scheme for dynamic client
	scheme := k8sruntime.NewScheme()

	fc := fake.NewSimpleDynamicClient(scheme)

	client := k8s.NewTestClient(fc, clientset)
	r := NewKubernetesRuntime(client)

	agents, err := r.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
		return
	}

	if agents[0].ID != "test-agent" {
		t.Errorf("expected ID test-agent, got %s", agents[0].ID)
	}

	if agents[0].ContainerStatus != "Running" {
		t.Errorf("expected container status Running, got %s", agents[0].ContainerStatus)
	}

	if agents[0].Image != "test-image" {
		t.Errorf("expected image test-image, got %s", agents[0].Image)
	}
}
