package bootstrap

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestBuildConfigWithContainerRegistry(t *testing.T) {
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("ca"),
		ContainerRegistry: &ContainerRegistrySettings{
			Mirrors: []ContainerRegistryMirror{
				{Registry: "docker.io", Endpoint: []string{"https://mirror.example.com"}},
			},
			TLS: map[string]ContainerRegistryTLSConfig{
				"https://mirror.example.com": {
					CA:           base64.StdEncoding.EncodeToString([]byte("ca-pem")),
					Cert:         base64.StdEncoding.EncodeToString([]byte("cert-pem")),
					Key:          base64.StdEncoding.EncodeToString([]byte("key-pem")),
					OverridePath: true,
				},
			},
			Credentials: []ContainerRegistryCredentialConfig{
				{Registry: "reg.example.com", Username: "user", Password: "pass"},
				{Registry: "auth.example.com", Auth: "dXNlcjpwYXNz"},
				{Registry: "tok.example.com", IdentityToken: "oa-token"},
			},
		},
	}

	cfg := buildConfig(options)
	if cfg.Settings.ContainerRegistry == nil {
		t.Fatal("ContainerRegistry settings should be set")
	}
	if len(cfg.Settings.ContainerRegistry.Mirrors) != 1 {
		t.Errorf("expected 1 mirror, got %d", len(cfg.Settings.ContainerRegistry.Mirrors))
	}
	if len(cfg.Settings.ContainerRegistry.Credentials) != 3 {
		t.Errorf("expected 3 credentials, got %d", len(cfg.Settings.ContainerRegistry.Credentials))
	}
	tls, ok := cfg.Settings.ContainerRegistry.TLS["https://mirror.example.com"]
	if !ok {
		t.Fatal("expected TLS config to be present")
	}
	if !tls.OverridePath {
		t.Error("expected OverridePath true")
	}
	if tls.CA == "" || tls.Cert == "" || tls.Key == "" {
		t.Errorf("expected non-empty CA/Cert/Key, got %+v", tls)
	}
}

func TestBuildConfigWithoutContainerRegistry(t *testing.T) {
	cfg := buildConfig(&Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("ca"),
	})
	if cfg.Settings.ContainerRegistry != nil {
		t.Errorf("expected nil ContainerRegistry, got %+v", cfg.Settings.ContainerRegistry)
	}
}

func TestMarshalTOMLWithContainerRegistry(t *testing.T) {
	cfg := &Config{
		Settings: Settings{
			Kubernetes: KubernetesSettings{
				APIServer:      "https://api.example.com",
				BootstrapToken: "t",
				CloudProvider:  "external",
			},
			ContainerRegistry: &ContainerRegistrySettings{
				Mirrors: []ContainerRegistryMirror{
					{Registry: "docker.io", Endpoint: []string{"https://m.example.com"}},
				},
			},
		},
	}
	data, err := marshalTOML(cfg)
	if err != nil {
		t.Fatalf("marshalTOML error: %v", err)
	}
	if !bytes.Contains(data, []byte("docker.io")) {
		t.Errorf("expected docker.io in output, got:\n%s", data)
	}
	if !bytes.Contains(data, []byte("container-registry")) {
		t.Errorf("expected container-registry section, got:\n%s", data)
	}
}
