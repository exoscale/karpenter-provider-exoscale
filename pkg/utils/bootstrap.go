package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BootstrapToken struct {
	TokenID     string
	TokenSecret string
	SecretName  string
}

func (bt *BootstrapToken) Token() string {
	return bt.TokenID + "." + bt.TokenSecret
}

func randomHex(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b) // never returns an error since Go 1.24
	return hex.EncodeToString(b)[:length]
}

func CreateAndApplyBootstrapTokenSecret(ctx context.Context, kubeClient client.Client, nodeClaimName string) (*BootstrapToken, error) {
	tokenID := randomHex(6)
	tokenSecret := randomHex(16)
	now := time.Now()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bootstrap-token-" + tokenID,
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				constants.LabelTokenProvider: constants.ProviderName,
			},
			Annotations: map[string]string{
				constants.AnnotationBootstrapToken: nodeClaimName,
				constants.AnnotationTokenCreated:   now.Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			"token-id":                       []byte(tokenID),
			"token-secret":                   []byte(tokenSecret),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":        []byte("true"),
			"auth-extra-groups":              []byte("system:bootstrappers:worker,system:bootstrappers:ingress"),
			"expiration":                     []byte(now.Add(constants.DefaultBootstrapTokenTTL).Format(time.RFC3339)),
			"description":                    []byte(fmt.Sprintf("Bootstrap token for %s created by Karpenter", nodeClaimName)),
		},
	}

	if err := kubeClient.Create(ctx, secret); err != nil {
		return nil, fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	return &BootstrapToken{TokenID: tokenID, TokenSecret: tokenSecret, SecretName: secret.Name}, nil
}
