package userdata

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata/bootstrap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegistryResolver loads the kube-system Secrets backing the container-registry
// feature. Both the NodeClass reconciler (validation, drift hash) and the
// userdata provider (TOML rendering) use it.
type RegistryResolver struct {
	kubeClient client.Client
}

func NewRegistryResolver(kubeClient client.Client) *RegistryResolver {
	return &RegistryResolver{kubeClient: kubeClient}
}

// ResolvedTLSSecret carries a mirror endpoint's TLS material. CA/Cert/Key are
// the raw PEM bytes rendered into the TOML; the *Dec fields hold the decoded
// PEM block bytes so the drift hash ignores whitespace-only re-encodings.
type ResolvedTLSSecret struct {
	CA      []byte
	Cert    []byte
	Key     []byte
	CADec   []byte
	CertDec []byte
	KeyDec  []byte
}

type ResolvedEndpoint struct {
	URL          string
	OverridePath bool
	SkipVerify   bool
	TLS          *ResolvedTLSSecret
}

type ResolvedMirror struct {
	Registry  string
	Endpoints []ResolvedEndpoint
}

// ResolvedCredential is one credential entry. Exactly one of the auth fields
// is populated, matching the input spec.
type ResolvedCredential struct {
	Registry      string
	Kind          string // "basic" | "auth" | "identitytoken"
	Username      []byte
	Password      []byte
	Auth          []byte
	IdentityToken []byte
}

type Resolved struct {
	Mirrors     []ResolvedMirror
	Credentials []ResolvedCredential
}

// Resolve validates and fetches every Secret referenced from spec. Errors are
// prefixed with a stable tag (SecretMissing / SecretKeyMissing /
// SecretNamespaceInvalid / InvalidPEM / TLSSecretInconsistent) so callers can
// surface them as precise condition reasons.
func (r *RegistryResolver) Resolve(ctx context.Context, spec *apiv1.ContainerRegistrySpec) (*Resolved, error) {
	if spec == nil {
		return nil, nil
	}
	out := &Resolved{}
	for _, mirror := range spec.Mirrors {
		rm := ResolvedMirror{Registry: mirror.Registry}
		for _, ep := range mirror.Endpoints {
			re := ResolvedEndpoint{URL: ep.URL, OverridePath: ep.OverridePath, SkipVerify: ep.SkipVerify}
			if ep.TLSSecretRef != nil {
				tls, err := r.loadTLSSecret(ctx, ep.TLSSecretRef)
				if err != nil {
					return nil, err
				}
				re.TLS = tls
			}
			rm.Endpoints = append(rm.Endpoints, re)
		}
		out.Mirrors = append(out.Mirrors, rm)
	}
	for ci, credential := range spec.Credentials {
		rc, err := r.loadCredential(ctx, credential, ci)
		if err != nil {
			return nil, err
		}
		out.Credentials = append(out.Credentials, rc)
	}
	return out, nil
}

func (r *RegistryResolver) loadTLSSecret(ctx context.Context, ref *v1.SecretReference) (*ResolvedTLSSecret, error) {
	if ref.Namespace != "" && ref.Namespace != metav1.NamespaceSystem {
		return nil, fmt.Errorf("SecretNamespaceInvalid: TLS secret %q must be in namespace %q, got %q", ref.Name, metav1.NamespaceSystem, ref.Namespace)
	}
	secret := &v1.Secret{}
	if err := r.kubeClient.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: metav1.NamespaceSystem}, secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("SecretMissing: TLS secret %q not found in namespace %q", ref.Name, metav1.NamespaceSystem)
		}
		return nil, fmt.Errorf("failed to get TLS secret %q: %w", ref.Name, err)
	}
	out := &ResolvedTLSSecret{}
	if b := secret.Data["ca.crt"]; len(b) > 0 {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, fmt.Errorf("InvalidPEM: TLS secret %q ca.crt is not valid PEM", ref.Name)
		}
		out.CA, out.CADec = b, block.Bytes
	}
	cert, key := secret.Data["tls.crt"], secret.Data["tls.key"]
	if (len(cert) == 0) != (len(key) == 0) {
		return nil, fmt.Errorf("TLSSecretInconsistent: TLS secret %q must declare both tls.crt and tls.key or neither", ref.Name)
	}
	if len(cert) > 0 {
		certBlock, _ := pem.Decode(cert)
		if certBlock == nil {
			return nil, fmt.Errorf("InvalidPEM: TLS secret %q tls.crt is not valid PEM", ref.Name)
		}
		keyBlock, _ := pem.Decode(key)
		if keyBlock == nil {
			return nil, fmt.Errorf("InvalidPEM: TLS secret %q tls.key is not valid PEM", ref.Name)
		}
		out.Cert, out.CertDec = cert, certBlock.Bytes
		out.Key, out.KeyDec = key, keyBlock.Bytes
	}
	return out, nil
}

func (r *RegistryResolver) loadCredential(ctx context.Context, credential apiv1.ContainerRegistryCredential, ci int) (ResolvedCredential, error) {
	rc := ResolvedCredential{Registry: credential.Registry}
	switch {
	case credential.Basic != nil:
		rc.Kind = "basic"
		u, err := r.loadSecretKey(ctx, credential.Basic.UsernameSecretRef, "username")
		if err != nil {
			return rc, err
		}
		p, err := r.loadSecretKey(ctx, credential.Basic.PasswordSecretRef, "password")
		if err != nil {
			return rc, err
		}
		rc.Username, rc.Password = u, p
	case credential.Auth != nil:
		rc.Kind = "auth"
		a, err := r.loadSecretKey(ctx, credential.Auth.AuthSecretRef, "auth")
		if err != nil {
			return rc, err
		}
		rc.Auth = a
	case credential.IdentityToken != nil:
		rc.Kind = "identitytoken"
		t, err := r.loadSecretKey(ctx, credential.IdentityToken.IdentityTokenSecretRef, "identitytoken")
		if err != nil {
			return rc, err
		}
		rc.IdentityToken = t
	default:
		return rc, fmt.Errorf("credential[%d] for registry %q has no auth method set", ci, credential.Registry)
	}
	return rc, nil
}

func (r *RegistryResolver) loadSecretKey(ctx context.Context, ref v1.SecretKeySelector, field string) ([]byte, error) {
	secret := &v1.Secret{}
	if err := r.kubeClient.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: metav1.NamespaceSystem}, secret); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("SecretMissing: %s secret %q not found in namespace %q", field, ref.Name, metav1.NamespaceSystem)
		}
		return nil, fmt.Errorf("failed to get %s secret %q: %w", field, ref.Name, err)
	}
	b, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("SecretKeyMissing: %s secret %q in namespace %q has no key %q", field, ref.Name, metav1.NamespaceSystem, ref.Key)
	}
	return b, nil
}

func (r *Resolved) ToBootstrap() *bootstrap.ContainerRegistrySettings {
	if r == nil {
		return nil
	}
	out := &bootstrap.ContainerRegistrySettings{}
	for _, m := range r.Mirrors {
		bm := bootstrap.ContainerRegistryMirror{Registry: m.Registry}
		for _, ep := range m.Endpoints {
			bm.Endpoint = append(bm.Endpoint, ep.URL)
			if ep.TLS == nil {
				continue
			}
			cfg := bootstrap.ContainerRegistryTLSConfig{OverridePath: ep.OverridePath, SkipVerify: ep.SkipVerify}
			if len(ep.TLS.CA) > 0 {
				cfg.CA = base64.StdEncoding.EncodeToString(ep.TLS.CA)
			}
			if len(ep.TLS.Cert) > 0 {
				cfg.Cert = base64.StdEncoding.EncodeToString(ep.TLS.Cert)
				cfg.Key = base64.StdEncoding.EncodeToString(ep.TLS.Key)
			}
			if out.TLS == nil {
				out.TLS = map[string]bootstrap.ContainerRegistryTLSConfig{}
			}
			out.TLS[ep.URL] = cfg
		}
		out.Mirrors = append(out.Mirrors, bm)
	}
	for _, c := range r.Credentials {
		bc := bootstrap.ContainerRegistryCredentialConfig{Registry: c.Registry}
		switch c.Kind {
		case "basic":
			bc.Username = string(c.Username)
			bc.Password = string(c.Password)
		case "auth":
			bc.Auth = string(c.Auth)
		case "identitytoken":
			bc.IdentityToken = string(c.IdentityToken)
		}
		out.Credentials = append(out.Credentials, bc)
	}
	return out
}
