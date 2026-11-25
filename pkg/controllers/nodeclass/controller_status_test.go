package nodeclass

import (
	"context"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/template"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newStatusTestReconciler(t *testing.T, funcs interceptor.Funcs) (*ExoscaleNodeClassReconciler, *events.FakeRecorder, reconcile.Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := apiv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	nodeClass := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc", Finalizers: []string{Finalizer}},
		Spec:       apiv1.ExoscaleNodeClassSpec{TemplateID: "template"},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nodeClass).
		WithStatusSubresource(nodeClass).
		WithInterceptorFuncs(funcs).
		Build()
	recorder := events.NewFakeRecorder(10)
	return &ExoscaleNodeClassReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: recorder,
		TemplateResolver: &template.MockResolver{ResolveFunc: func(context.Context, *apiv1.ExoscaleNodeClass) (*template.Template, error) {
			return &template.Template{ID: "template"}, nil
		}},
		ExoscaleClient: &providers.MockClient{GetTemplateFunc: func(context.Context, egov3.UUID) (*egov3.Template, error) {
			return &egov3.Template{}, nil
		}},
	}, recorder, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nodeClass)}
}

func TestReconcileReturnsStatusPatchConflict(t *testing.T) {
	r, _, req := newStatusTestReconciler(t, interceptor.Funcs{
		SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
			return apierrors.NewConflict(schema.GroupResource{Group: apiv1.GroupVersion.Group, Resource: "exoscalenodeclasses"}, "nc", nil)
		},
	})

	if _, err := r.Reconcile(context.Background(), req); !apierrors.IsConflict(err) {
		t.Fatalf("Reconcile() error = %v, want a conflict so the NodeClass is requeued", err)
	}
}
