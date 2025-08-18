package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BootstrapToken struct {
	TokenID     string
	TokenSecret string
	SecretName  string
	Secret      *corev1.Secret
}

func (bt *BootstrapToken) Token() string {
	return bt.TokenID + "." + bt.TokenSecret
}

func CreateAndApplyBootstrapTokenSecret(ctx context.Context, kubeClient client.Client, nodeClaimName string) (*BootstrapToken, error) {
	tokenID, err := GenerateSecureRandomString(6)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token ID: %w", err)
	}

	tokenSecret, err := GenerateSecureRandomString(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token secret: %w", err)
	}

	secret := BuildBootstrapTokenSecret(tokenID, tokenSecret, nodeClaimName, constants.DefaultBootstrapTokenTTL, constants.BootstrapTokenExtraGroups)

	if err := kubeClient.Create(ctx, secret); err != nil {
		return nil, fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	return &BootstrapToken{
		TokenID:     tokenID,
		TokenSecret: tokenSecret,
		SecretName:  secret.Name,
		Secret:      secret,
	}, nil
}

func BuildBootstrapTokenSecret(tokenID, tokenSecret, nodeClaimName string, ttl time.Duration, extraGroups string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.BootstrapTokenPrefix + tokenID,
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				constants.LabelTokenProvider: constants.ProviderName,
			},
			Annotations: map[string]string{
				constants.AnnotationBootstrapToken: nodeClaimName,
				constants.AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeBootstrapToken,
		Data: map[string][]byte{
			"token-id":                       []byte(tokenID),
			"token-secret":                   []byte(tokenSecret),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":        []byte("true"),
			"auth-extra-groups":              []byte(extraGroups),
			"expiration":                     []byte(time.Now().Add(ttl).Format(time.RFC3339)),
			"description":                    []byte(fmt.Sprintf("Bootstrap token for %s created by Karpenter", nodeClaimName)),
		},
	}
}
