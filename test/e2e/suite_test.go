package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

// Tests run from test/e2e.
const repoRoot = "../.."

var (
	exo     *egov3.Client
	cluster *infra.Cluster
	kube    client.Client

	// Name of every Exoscale resource the run creates, and the cluster version.
	runName    string
	k8sVersion string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Karpenter Exoscale e2e")
}

var _ = BeforeSuite(func(ctx SpecContext) {
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

	DeferCleanup(func(ctx SpecContext) {
		Expect(infra.Destroy(ctx, exo, runName)).To(Succeed())
	}, NodeTimeout(30*time.Minute))

	cluster, err = infra.Create(ctx, exo, runName, k8sVersion, iamPolicy())
	Expect(err).NotTo(HaveOccurred())

	Expect(apiv1.AddToScheme(scheme.Scheme)).To(Succeed())
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(cluster.AdminKubeconfig)
	Expect(err).NotTo(HaveOccurred())
	kube, err = client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	createManifests(ctx,
		filepath.Join(repoRoot, "config/crd/bases/*.yaml"),
		filepath.Join(repoRoot, "vendor/sigs.k8s.io/karpenter/pkg/apis/crds/*.yaml"),
		filepath.Join(repoRoot, "config/rbac/role.yaml"),
	)
	Expect(kube.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-karpenter"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "manager-role"},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: "karpenter-" + cluster.ID.String()}},
	})).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(kube.List(ctx, &apiv1.ExoscaleNodeClassList{})).To(Succeed())
		g.Expect(kube.List(ctx, &karpv1.NodePoolList{})).To(Succeed())
	}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

	stop := startKarpenter(ctx, runName)
	createOrphanInstance(ctx)
	DeferCleanup(func(ctx SpecContext) {
		// Let Karpenter terminate its own instances before it stops.
		Expect(kube.DeleteAllOf(ctx, &karpv1.NodePool{})).To(Succeed())
		Eventually(func(g Gomega) {
			var claims karpv1.NodeClaimList
			g.Expect(kube.List(ctx, &claims)).To(Succeed())
			g.Expect(claims.Items).To(BeEmpty())
		}).WithContext(ctx).WithTimeout(10 * time.Minute).Should(Succeed())
		stop()
	}, NodeTimeout(15*time.Minute))
}, NodeTimeout(45*time.Minute))

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

// startKarpenter builds the controller from this checkout and runs it in the
// CI job with the identity, IAM policy and settings SKS uses in production.
func startKarpenter(ctx context.Context, name string) (stop func()) {
	dir := GinkgoT().TempDir()
	bin := filepath.Join(dir, "karpenter")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/karpenter-exoscale")
	build.Dir = repoRoot
	build.Stdout, build.Stderr = GinkgoWriter, GinkgoWriter
	Expect(build.Run()).To(Succeed())

	kubeconfig := filepath.Join(dir, "karpenter.kubeconfig")
	Expect(os.WriteFile(kubeconfig, cluster.KarpenterKubeconfig, 0o600)).To(Succeed())

	logPath := os.Getenv("KARPENTER_LOG")
	if logPath == "" {
		logPath = filepath.Join(os.TempDir(), name+"-karpenter.log")
	}
	logFile, err := os.Create(logPath)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("karpenter log: %s\n", logPath)

	cmd := exec.Command(bin, "--leader-election-namespace=kube-system")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	// Later entries win, so these override the CI's admin credentials.
	cmd.Env = append(os.Environ(),
		"EXOSCALE_API_KEY="+cluster.APIKey,
		"EXOSCALE_API_SECRET="+cluster.APISecret,
		"EXOSCALE_ZONE="+infra.Zone(),
		"EXOSCALE_SKS_CLUSTER_ID="+cluster.ID.String(),
		"EXOSCALE_COMPUTE_INSTANCE_PREFIX="+name+"-",
		"USE_SKS_DEFAULT_SECURITY_GROUP=true",
		"KUBECONFIG="+kubeconfig,
	)
	Expect(cmd.Start()).To(Succeed())

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	return func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
		}
		_ = logFile.Close()
	}
}
