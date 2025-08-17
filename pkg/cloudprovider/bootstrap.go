package cloudprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	bootstrapTokenTTL = 5 * time.Minute
)

func extractTokenID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid token format, expected tokenID.tokenSecret")
	}

	tokenID := parts[0]
	if len(tokenID) != 6 {
		return "", fmt.Errorf("invalid token ID length, expected 6 characters")
	}

	return tokenID, nil
}

func getBootstrapTokenSecretName(tokenID string) string {
	return fmt.Sprintf("bootstrap-token-%s", tokenID)
}

func (c *CloudProvider) deleteBootstrapTokenSecret(ctx context.Context, token string) error {
	tokenID, err := extractTokenID(token)
	if err != nil {
		return nil
	}

	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "deleteBootstrapTokenSecret",
		"tokenID", tokenID,
	)

	secretName := getBootstrapTokenSecretName(tokenID)
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: "kube-system",
		},
	}

	if err := c.kubeClient.Delete(ctx, secret); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("bootstrap token secret already deleted")
			return nil
		}
		return fmt.Errorf("failed to delete bootstrap token secret: %w", err)
	}

	logger.Info("bootstrap token secret deleted")
	return nil
}

func buildBootstrapTokenSecret(tokenID, tokenSecret, nodeClaimName string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", tokenID),
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelTokenProvider: "karpenter-exoscale",
			},
			Annotations: map[string]string{
				AnnotationBootstrapToken: nodeClaimName,
				AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
			},
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":                       []byte(tokenID),
			"token-secret":                   []byte(tokenSecret),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":        []byte("true"),
			"auth-extra-groups":              []byte("system:bootstrappers:worker,system:bootstrappers:ingress"),
			"expiration":                     []byte(time.Now().Add(bootstrapTokenTTL).Format(time.RFC3339)),
			"description":                    []byte(fmt.Sprintf("Bootstrap token for %s created by Karpenter", nodeClaimName)),
		},
	}
}

func (c *CloudProvider) applyBootstrapTokenSecret(ctx context.Context, nodeClaimName string) (*v1.Secret, string, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "applyBootstrapTokenSecret",
		"nodeClaimName", nodeClaimName,
	)

	tokenID, err := utils.GenerateBootstrapTokenID()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token ID: %w", err)
	}

	tokenSecret, err := utils.GenerateBootstrapTokenSecret()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token secret: %w", err)
	}

	token := fmt.Sprintf("%s.%s", tokenID, tokenSecret)

	secret := buildBootstrapTokenSecret(tokenID, tokenSecret, nodeClaimName)
	if err := c.kubeClient.Create(ctx, secret); err != nil {
		return nil, "", fmt.Errorf("failed to create bootstrap token secret: %w", err)
	}

	metrics.BootstrapTokensCreatedTotal.Inc(map[string]string{})
	logger.Info("bootstrap token secret created", "tokenID", tokenID)
	return secret, token, nil
}
