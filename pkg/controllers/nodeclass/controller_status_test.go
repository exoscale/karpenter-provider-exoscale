package nodeclass

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/awslabs/operatorpkg/status"
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

func drain(recorder *events.FakeRecorder) []string {
	var got []string
	for {
		select {
		case e := <-recorder.Events:
			got = append(got, e)
		default:
			return got
		}
	}
}

func TestReconcileEmitsReadyOnlyWhenBecomingReady(t *testing.T) {
	r, recorder, req := newStatusTestReconciler(t, interceptor.Funcs{})

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := drain(recorder); len(got) != 1 || !strings.Contains(got[0], "Ready") {
		t.Fatalf("first reconcile events = %q, want one Ready event", got)
	}

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := drain(recorder); len(got) != 0 {
		t.Fatalf("resync events = %q, want none", got)
	}
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

// A failed step must keep Ready false even when later steps succeed.
func TestReconcileKeepsReadyFalseWhenAStepFails(t *testing.T) {
	r, _, req := newStatusTestReconciler(t, interceptor.Funcs{})
	r.ExoscaleClient.(*providers.MockClient).ListVpcsFunc = func(context.Context) (*egov3.ListVpcsResponse, error) {
		return nil, errors.New("forbidden")
	}
	ctx := context.Background()
	nodeClass := &apiv1.ExoscaleNodeClass{}
	if err := r.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		t.Fatal(err)
	}
	nodeClass.Spec.VpcSelector = &apiv1.SelectorTerms{Name: "vpc"}
	nodeClass.Spec.VpcSubnetSelectorTerms = []apiv1.SelectorTerms{{Name: "subnet"}}
	if err := r.Update(ctx, nodeClass); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		t.Fatal(err)
	}
	if nodeClass.StatusConditions().IsTrue(status.ConditionReady) {
		t.Fatalf("Ready = true with a failed VPC subnet resolution: %+v", nodeClass.Status.Conditions)
	}
	if !nodeClass.StatusConditions().Get(ConditionVpcSubnetsResolved).IsFalse() {
		t.Fatalf("%s is not false: %+v", ConditionVpcSubnetsResolved, nodeClass.Status.Conditions)
	}
}
