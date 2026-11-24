package userdata

import (
	"bytes"
	"testing"
)

// "Q0E=" is base64 for "CA".
func TestExtractCACertFromKubeconfig(t *testing.T) {
	tests := []struct {
		name       string
		kubeconfig string
		want       string
		wantErr    bool
	}{
		{
			name: "kubeadm cluster-info: single unnamed cluster, no contexts",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://api.example:6443
  name: ""
contexts: null
current-context: ""
`,
			want: "CA",
		},
		{
			name: "multiple clusters picks current-context",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster: {certificate-authority-data: b3RoZXI=, server: https://a}
  name: a
- cluster: {certificate-authority-data: Q0E=, server: https://b}
  name: b
contexts:
- context: {cluster: b, user: u}
  name: ctx
current-context: ctx
`,
			want: "CA",
		},
		{
			name: "multiple clusters without current-context is ambiguous",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster: {certificate-authority-data: Q0E=, server: https://a}
  name: a
- cluster: {certificate-authority-data: Q0E=, server: https://b}
  name: b
`,
			wantErr: true,
		},
		{
			name:       "no clusters",
			kubeconfig: "apiVersion: v1\nkind: Config\n",
			wantErr:    true,
		},
		{
			name: "missing CA data",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster: {server: https://a}
  name: a
`,
			wantErr: true,
		},
		{
			name: "invalid base64",
			kubeconfig: `apiVersion: v1
kind: Config
clusters:
- cluster: {certificate-authority-data: "!!!", server: https://a}
  name: a
`,
			wantErr: true,
		},
		{
			name:       "not yaml",
			kubeconfig: "{",
			wantErr:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractCACertFromKubeconfig(tt.kubeconfig)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
