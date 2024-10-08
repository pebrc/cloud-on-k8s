// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package v1

import (
	"reflect"
	"testing"
)

func TestTLSOptions_Enabled(t *testing.T) {
	type fields struct {
		SelfSignedCertificate *SelfSignedCertificate
		Certificate           SecretRef
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "disabled: no custom cert and self-signed disabled",
			fields: fields{
				SelfSignedCertificate: &SelfSignedCertificate{
					Disabled: true,
				},
				Certificate: SecretRef{},
			},
			want: false,
		},
		{
			name: "enabled: custom certs and self-signed disabled",
			fields: fields{
				SelfSignedCertificate: &SelfSignedCertificate{
					Disabled: true,
				},
				Certificate: SecretRef{
					SecretName: "my-custom-certs",
				},
			},
			want: true,
		},
		{
			name:   "enabled: by default",
			fields: fields{},
			want:   true,
		},
		{
			name: "enabled: via self-signed certificates",
			fields: fields{
				SelfSignedCertificate: &SelfSignedCertificate{
					SubjectAlternativeNames: []SubjectAlternativeName{},
					Disabled:                false,
				},
				Certificate: SecretRef{},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tls := TLSOptions{
				SelfSignedCertificate: tt.fields.SelfSignedCertificate,
				Certificate:           tt.fields.Certificate,
			}
			if got := tls.Enabled(); got != tt.want {
				t.Errorf("TLSOptions.Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHTTPConfig_Scheme(t *testing.T) {
	type fields struct {
		TLS TLSOptions
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "enabled",
			fields: fields{
				TLS: TLSOptions{
					SelfSignedCertificate: &SelfSignedCertificate{
						Disabled: false,
					},
				},
			},
			want: "https",
		},
		{
			name: "enabled: custom certs and self-signed disabled",
			fields: fields{
				TLS: TLSOptions{
					SelfSignedCertificate: &SelfSignedCertificate{
						Disabled: true,
					},
					Certificate: SecretRef{
						SecretName: "my-custom-certs",
					},
				},
			},
			want: "https",
		},
		{
			name: "disabled",
			fields: fields{
				TLS: TLSOptions{
					SelfSignedCertificate: &SelfSignedCertificate{
						Disabled: true,
					},
				},
			},
			want: "http",
		},
		{
			name: "default",
			fields: fields{
				TLS: TLSOptions{},
			},
			want: "https",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			http := HTTPConfig{
				TLS: tt.fields.TLS,
			}
			if got := http.Protocol(); got != tt.want {
				t.Errorf("Protocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectSelector_WithDefaultNamespace(t *testing.T) {
	type fields struct {
		Name        string
		Namespace   string
		ServiceName string
	}
	type args struct {
		defaultNamespace string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   ObjectSelector
	}{
		{
			name: "keep non-empty namespace and name, serviceName",
			fields: fields{
				Name:        "a",
				Namespace:   "b",
				ServiceName: "c",
			},
			args: args{
				defaultNamespace: "d",
			},
			want: ObjectSelector{
				Name:        "a",
				Namespace:   "b",
				ServiceName: "c",
			},
		},
		{
			name: "default empty namespace, keep name and serviceName",
			fields: fields{
				Name:        "a",
				Namespace:   "",
				ServiceName: "c",
			},
			args: args{
				defaultNamespace: "d",
			},
			want: ObjectSelector{
				Name:        "a",
				Namespace:   "d",
				ServiceName: "c",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := ObjectSelector{
				Name:        tt.fields.Name,
				Namespace:   tt.fields.Namespace,
				ServiceName: tt.fields.ServiceName,
			}
			if got := o.WithDefaultNamespace(tt.args.defaultNamespace); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WithDefaultNamespace() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
