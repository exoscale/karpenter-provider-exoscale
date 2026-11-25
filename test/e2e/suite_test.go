package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

// Tests run from test/e2e.
const repoRoot = "../.."

// Set on every Ginkgo process.
var (
	exo        *egov3.Client
	kube       client.Client
	restConfig *rest.Config

	// Name of every Exoscale resource the run creates, and the cluster version.
	runName    string
	k8sVersion string
	clusterID  string
)

// Only set on process 1, which owns the cluster and the Karpenter process.
// Specs that use them must be Serial: Ginkgo runs those on process 1.
var (
	cluster   *infra.Cluster
	karpenter *karpenterProcess
)

// shared is what process 1 hands to every process once the cluster is up.
type shared struct {
	RunName, K8sVersion, ClusterID string
	AdminKubeconfig                []byte
	OrphanID                       egov3.UUID
	OrphanCreated                  time.Time
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Karpenter Exoscale e2e")
}

var _ = SynchronizedBeforeSuite(func(ctx SpecContext) []byte {
	var err error
	exo, err = infra.NewClient(ctx)
	Expect(err).NotTo(HaveOccurred())

	runName = os.Getenv("E2E_NAME")
	if runName == "" {
		runName = infra.NamePrefix + randomSuffix()
	}
	k8sVersion = os.Getenv("K8S_VERSION")
	if k8sVersion == "" {
		k8sVersion = latestVersion(ctx)
	}
	GinkgoWriter.Printf("cluster %s, Kubernetes %s, zone %s\n", runName, k8sVersion, infra.Zone())

	// Cleanups registered here run on process 1 after every process finished.
	DeferCleanup(func(ctx SpecContext) {
		Expect(infra.Destroy(ctx, exo, runName)).To(Succeed())
	}, NodeTimeout(30*time.Minute))

	cluster, err = infra.Create(ctx, exo, runName, k8sVersion, iamPolicy())
	Expect(err).NotTo(HaveOccurred())
	clusterID = cluster.ID.String()
	connect(cluster.AdminKubeconfig)

	createManifests(ctx,
		filepath.Join(repoRoot, "config/crd/bases/karpenter.exoscale.com_*.yaml"),
		filepath.Join(repoRoot, "vendor/sigs.k8s.io/karpenter/pkg/apis/crds/*.yaml"),
		filepath.Join(repoRoot, "config/rbac/role.yaml"),
	)
	Expect(kube.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-karpenter"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "manager-role"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "karpenter-" + clusterID}},
	})).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(kube.List(ctx, &apiv1.ExoscaleNodeClassList{})).To(Succeed())
		g.Expect(kube.List(ctx, &karpv1.NodePoolList{})).To(Succeed())
	}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

	karpenter = buildKarpenter(ctx)
	karpenter.start()
	DeferCleanup(func(ctx SpecContext) {
		// Let Karpenter terminate its own instances before it stops.
		Expect(kube.DeleteAllOf(ctx, &karpv1.NodePool{})).To(Succeed())
		Eventually(func(g Gomega) {
			var claims karpv1.NodeClaimList
			g.Expect(kube.List(ctx, &claims)).To(Succeed())
			g.Expect(claims.Items).To(BeEmpty())
		}).WithContext(ctx).WithTimeout(10 * time.Minute).Should(Succeed())
		karpenter.stop()
		_ = karpenter.log.Close()
	}, NodeTimeout(15*time.Minute))

	createOrphanInstance(ctx)

	data, err := json.Marshal(shared{
		RunName: runName, K8sVersion: k8sVersion, ClusterID: clusterID,
		AdminKubeconfig: cluster.AdminKubeconfig,
		OrphanID:        orphanID, OrphanCreated: orphanCreated,
	})
	Expect(err).NotTo(HaveOccurred())
	return data
}, func(ctx SpecContext, data []byte) {
	var s shared
	Expect(json.Unmarshal(data, &s)).To(Succeed())
	runName, k8sVersion, clusterID = s.RunName, s.K8sVersion, s.ClusterID
	orphanID, orphanCreated = s.OrphanID, s.OrphanCreated

	var err error
	exo, err = infra.NewClient(ctx)
	Expect(err).NotTo(HaveOccurred())
	connect(s.AdminKubeconfig)
}, NodeTimeout(45*time.Minute))

func connect(kubeconfig []byte) {
	Expect(apiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	var err error
	restConfig, err = clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	Expect(err).NotTo(HaveOccurred())
	kube, err = client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func latestVersion(ctx context.Context) string {
	versions, err := infra.Versions(ctx, exo)
	Expect(err).NotTo(HaveOccurred())
	Expect(versions).NotTo(BeEmpty())
	return slices.MaxFunc(versions, func(a, b string) int {
		c, err := utilversion.MustParseGeneric(a).Compare(b)
		Expect(err).NotTo(HaveOccurred())
		return c
	})
}

// iamPolicy reads the least-privilege policy from doc/iam.md, so the
// documented policy is the one Karpenter runs with.
func iamPolicy() egov3.IAMPolicy {
	doc, err := os.ReadFile(filepath.Join(repoRoot, "doc/iam.md"))
	Expect(err).NotTo(HaveOccurred())
	_, after, ok := strings.Cut(string(doc), "## Least-privilege IAM policy")
	Expect(ok).To(BeTrue(), "policy section missing from doc/iam.md")
	_, after, ok = strings.Cut(after, "```json\n")
	Expect(ok).To(BeTrue(), "policy JSON block missing from doc/iam.md")
	block, _, _ := strings.Cut(after, "```")

	var policy egov3.IAMPolicy
	Expect(json.Unmarshal([]byte(block), &policy)).To(Succeed())
	return policy
}

func createManifests(ctx context.Context, globs ...string) {
	for _, glob := range globs {
		files, err := filepath.Glob(glob)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(BeEmpty(), glob)
		for _, f := range files {
			data, err := os.ReadFile(f)
			Expect(err).NotTo(HaveOccurred())
			dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
			for {
				var obj unstructured.Unstructured
				err := dec.Decode(&obj.Object)
				if errors.Is(err, io.EOF) {
					break
				}
				Expect(err).NotTo(HaveOccurred(), f)
				if len(obj.Object) == 0 {
					continue
				}
				if err := kube.Create(ctx, &obj); !apierrors.IsAlreadyExists(err) {
					Expect(err).NotTo(HaveOccurred(), f)
				}
			}
		}
	}
}

// karpenterProcess runs the controller built from this checkout in the CI
// job, with the identity, IAM policy and settings SKS uses in production.
type karpenterProcess struct {
	bin, kubeconfig string
	log             *os.File
	cmd             *exec.Cmd
	exited          chan error
}

func buildKarpenter(ctx context.Context) *karpenterProcess {
	dir := GinkgoT().TempDir()
	k := &karpenterProcess{bin: filepath.Join(dir, "karpenter"), kubeconfig: filepath.Join(dir, "karpenter.kubeconfig")}
	build := exec.CommandContext(ctx, "go", "build", "-o", k.bin, "./cmd/karpenter-exoscale")
	build.Dir = repoRoot
	build.Stdout, build.Stderr = GinkgoWriter, GinkgoWriter
	Expect(build.Run()).To(Succeed())
	Expect(os.WriteFile(k.kubeconfig, cluster.KarpenterKubeconfig, 0o600)).To(Succeed())

	logPath := os.Getenv("KARPENTER_LOG")
	if logPath == "" {
		logPath = filepath.Join(os.TempDir(), runName+"-karpenter.log")
	}
	var err error
	k.log, err = os.Create(logPath)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("karpenter log: %s\n", logPath)
	return k
}

func (k *karpenterProcess) start() {
	fmt.Fprintf(k.log, "=== e2e: starting karpenter at %s\n", time.Now().Format(time.RFC3339))
	k.cmd = exec.Command(k.bin, "--leader-election-namespace=kube-system")
	k.cmd.Stdout, k.cmd.Stderr = k.log, k.log
	// Later entries win, so these override the CI's admin credentials.
	k.cmd.Env = append(os.Environ(),
		"EXOSCALE_API_KEY="+cluster.APIKey,
		"EXOSCALE_API_SECRET="+cluster.APISecret,
		"EXOSCALE_ZONE="+infra.Zone(),
		"EXOSCALE_SKS_CLUSTER_ID="+clusterID,
		"EXOSCALE_COMPUTE_INSTANCE_PREFIX="+runName+"-",
		"USE_SKS_DEFAULT_SECURITY_GROUP=true",
		"KUBECONFIG="+k.kubeconfig,
	)
	Expect(k.cmd.Start()).To(Succeed())
	k.exited = make(chan error, 1)
	go func() { k.exited <- k.cmd.Wait() }()
}

func (k *karpenterProcess) stop() {
	_ = k.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-k.exited:
	case <-time.After(30 * time.Second):
		_ = k.cmd.Process.Kill()
		<-k.exited
	}
}
