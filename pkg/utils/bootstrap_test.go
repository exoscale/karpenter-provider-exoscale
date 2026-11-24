package utils

import (
	"context"
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateAndApplyBootstrapTokenSecret(t *testing.T) {
	ctx := context.Background()
	kubeClient := fake.NewClientBuilder().Build()

	bt, err := CreateAndApplyBootstrapTokenSecret(ctx, kubeClient, "nc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`).MatchString(bt.Token()) {
		t.Fatalf("token %q does not match the kubeadm bootstrap token format", bt.Token())
	}

	var secret corev1.Secret
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: metav1.NamespaceSystem, Name: bt.SecretName}, &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["token-id"]) != bt.TokenID || string(secret.Data["token-secret"]) != bt.TokenSecret {
		t.Errorf("secret data does not match returned token")
	}
}
