package instance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestCreateHTTPRetries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		createCode int
		wantError  bool
		wantGets   int32
	}{
		{"accepted create with lost response is not retried", http.StatusBadGateway, true, 0},
		{"successful create retains GET retries", http.StatusOK, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts, gets atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/instance-type":
					fmt.Fprint(w, `{"instance-types":[{"id":"test-type","family":"standard","size":"medium","cpus":2,"memory":4294967296,"authorized":true,"zones":["test-zone"]}]}`)
				case r.Method == http.MethodPost && r.URL.Path == "/instance":
					// Each accepted POST represents a new instance, even if its
					// response fails. A retry would return a different instance ID.
					n := posts.Add(1)
					if n == 1 && tc.createCode != http.StatusOK {
						http.Error(w, "response lost after instance creation", tc.createCode)
						return
					}
					fmt.Fprintf(w, `{"id":"operation-%d","reference":{"id":"instance-%d"}}`, n, n)
				case r.Method == http.MethodGet && r.URL.Path == "/instance/instance-1":
					if gets.Add(1) == 1 {
						http.Error(w, "temporary read failure", http.StatusBadGateway)
						return
					}
					fmt.Fprint(w, `{"id":"instance-1","name":"test-claim","instance-type":{"id":"test-type"},"template":{"id":"test-template"},"disk-size":20}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			exoClient, err := egov3.NewClient(
				credentials.NewStaticCredentials("test-key", "test-secret"),
				egov3.ClientOptWithEndpoint(egov3.Endpoint(server.URL)),
			)
			require.NoError(t, err)
			types, err := instancetype.NewExoscaleProvider(ctx, exoClient, "test-zone")
			require.NoError(t, err)
			require.NoError(t, types.Refresh(ctx))

			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: metav1.NamespaceSystem},
				Data:       map[string]string{"ca.crt": "test-ca"},
			}).Build()
			p := NewProvider(exoClient, kubeClient, types,
				template.NewResolver(exoClient, "test-zone", nil), userdata.NewProvider(kubeClient),
				&Options{Zone: "test-zone", ClusterID: "test-cluster", ClusterEndpoint: "https://example.invalid", ClusterDomain: "cluster.local"},
			)
			nodeClass := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{TemplateID: "test-template", DiskSize: 20},
			}
			nodeClaim := &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "test-claim"},
				Spec: karpenterv1.NodeClaimSpec{
					Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{{
						Key: corev1.LabelInstanceTypeStable, Operator: corev1.NodeSelectorOpIn, Values: []string{"standard.medium"},
					}},
				},
			}

			created, err := p.Create(ctx, nodeClass, nodeClaim, "abcdef.0123456789abcdef")
			require.Equal(t, int32(1), posts.Load(), "instance creation must issue exactly one POST")
			require.Equal(t, tc.wantGets, gets.Load())
			if tc.wantError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "failed to create instance")
				require.Nil(t, created)
			} else {
				require.NoError(t, err)
				require.Equal(t, "instance-1", created.ID)
			}
		})
	}
}
